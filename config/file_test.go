package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadFileMissingAndSymlinkSemantics(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "private-input")
	if data, found, err := ReadFile(t.Context(), missing, true); data != nil || found || err != nil {
		t.Fatalf("optional missing file: found=%v err=%v", found, err)
	}
	if data, found, err := ReadFile(t.Context(), missing, false); data != nil || found || !errors.Is(err, ErrInvalid) {
		t.Fatalf("required missing file: found=%v err=%v", found, err)
	} else if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private-input") {
		t.Fatal("error exposed input path")
	}
	body := []byte("# content remains unparsed here\nprivate: [\n")
	if err := os.WriteFile(missing, body, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(missing, link); err != nil {
		t.Fatal(err)
	}
	data, found, err := ReadFile(t.Context(), link, false)
	if err != nil || !found || !bytes.Equal(data, body) {
		t.Fatalf("readable symlink: found=%v err=%v", found, err)
	}
}

func TestReadFileExactBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "limit")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, MaxBytes); err != nil {
		t.Fatal(err)
	}
	data, found, err := ReadFile(t.Context(), path, false)
	if err != nil || !found || len(data) != MaxBytes {
		t.Fatalf("exact byte budget: bytes=%d found=%v err=%v", len(data), found, err)
	}
	// Enforce the read budget independently of Stat, including files that grow.
	if data, err := readContents(t.Context(), io.LimitReader(zeroReader{}, MaxBytes+1)); data != nil || !errors.Is(err, ErrInvalid) {
		t.Fatalf("over-budget contents retained: bytes=%d err=%v", len(data), err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestReadFileCanceledBeforeOpen(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{name: "canceled", ctx: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			return ctx, cancel
		}, want: context.Canceled},
		{name: "deadline", ctx: func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		}, want: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := test.ctx()
			defer cancel()
			data, found, err := ReadFile(ctx, filepath.Join(t.TempDir(), "missing"), true)
			if data != nil || found || !errors.Is(err, test.want) {
				t.Fatalf("missingOK suppressed cancellation: found=%v err=%v", found, err)
			}
		})
	}
}

type cancelRead struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelRead) Read(p []byte) (int, error) {
	r.reads++
	n := copy(p, "private partial contents")
	r.cancel()
	return n, io.EOF
}

func TestReadContentsCancellationDiscardsPartialAndFinalRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &cancelRead{cancel: cancel}
	data, err := readContents(ctx, reader)
	if data != nil || !errors.Is(err, context.Canceled) || reader.reads != 1 {
		t.Fatalf("canceled read: bytes=%d reads=%d err=%v", len(data), reader.reads, err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") {
		t.Fatal("cancellation exposed partial contents")
	}
}
