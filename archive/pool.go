package archive

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gatework/go-net-connector/transfer"
)

// PoolOptions describes local or SSH archive storage. NewPool does no I/O.
type PoolOptions struct {
	Root, Directory, WorkerPath, Password string
	Parallel                              int
	Timeouts                              Timeouts
}

// PrepareStage identifies the safe phase of archive service preparation.
type PrepareStage string

const (
	PrepareStageSSH      PrepareStage = "ssh"
	PrepareStageProtocol PrepareStage = "protocol"
	PrepareStageReady    PrepareStage = "ready"
)

// PrepareProgress reports bounded archive preparation metadata.
type PrepareProgress struct {
	Completed, Total, Active int
	Workers                  int
	Stage                    PrepareStage
}

type Pool struct {
	timeouts                             Timeouts
	clients                              []*Client
	masters                              []*sshMaster
	available                            chan *Client
	endpoint                             *url.URL
	mu                                   sync.Mutex
	prepared, closed, cleaned, preparing bool
	prepareDone                          chan struct{}
	prepareCancel                        context.CancelFunc
	closeDone                            chan struct{}
	closeErr                             error
	stop                                 chan struct{}
	active                               sync.WaitGroup
}

func NewPool(o PoolOptions) (*Pool, error) {
	timeouts, err := o.Timeouts.normalize()
	if err != nil {
		return nil, err
	}
	if o.Parallel < 1 || o.Parallel > 500 || o.Directory == "" || strings.ContainsAny(o.Password, "\x00\r\n") {
		return nil, ErrArchive
	}
	root, err := transfer.VerificationRoot(o.Root)
	if err != nil {
		return nil, err
	}
	worker := o.WorkerPath
	if worker == "" {
		worker = DefaultWorkerPath
	}
	if !safeWorkerPath(worker) {
		return nil, ErrArchive
	}
	p := &Pool{timeouts: timeouts, available: make(chan *Client, o.Parallel), stop: make(chan struct{}), closeDone: make(chan struct{})}
	clientRoot := root
	if strings.HasPrefix(root, "ssh://") {
		p.endpoint, clientRoot, err = parseArchiveEndpoint(root)
		if err != nil {
			return nil, err
		}
	}
	for remaining := o.Parallel; remaining > 0; {
		count := min(remaining, 4)
		var launch Launcher
		if p.endpoint != nil {
			master := &sshMaster{endpoint: p.endpoint, worker: worker, password: o.Password}
			p.masters = append(p.masters, master)
			launch = master.launch
		} else {
			launch = LocalLauncher()
		}
		client, e := NewClient(ClientOptions{Root: clientRoot, Directory: o.Directory, Launcher: launch, Timeouts: timeouts})
		if e != nil {
			return nil, e
		}
		if len(p.clients) > 0 {
			client.batch = p.clients[0].batch
		}
		p.clients = append(p.clients, client)
		for range count {
			p.available <- client
		}
		remaining -= count
	}
	return p, nil
}

// Prepare preflights every master, protocol version and archive directory.
// A non-nil observe receives stage counts without credentials or protocol
// bodies. Close can cancel and join this work even without a caller deadline.
func (p *Pool) Prepare(ctx context.Context, observe func(PrepareProgress)) error {
	return p.prepare(ctx, observe)
}

// Upload borrows one of at most Parallel slots and returns it after the transaction.
func (p *Pool) Upload(ctx context.Context, filename, archiveName string, upload func(context.Context, string) (transfer.Receipt, error)) (Result, error) {
	prepare, cancelPrepare := context.WithTimeout(ctx, p.timeouts.Prepare)
	defer cancelPrepare()
	p.mu.Lock()
	if !p.prepared || p.closed {
		p.mu.Unlock()
		return Result{}, ErrArchive
	}
	p.active.Add(1)
	p.mu.Unlock()
	defer p.active.Done()
	select {
	case <-prepare.Done():
		return Result{}, stageFailure("prepare", prepare.Err())
	case <-p.stop:
		return Result{}, ErrArchive
	case client := <-p.available:
		defer func() { p.available <- client }()
		select {
		case <-p.stop:
			return Result{}, ErrArchive
		default:
		}
		callCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(&poolDoneContext{done: p.stop}, cancel)
		defer func() { stop(); cancel() }()
		// Prepare admission must also observe Pool.Close, while keeping its
		// original deadline across the local queue and the server's target lock.
		admission, cancelAdmission := context.WithCancel(prepare)
		stopAdmission := context.AfterFunc(callCtx, cancelAdmission)
		defer func() { stopAdmission(); cancelAdmission() }()
		receipt, err := client.upload(callCtx, admission, filename, archiveName, upload)
		if p.endpoint != nil && receipt.Receipt.Server != nil && receipt.Receipt.Server.ArchivePath != "" {
			u := *p.endpoint
			u.Path, u.RawPath = receipt.Receipt.Server.ArchivePath, ""
			receipt.Receipt.Server.ArchivePath = u.String()
		}
		return receipt, err
	}
}

func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed && p.cleaned && p.closeErr != nil {
		// A completed failed attempt retains owners; a later Close retries them.
		p.cleaned = false
		p.closeDone = make(chan struct{})
		defer p.mu.Unlock()
		return p.closeLocked()
	}
	if p.closed {
		done := p.closeDone
		p.mu.Unlock()
		<-done
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.closeErr
	}
	p.closed = true
	close(p.stop)
	if p.prepareCancel != nil {
		p.prepareCancel()
	}
	prepDone := p.prepareDone
	p.mu.Unlock()
	if prepDone != nil {
		<-prepDone
	}
	p.active.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeLocked()
}

// ArchiveDirectory returns the server directory after successful preparation.
func (p *Pool) ArchiveDirectory() string {
	return filepath.Join(p.clients[0].root, "archive", p.clients[0].batch)
}

func (p *Pool) prepare(ctx context.Context, observe func(PrepareProgress)) error {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return ErrArchive
		}
		if p.prepared {
			progress := PrepareProgress{Completed: len(p.clients), Total: len(p.clients), Workers: p.preparationWorkers(), Stage: PrepareStageReady}
			p.mu.Unlock()
			if observe != nil {
				observe(progress)
			}
			return nil
		}
		if p.preparing {
			done := p.prepareDone
			p.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		prepCtx, cancel := context.WithCancel(ctx)
		p.preparing = true
		p.prepareDone = make(chan struct{})
		p.prepareCancel = cancel
		p.mu.Unlock()

		err := p.prepareAll(prepCtx, observe)
		cancel()
		p.mu.Lock()
		if p.closed && err == nil {
			err = context.Canceled
		}
		p.preparing = false
		p.prepareCancel = nil
		p.prepared = err == nil
		close(p.prepareDone)
		p.mu.Unlock()
		if err != nil {
			return errors.Join(err, p.Close())
		}
		return nil
	}
}

func (p *Pool) prepareAll(ctx context.Context, observe func(PrepareProgress)) error {
	if p.endpoint == nil {
		batch, err := reserveLocalBatch(ctx, p.clients[0].root, p.clients[0].directory)
		if err != nil {
			return err
		}
		for _, client := range p.clients {
			client.batch = batch
		}
		if observe != nil {
			observe(PrepareProgress{Total: len(p.clients), Workers: 1, Stage: PrepareStageProtocol})
		}
		completed := 0
		for _, client := range p.clients {
			if err := ctx.Err(); err != nil {
				return err
			}
			if observe != nil {
				observe(PrepareProgress{Completed: completed, Total: len(p.clients), Active: 1, Workers: 1, Stage: PrepareStageProtocol})
			}
			err := client.Prepare(ctx)
			if err != nil {
				if observe != nil {
					observe(PrepareProgress{Completed: completed, Total: len(p.clients), Workers: 1, Stage: PrepareStageProtocol})
				}
				return err
			}
			completed++
			if observe != nil {
				observe(PrepareProgress{Completed: completed, Total: len(p.clients), Workers: 1, Stage: PrepareStageProtocol})
			}
		}
		if observe != nil {
			observe(PrepareProgress{Completed: completed, Total: len(p.clients), Workers: 1, Stage: PrepareStageReady})
		}
		return nil
	}

	return p.prepareRemote(ctx, observe)
}

type poolDoneContext struct{ done <-chan struct{} }

func (c *poolDoneContext) Deadline() (deadline time.Time, ok bool) { return }
func (c *poolDoneContext) Done() <-chan struct{}                   { return c.done }
func (c *poolDoneContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}
func (c *poolDoneContext) Value(any) any { return nil }

// prepareRemote starts every SSH master and prepares its client with a
// bounded set of workers, stopping all of them at the first failure.
func (p *Pool) prepareRemote(ctx context.Context, observe func(PrepareProgress)) error {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	t := &prepareTracker{observe: observe, total: len(p.clients), workers: p.preparationWorkers(), cancel: cancel}
	var next atomic.Int64
	var wg sync.WaitGroup
	for range t.workers {
		wg.Go(func() {
			for workerCtx.Err() == nil {
				i := int(next.Add(1) - 1)
				if i >= len(p.clients) || !p.prepareOne(workerCtx, t, i) {
					return
				}
			}
		})
	}
	wg.Wait()
	if err := t.err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	t.ready()
	return nil
}

// prepareOne starts master i and prepares client i, reporting each stage.
// It reports false after a failure.
func (p *Pool) prepareOne(ctx context.Context, t *prepareTracker, i int) bool {
	t.report(PrepareStageSSH, 1, 0)
	if err := p.masters[i].prepare(ctx); err != nil {
		t.fail(err)
		t.report(PrepareStageSSH, -1, 0)
		return false
	}
	t.report(PrepareStageProtocol, 0, 0)
	if err := p.clients[i].Prepare(ctx); err != nil {
		t.fail(err)
		t.report(PrepareStageProtocol, -1, 0)
		return false
	}
	t.report(PrepareStageProtocol, -1, 1)
	return true
}

// prepareTracker counts preparation progress across workers and keeps the
// first failure.
type prepareTracker struct {
	observe   func(PrepareProgress)
	total     int
	workers   int
	cancel    context.CancelFunc
	mu        sync.Mutex
	completed int
	active    int
	errorMu   sync.Mutex
	first     error
}

// report adjusts the counts and notifies the observer.
func (t *prepareTracker) report(stage PrepareStage, activeDelta, completedDelta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active += activeDelta
	t.completed += completedDelta
	if t.observe != nil {
		t.observe(PrepareProgress{Completed: t.completed, Total: t.total, Active: t.active, Workers: t.workers, Stage: stage})
	}
}

// fail records err if it is the first failure and cancels the workers.
func (t *prepareTracker) fail(err error) {
	t.errorMu.Lock()
	defer t.errorMu.Unlock()
	if t.first == nil {
		t.first = err
		t.cancel()
	}
}

// err returns the first failure.
func (t *prepareTracker) err() error {
	t.errorMu.Lock()
	defer t.errorMu.Unlock()
	return t.first
}

// ready reports the final ready progress outside the lock.
func (t *prepareTracker) ready() {
	if t.observe == nil {
		return
	}
	t.mu.Lock()
	ready := PrepareProgress{Completed: t.completed, Total: t.total, Workers: t.workers, Stage: PrepareStageReady}
	t.mu.Unlock()
	t.observe(ready)
}

func (p *Pool) preparationWorkers() int {
	if p.endpoint == nil {
		return 1
	}
	return min(4, len(p.clients))
}

// MarshalJSON exposes operational metadata only; credentials remain available explicitly on the options.
func (o PoolOptions) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Root, Directory, WorkerPath string
		Parallel                    int
	}{safeOptionURL(o.Root), o.Directory, o.WorkerPath, o.Parallel})
}
func (o PoolOptions) String() string   { b, _ := o.MarshalJSON(); return "PoolOptions" + string(b) }
func (o PoolOptions) GoString() string { return o.String() }

func safeOptionURL(raw string) string {
	if !strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ssh" || u.Host == "" {
		return "[invalid URL]"
	}
	return u.Scheme + "://" + u.Host
}

func (p *Pool) closeLocked() error {
	if p.cleaned {
		return p.closeErr
	}
	if !p.closed {
		p.closed = true
		close(p.stop)
	}
	var err error
	for _, client := range p.clients {
		err = errors.Join(err, client.Close())
	}
	for _, master := range p.masters {
		err = errors.Join(err, master.close())
	}
	p.closeErr = err
	p.cleaned = true
	close(p.closeDone)
	return err
}
