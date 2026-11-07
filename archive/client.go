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
	Timeout         time.Duration
}
type Client struct {
	cleanupMu              sync.Mutex
	pending                []func() error
	active                 int
	closed                 bool
	root, directory, batch string
	launch                 Launcher
	timeout                time.Duration
	slots                  chan struct{}
	prepareGate            chan struct{}
	mu                     sync.Mutex
	prepared               bool
}

func NewClient(o ClientOptions) (*Client, error) {
	if !filepath.IsAbs(o.Root) || filepath.Clean(o.Root) != o.Root || o.Directory == "" || o.Launcher == nil || o.Timeout < 0 {
		return nil, ErrArchive
	}
	directory, e := filepath.Abs(o.Directory)
	if e != nil {
		return nil, e
	}
	batch := time.Now().In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02")
	return &Client{root: o.Root, directory: directory, batch: batch, launch: o.Launcher, timeout: o.Timeout, slots: make(chan struct{}, 4), prepareGate: make(chan struct{}, 1)}, nil
}
func (c *Client) channel(ctx context.Context, run func(*Protocol) error) (err error) {
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
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.slots }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return runChannel(ctx, c.launch, c.timeout, run, func(close func() error) {
		c.cleanupMu.Lock()
		c.pending = append(c.pending, close)
		c.cleanupMu.Unlock()
	})
}

func (c *Client) Prepare(ctx context.Context) error {
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
	e := c.channel(ctx, func(p *Protocol) error {
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
	e = c.channel(ctx, func(p *Protocol) error {
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
func (c *Client) Upload(ctx context.Context, filename, archiveName string, upload func(context.Context, string) (transfer.Receipt, error)) (receipt transfer.Receipt, err error) {
	c.mu.Lock()
	prepared := c.prepared
	c.mu.Unlock()
	if !prepared || !isSafeName(filename) || !isSafeName(archiveName) || upload == nil {
		return receipt, ErrArchive
	}
	err = c.channel(ctx, func(p *Protocol) error {
		request := Request{Root: c.root, Filename: filename, ArchiveName: archiveName, Batch: c.batch}
		if e := p.Send(ctx, request); e != nil {
			return e
		}
		if _, e := p.Expect(ctx, "ready"); e != nil {
			return e
		}
		var primary error
		receipt, primary = upload(ctx, filename)
		if !receipt.Reported() || receipt.Path != filename {
			_ = p.Send(ctx, Request{Action: "abort"})
			if primary != nil {
				return primary
			}
			return ErrArchive
		}
		if e := p.Send(ctx, Request{Action: "verify"}); e != nil {
			return errors.Join(primary, e)
		}
		proof, e := p.Expect(ctx, "archived")
		if e != nil {
			return errors.Join(primary, e)
		}
		if proof.ArchivePath != filepath.Join(c.root, "archive", c.batch, archiveName) {
			return errors.Join(primary, ErrArchive)
		}
		destination := filepath.Join(c.directory, "tftp", archiveName)
		called := false
		var received Proof
		fileReceipt, persistErr := storage.WriteStream(ctx, destination, func(w io.Writer) error { called = true; var e error; received, e = p.Receive(ctx, proof, w); return e })
		if !called {
			received, e = p.Receive(ctx, proof, io.Discard)
			if e != nil {
				return errors.Join(primary, persistErr, e)
			}
		}
		if received.Verified {
			receipt.Verification = "server_verified"
			receipt.ServerSHA256 = received.SHA256
			receipt.ServerBytes = received.Bytes
			receipt.ArchivePath = received.ArchivePath
		}
		if fileReceipt.Committed() {
			receipt.LocalPath = fileReceipt.Path
		}
		if primary != nil || persistErr != nil || !received.Verified {
			_ = p.Send(ctx, Request{Action: "abort"})
			if primary != nil || persistErr != nil {
				return errors.Join(primary, persistErr)
			}
			return ErrArchive
		}
		if e = p.Send(ctx, Request{Action: "cleanup", SHA256: received.SHA256}); e != nil {
			return e
		}
		_, e = p.Expect(ctx, "complete")
		return e
	})
	return receipt, err
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
	for _, close := range c.pending {
		if e := close(); e != nil {
			err = errors.Join(err, e)
			failed = append(failed, close)
		}
	}
	c.pending = failed
	if err != nil {
		return &Error{cause: err}
	}
	return nil
}
