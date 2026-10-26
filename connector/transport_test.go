package connector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessPTYAndReap(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	p, e := startProcess(ctx, exec.Command("/bin/sh", "-c", "printf ready; read value; printf '%s' \"$value\""))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	b := make([]byte, 5)
	if _, e = io.ReadFull(p, b); e != nil || string(b) != "ready" {
		t.Fatalf("%q %v", b, e)
	}
	if _, e = p.Write([]byte("hello\n")); e != nil {
		t.Fatal(e)
	}
	if _, e = io.ReadFull(p, b); e != nil || string(b) != "hello" {
		t.Fatalf("%q %v", b, e)
	}
	_, e = p.Read(b)
	if !errors.Is(e, io.EOF) {
		t.Fatal(e)
	}
	if e = p.Close(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-p.done:
	default:
		t.Fatal("child not reaped")
	}
}

func TestHostTrustRemovesOldKeyOnlyFromRecoverySnapshot(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if output, e := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); e != nil {
		t.Fatalf("key fixture: %v %s", e, output)
	}
	public, e := os.ReadFile(key + ".pub")
	if e != nil {
		t.Fatal(e)
	}
	known := filepath.Join(dir, "known_hosts")
	original := append([]byte("fixture "), public...)
	if e = os.WriteFile(known, original, 0600); e != nil {
		t.Fatal(e)
	}
	cfg := ConnectionConfig{Host: "fixture", KnownHosts: known, HostKeyPolicy: "replace"}
	for _, replacement := range []bool{false, true} {
		cfg.replaceHostKey = replacement
		trust, e := newHostTrust(t.Context(), cfg, 22)
		if e != nil {
			t.Fatal(e)
		}
		snapshot, e := os.ReadFile(trust.snapshot)
		if e != nil {
			t.Fatal(e)
		}
		if !replacement && !bytes.Equal(snapshot, original) {
			t.Fatal("normal connection removed trusted key")
		}
		if replacement && bytes.Contains(snapshot, public) {
			t.Fatal("recovery snapshot retained changed key")
		}
		if len(trust.original) != 1 {
			t.Fatalf("lost concurrency baseline: %v", trust.original)
		}
		trust.close()
		shared, e := os.ReadFile(known)
		if e != nil || !bytes.Equal(shared, original) {
			t.Fatal("unauthenticated recovery altered shared trust file")
		}
	}
}
