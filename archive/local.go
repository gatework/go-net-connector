package archive

import (
	"context"
	"net"
	"sync"
	"time"
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

// reserveLocalBatch prepares the shared daily directory without deleting history.
func reserveLocalBatch(ctx context.Context, root, reportDirectory string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dirs, err := openDirectories(root)
	if err != nil {
		return "", err
	}
	defer dirs.close()
	archive, err := dirs.private(dirs.root, "archive")
	if err != nil {
		return "", err
	}
	day := time.Now().In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02")
	if _, err := dirs.private(archive, day); err != nil {
		return "", err
	}
	return day, nil
}
