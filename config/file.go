package config

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// ReadFile reads at most MaxBytes from a regular configuration file. Readable
// symlinks are followed; directories and special files, including FIFOs, are
// rejected without waiting for a writer. If missingOK is true, a missing file
// returns (nil, false, nil); other failures never return partial contents.
// Cancellation is checked between synchronous filesystem calls and read chunks,
// not by interrupting a blocked syscall. Errors retain context cancellation for
// errors.Is and otherwise use ErrInvalid without exposing paths or file data.
func ReadFile(ctx context.Context, path string, missingOK bool) (data []byte, found bool, err error) {
	if ctx == nil {
		return nil, false, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	// NONBLOCK allows rejecting a FIFO after opening it without a writer.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return nil, false, canceled
		}
		if missingOK && errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, ErrInvalid
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() {
		if file.Close() != nil {
			err = errors.Join(err, ErrInvalid)
		}
		if canceled := ctx.Err(); canceled != nil {
			err = errors.Join(err, canceled)
		}
		if err != nil {
			data, found = nil, false
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	info, err := file.Stat()
	if canceled := ctx.Err(); canceled != nil {
		return nil, false, canceled
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return nil, false, ErrInvalid
	}
	data, err = readContents(ctx, file)
	return data, err == nil, err
}

func readContents(ctx context.Context, source io.Reader) ([]byte, error) {
	reader := io.LimitReader(source, MaxBytes+1)
	var data bytes.Buffer
	buffer := make([]byte, 32<<10)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := reader.Read(buffer)
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		if data.Len()+n > MaxBytes || err != nil && !errors.Is(err, io.EOF) {
			return nil, ErrInvalid
		}
		data.Write(buffer[:n])
		if errors.Is(err, io.EOF) {
			return data.Bytes(), nil
		}
	}
}
