package archive

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// LocalLauncher runs the same pinned-directory transaction used by the remote
// worker. Closing a channel cancels and joins its worker before returning.
func LocalLauncher() Launcher {
	return func(ctx context.Context) (Channel, error) {
		ctx, cancel := context.WithCancel(ctx)
		client, server := net.Pipe()
		done := make(chan struct{})
		go func() { defer close(done); defer server.Close(); _ = Serve(ctx, server, server) }()
		var once sync.Once
		close := func() error { once.Do(func() { cancel(); client.Close(); <-done }); return nil }
		return Channel{Input: client, Output: client, Close: close}, nil
	}
}

var localBatchName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}(?:_\d{2,})?$`)

// reserveLocalBatch atomically chooses one server archive directory for the
// entire local pool. The reservation is never reused by another batch.
func reserveLocalBatch(ctx context.Context, root, reportDirectory string) (string, error) {
	dirs, err := openDirectories(root)
	if err != nil {
		return "", err
	}
	defer dirs.close()
	archive, err := dirs.private(dirs.root, "archive")
	if err != nil {
		return "", err
	}
	base := filepath.Base(reportDirectory)
	if !localBatchName.MatchString(base) {
		base = time.Now().In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02_15-04-05")
	}
	for suffix := 0; suffix < 10000; suffix++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		candidate := base
		if suffix != 0 {
			candidate = fmt.Sprintf("%s_%02d", base, suffix)
		}
		err := unix.Mkdirat(archive, candidate, 0700)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := unix.Fsync(archive); err != nil {
			return "", err
		}
		return candidate, nil
	}
	return "", ErrArchive
}
