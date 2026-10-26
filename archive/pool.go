package archive

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gatework/go-net-connector/transfer"
	"net/url"
	"strings"
	"sync"
	"time"
)

// PoolOptions describes local or SSH archive storage. NewPool does no I/O.
type PoolOptions struct {
	Root, Directory, WorkerPath, Password string
	Parallel                              int
}

type Pool struct {
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
	if o.Parallel < 1 || o.Parallel > 500 || o.Directory == "" || strings.ContainsAny(o.Password, "\x00\r\n") {
		return nil, ErrArchive
	}
	root, err := transfer.VerificationRoot(o.Root)
	if err != nil {
		return nil, err
	}
	worker := o.WorkerPath
	if worker == "" {
		worker = defaultWorkerPath
	}
	if !safeWorkerPath(worker) {
		return nil, ErrArchive
	}
	p := &Pool{available: make(chan *Client, o.Parallel), stop: make(chan struct{}), closeDone: make(chan struct{})}
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
		client, e := NewClient(ClientOptions{Root: clientRoot, Directory: o.Directory, Launcher: launch})
		if e != nil {
			return nil, e
		}
		p.clients = append(p.clients, client)
		for i := 0; i < count; i++ {
			p.available <- client
		}
		remaining -= count
	}
	return p, nil
}

// Prepare preflights every master, protocol version and archive directory.
// Close can cancel and join this work even when the caller supplied no deadline.
func (p *Pool) Prepare(ctx context.Context) error {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return ErrArchive
		}
		if p.prepared {
			p.mu.Unlock()
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

		err := p.prepareAll(prepCtx)
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

func (p *Pool) prepareAll(ctx context.Context) error {
	if p.endpoint == nil {
		batch, err := reserveLocalBatch(ctx, p.clients[0].root, p.clients[0].directory)
		if err != nil {
			return err
		}
		for _, client := range p.clients {
			client.batch = batch
		}
	}
	for i, client := range p.clients {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.endpoint != nil {
			if err := p.masters[i].prepare(ctx); err != nil {
				return err
			}
		}
		if err := client.Prepare(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Upload borrows one of at most Parallel slots and returns it after the transaction.
func (p *Pool) Upload(ctx context.Context, filename, archiveName string, upload func(context.Context, string) (transfer.Receipt, error)) (transfer.Receipt, error) {
	p.mu.Lock()
	if !p.prepared || p.closed {
		p.mu.Unlock()
		return transfer.Receipt{}, ErrArchive
	}
	p.active.Add(1)
	p.mu.Unlock()
	defer p.active.Done()
	select {
	case <-ctx.Done():
		return transfer.Receipt{}, ctx.Err()
	case <-p.stop:
		return transfer.Receipt{}, ErrArchive
	case client := <-p.available:
		defer func() { p.available <- client }()
		select {
		case <-p.stop:
			return transfer.Receipt{}, ErrArchive
		default:
		}
		callCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(&poolDoneContext{done: p.stop}, cancel)
		defer func() { stop(); cancel() }()
		receipt, err := client.Upload(callCtx, filename, archiveName, upload)
		if p.endpoint != nil && receipt.ArchivePath != "" {
			u := *p.endpoint
			u.Path, u.RawPath = receipt.ArchivePath, ""
			receipt.ArchivePath = u.String()
		}
		return receipt, err
	}
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

func (p *Pool) Close() error {
	p.mu.Lock()
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

func (p *Pool) closeLocked() error {
	if p.cleaned {
		return p.closeErr
	}
	if !p.closed {
		p.closed = true
		close(p.stop)
	}
	var err error
	for _, master := range p.masters {
		err = errors.Join(err, master.close())
	}
	p.closeErr = err
	p.cleaned = true
	close(p.closeDone)
	return err
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
