package archive

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
)

type ClientOptions struct {
	Root, Directory string
	Launcher        Launcher
	Timeouts        Timeouts
}
type Client struct {
	cleanupMu              sync.Mutex
	pending                []func() error
	active                 int
	closed                 bool
	root, directory, batch string
	launch                 Launcher
	timeouts               Timeouts
	persist                func(context.Context, string, func(io.Writer) error) (storage.Receipt, error)
	slots                  chan struct{}
	prepareGate            chan struct{}
	mu                     sync.Mutex
	prepared               bool
}

func NewClient(o ClientOptions) (*Client, error) {
	if !filepath.IsAbs(o.Root) || filepath.Clean(o.Root) != o.Root || o.Directory == "" || o.Launcher == nil {
		return nil, ErrArchive
	}
	timeouts, e := o.Timeouts.normalize()
	if e != nil {
		return nil, e
	}
	directory, e := filepath.Abs(o.Directory)
	if e != nil {
		return nil, e
	}
	batch := time.Now().In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02")
	return &Client{root: o.Root, directory: directory, batch: batch, launch: o.Launcher, timeouts: timeouts, persist: storage.WriteStream, slots: make(chan struct{}, 4), prepareGate: make(chan struct{}, 1)}, nil
}
func (c *Client) Prepare(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeouts.Prepare)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case c.prepareGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.prepareGate }()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.cleanupMu.Lock()
	if c.closed {
		c.cleanupMu.Unlock()
		return ErrArchive
	}
	// Own the whole initialization, including local directory preparation.
	c.active++
	c.cleanupMu.Unlock()
	defer func() { c.cleanupMu.Lock(); c.active--; c.cleanupMu.Unlock() }()
	c.mu.Lock()
	prepared := c.prepared
	c.mu.Unlock()
	if prepared {
		return nil
	}
	if e := storage.PrepareDirectory(filepath.Join(c.directory, "tftp")); e != nil {
		return e
	}
	probeCtx, cancelProbe := context.WithTimeout(ctx, controlTimeout)
	defer cancelProbe()
	e := c.channel(probeCtx, func(p *Protocol) error {
		ctx := probeCtx
		if e := p.Send(ctx, Request{Action: "probe"}); e != nil {
			return e
		}
		if _, e := p.Expect(ctx, "ready"); e != nil {
			return e
		}
		if p.last.Version != Version {
			return ErrArchive
		}
		found := map[string]bool{}
		for _, v := range p.last.Capabilities {
			found[v] = true
		}
		for _, v := range capabilities {
			if !found[v] {
				return ErrArchive
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	preflightCtx, cancelPreflight := context.WithTimeout(ctx, controlTimeout)
	defer cancelPreflight()
	e = c.channel(preflightCtx, func(p *Protocol) error {
		ctx := preflightCtx
		if e := p.Send(ctx, Request{Action: "preflight", Root: c.root, Filename: "preflight.cfg", ArchiveName: "preflight.cfg", Batch: c.batch}); e != nil {
			return e
		}
		_, e := p.Expect(ctx, "ready")
		return e
	})
	if e == nil {
		e = ctx.Err()
	}
	if e == nil {
		c.mu.Lock()
		c.prepared = true
		c.mu.Unlock()
	}
	return e
}

// Upload owns one target transaction. Independent child contexts prevent queue
// time from consuming the upload window, without detaching from caller cancel.
func (c *Client) Upload(ctx context.Context, filename, archiveName string, upload func(context.Context, string) (transfer.Receipt, error)) (Result, error) {
	prepare, cancel := context.WithTimeout(ctx, c.timeouts.Prepare)
	defer cancel()
	return c.upload(ctx, prepare, filename, archiveName, upload)
}

// Close rejects new transactions and retries pending channel cleanup. Call after
// transactions return; active work belongs to its context and returns ErrArchive.
// Pool cancels and joins admitted work before calling this method.
func (c *Client) Close() error {
	c.cleanupMu.Lock()
	defer c.cleanupMu.Unlock()
	if c.active != 0 {
		return ErrArchive
	}
	c.closed = true
	var failed []func() error
	var err error
	for _, closeFn := range c.pending {
		if e := closeFn(); e != nil {
			err = errors.Join(err, e)
			failed = append(failed, closeFn)
		}
	}
	c.pending = failed
	if err != nil {
		return &Error{cause: err}
	}
	return nil
}

func (c *Client) channel(ctx context.Context, run func(*Protocol) error) error {
	return c.channelWithin(ctx, ctx, run)
}

func (c *Client) channelWithin(ctx, admission context.Context, run func(*Protocol) error) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.cleanupMu.Lock()
	if c.closed {
		c.cleanupMu.Unlock()
		return ErrArchive
	}
	c.active++
	c.cleanupMu.Unlock()
	defer func() { c.cleanupMu.Lock(); c.active--; c.cleanupMu.Unlock() }()
	select {
	case c.slots <- struct{}{}:
	case <-admission.Done():
		return stageFailure("prepare", admission.Err())
	}
	defer func() { <-c.slots }()
	if err := admission.Err(); err != nil {
		return stageFailure("prepare", err)
	}
	// The launcher must honor admission cancellation, but the launched worker
	// belongs to the transaction, not to the expiring preparation context.
	channelCtx, cancelChannel := context.WithCancel(ctx)
	defer cancelChannel()
	launch := func(context.Context) (Channel, error) {
		stop := context.AfterFunc(admission, cancelChannel)
		if admission.Err() != nil {
			cancelChannel()
		}
		ch, err := c.launch(channelCtx)
		if !stop() {
			cancelChannel()
		}
		if admission.Err() != nil {
			// Cancellation is how the launcher is interrupted, not the cause of
			// the preparation failure. Put the stage deadline first while retaining
			// every launcher cause, including parallel I/O or cleanup failures.
			return ch, errors.Join(stageFailure("prepare", admission.Err()), err)
		}
		return ch, errors.Join(err, channelCtx.Err())
	}
	return runChannel(channelCtx, launch, run, func(closeFn func() error) {
		c.cleanupMu.Lock()
		c.pending = append(c.pending, closeFn)
		c.cleanupMu.Unlock()
	})
}

// upload runs one archive transaction: the worker prepares the target,
// upload delivers the file, and the worker archives, returns and finally
// removes the staging copy.
func (c *Client) upload(ctx, prepare context.Context, filename, archiveName string, upload func(context.Context, string) (transfer.Receipt, error)) (result Result, err error) {
	result.Receipt.Cleanup = CleanupNotRequested
	c.mu.Lock()
	prepared := c.prepared
	c.mu.Unlock()
	if !prepared || !isSafeName(filename) || !isSafeName(archiveName) || upload == nil {
		return result, stageFailure("prepare", ErrArchive)
	}
	err = c.channelWithin(ctx, prepare, func(p *Protocol) error {
		t := &uploadTransaction{client: c, protocol: p, filename: filename, archiveName: archiveName, result: &result}
		return t.run(ctx, prepare, upload)
	})
	return result, err
}

// uploadTransaction is the client side of one archive transaction.
type uploadTransaction struct {
	client      *Client
	protocol    *Protocol
	filename    string
	archiveName string
	result      *Result
}

// run drives the prepare, upload, verify and cleanup stages.
func (t *uploadTransaction) run(ctx, prepare context.Context, upload func(context.Context, string) (transfer.Receipt, error)) error {
	c, p := t.client, t.protocol
	if e := t.prepare(prepare); e != nil {
		return e
	}
	uploadCtx, cancelUpload := context.WithTimeout(ctx, c.timeouts.Upload)
	var primary error
	t.result.Transfer, primary = upload(uploadCtx, t.filename)
	primary = errors.Join(primary, uploadCtx.Err())
	cancelUpload()
	if primary != nil {
		primary = stageFailure("upload", primary)
	}
	if !t.result.Transfer.Reported() || t.result.Transfer.Path != t.filename {
		c.abort(ctx, p)
		if primary != nil {
			return primary
		}
		return stageFailure("upload", ErrArchive)
	}
	verifyCtx, cancelVerify := context.WithTimeout(ctx, c.timeouts.Verify)
	defer cancelVerify()
	received, e := t.verify(verifyCtx, primary)
	if e != nil {
		return e
	}
	persistErr := errors.Join(t.receive(verifyCtx, received), verifyCtx.Err())
	if primary != nil || persistErr != nil || !t.result.Receipt.Verified() || !t.result.Receipt.Local.Durable() {
		c.abort(ctx, p)
		if persistErr != nil {
			return errors.Join(primary, stageFailure("verify", persistErr))
		}
		if primary != nil {
			return primary
		}
		return stageFailure("verify", ErrArchive)
	}
	cleanupCtx, cancelCleanup := context.WithTimeout(ctx, c.timeouts.Cleanup)
	defer cancelCleanup()
	return t.cleanup(cleanupCtx)
}

// prepare asks the worker to lock and preserve the target and waits for
// ready.
func (t *uploadTransaction) prepare(ctx context.Context) error {
	c, p := t.client, t.protocol
	budgets, e := c.timeouts.wire(ctx)
	if e != nil {
		return e
	}
	request := Request{Root: c.root, Filename: t.filename, ArchiveName: t.archiveName, Batch: c.batch, Timeouts: &budgets}
	if e := p.Send(ctx, request); e != nil {
		return stageFailure("prepare", e)
	}
	if _, e := p.Expect(ctx, "ready"); e != nil {
		return stageFailure("prepare", e)
	}
	if e := ctx.Err(); e != nil {
		return stageFailure("prepare", e)
	}
	return nil
}

// verify asks the worker to archive the upload and checks the archive
// path it reports. primary is the upload failure joined to any error.
func (t *uploadTransaction) verify(ctx context.Context, primary error) (Proof, error) {
	c, p := t.client, t.protocol
	if e := p.Send(ctx, Request{Action: "verify"}); e != nil {
		return Proof{}, errors.Join(primary, stageFailure("verify", e))
	}
	proof, e := p.Expect(ctx, "archived")
	if e != nil {
		return Proof{}, errors.Join(primary, stageFailure("verify", e))
	}
	if proof.ArchivePath != filepath.Join(c.root, "archive", c.batch, t.archiveName) {
		return Proof{}, errors.Join(primary, stageFailure("verify", ErrArchive))
	}
	t.result.Receipt.Server = &proof
	return proof, nil
}

// receive streams the archived copy into the local batch, draining it when
// the local write never starts, and records both receipts.
func (t *uploadTransaction) receive(ctx context.Context, proof Proof) error {
	c, p := t.client, t.protocol
	destination := filepath.Join(c.directory, "tftp", t.archiveName)
	called := false
	received := proof
	fileReceipt, persistErr := c.persist(ctx, destination, func(w io.Writer) error {
		called = true
		var e error
		received, e = p.Receive(ctx, proof, w)
		return e
	})
	t.result.Receipt.Local = fileReceipt
	if !called {
		var receiveErr error
		received, receiveErr = p.Receive(ctx, proof, io.Discard)
		persistErr = errors.Join(persistErr, receiveErr)
	}
	t.result.Receipt.Server = &received
	return persistErr
}

// cleanup confirms the delivered digest so the worker removes the staging
// upload.
func (t *uploadTransaction) cleanup(ctx context.Context) error {
	p := t.protocol
	t.result.Receipt.Cleanup = CleanupUnconfirmed
	if e := p.Send(ctx, Request{Action: "cleanup", SHA256: t.result.Receipt.Server.SHA256}); e != nil {
		return stageFailure("cleanup", e)
	}
	if _, e := p.Expect(ctx, "complete"); e != nil {
		return stageFailure("cleanup", e)
	}
	t.result.Receipt.Cleanup = CleanupComplete
	return nil
}

func (c *Client) abort(ctx context.Context, p *Protocol) {
	control, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	_ = p.Send(control, Request{Action: "abort"})
}
