package archive

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
	"io"
	"path/filepath"
	"sync"
	"time"
)

type Channel struct {
	Input  io.Writer
	Output io.Reader
	Close  func() error
}
type Launcher func(context.Context) (Channel, error)
type ClientOptions struct {
	Root, Directory string
	Launcher        Launcher
	Timeout         time.Duration
}
type Client struct {
	root, directory, batch string
	launch                 Launcher
	timeout                time.Duration
	slots                  chan struct{}
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
	var random [8]byte
	if _, e = rand.Read(random[:]); e != nil {
		return nil, e
	}
	batch := time.Now().In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02_15-04-05") + "-ssh-" + hex.EncodeToString(random[:])
	return &Client{root: o.Root, directory: directory, batch: batch, launch: o.Launcher, timeout: o.Timeout, slots: make(chan struct{}, 4)}, nil
}
func (c *Client) channel(ctx context.Context, run func(*Protocol) error) (err error) {
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.slots }()
	ch, e := c.launch(ctx)
	if e != nil {
		return e
	}
	if ch.Close != nil {
		defer func() {
			if e := ch.Close(); err == nil {
				err = e
			}
		}()
	}
	input, ok := ch.Input.(interface{ SetWriteDeadline(time.Time) error })
	if !ok || ch.Output == nil {
		return ErrArchive
	}
	output, ok := ch.Output.(interface{ SetReadDeadline(time.Time) error })
	if !ok {
		return ErrArchive
	}
	// Reject endpoints whose deadline methods exist but cannot enforce deadlines.
	deadline := time.Now().Add(time.Second)
	if input.SetWriteDeadline(deadline) != nil || output.SetReadDeadline(deadline) != nil {
		return ErrArchive
	}
	if input.SetWriteDeadline(time.Time{}) != nil || output.SetReadDeadline(time.Time{}) != nil {
		return ErrArchive
	}
	return run(NewProtocol(ch.Input, ch.Output, c.timeout))
}
func (c *Client) Prepare(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.prepared {
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
		c.prepared = true
	}
	return e
}
func (c *Client) Upload(ctx context.Context, filename, archiveName string, upload func(context.Context, string) (transfer.Receipt, error)) (receipt transfer.Receipt, err error) {
	c.mu.Lock()
	prepared := c.prepared
	c.mu.Unlock()
	if !prepared || !safeName(filename) || !safeName(archiveName) || upload == nil {
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
