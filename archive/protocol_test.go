package archive

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

type failedWriter struct{ err error }

func (w failedWriter) Write([]byte) (int, error) { return 0, w.err }
func TestProtocolDrainsAfterSinkFailureAndChecksDigest(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	data := []byte("config\x00binary\n")
	go func() {
		defer s.Close()
		json.NewEncoder(s).Encode(map[string]any{"version": Version, "ok": true, "phase": "archived", "bytes": len(data), "sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "archive_path": "/tftp/archive/batch/file"})
		s.Write(data)
		json.NewEncoder(s).Encode(map[string]any{"version": Version, "ok": true, "phase": "delivered"})
	}()
	p := NewProtocol(c, c)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	proof, e := p.Expect(ctx, "archived")
	if e != nil {
		t.Fatal(e)
	}
	primary := errors.New("disk failed")
	got, e := p.Receive(ctx, proof, failedWriter{primary})
	if !errors.Is(e, primary) || !got.Verified {
		t.Fatal(got, e)
	}
}
func TestProtocolDeadlineDoesNotResetForBodyAndTrailer(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()
	go func() {
		json.NewEncoder(s).Encode(map[string]any{"version": Version, "ok": true, "phase": "archived", "bytes": 1, "sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("x"))), "archive_path": "/archive/config.cfg"})
		time.Sleep(350 * time.Millisecond)
		s.Write([]byte("x"))
		time.Sleep(300 * time.Millisecond)
		json.NewEncoder(s).Encode(map[string]any{"version": Version, "ok": true, "phase": "delivered"})
	}()
	p := NewProtocol(c, c)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	proof, e := p.Expect(ctx, "archived")
	if e != nil {
		t.Fatal(e)
	}
	_, e = p.Receive(ctx, proof, io.Discard)
	if e == nil {
		t.Fatal("deadline reset")
	}
}
