package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/sys/unix"

	"github.com/gatework/go-net-connector/internal/batchreport"
)

// ErrBatchEvidence means the selected batch cannot establish the requested file.
var ErrBatchEvidence = errors.New("storage: invalid batch evidence")

// BatchArtifact identifies the verified file without changing the batch outcome.
// Status can retain a late cleanup failure even when the file is intact.
type BatchArtifact struct {
	Host   string `json:"host"`
	Batch  string `json:"batch"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	Status string `json:"status"`
}

// ReadBatchSaved reads only the explicitly selected directory. A stored absolute
// path supplies its basename, never an external location, so moved batches work.
func ReadBatchSaved(ctx context.Context, directory, host string) ([]byte, BatchArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, BatchArtifact{}, err
	}
	if strings.TrimSpace(directory) == "" {
		return nil, BatchArtifact{}, ErrBatchEvidence
	}
	canonicalHost, err := batchreport.NormalizeHost(host)
	if err != nil {
		return nil, BatchArtifact{}, ErrBatchEvidence
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return nil, BatchArtifact{}, err
	}
	root, err := openBatchRoot(directory)
	if err != nil {
		return nil, BatchArtifact{}, err
	}
	defer root.Close()
	data, err := readBatchFile(ctx, root, "report.json", 128<<20)
	if err != nil {
		return nil, BatchArtifact{}, err
	}
	selected, err := batchreport.SelectArtifact(data, canonicalHost)
	if err != nil {
		return nil, BatchArtifact{}, ErrBatchEvidence
	}
	name, err := batchBasename(selected.Path)
	if err != nil {
		return nil, BatchArtifact{}, err
	}
	body, err := readArtifactBody(ctx, root, selected.Subdirectory, name)
	if err != nil {
		return nil, BatchArtifact{}, err
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != selected.Bytes || !strings.EqualFold(hex.EncodeToString(sum[:]), selected.SHA256) {
		return nil, BatchArtifact{}, ErrBatchEvidence
	}
	artifact := BatchArtifact{Host: selected.Host, Batch: directory, Path: filepath.Join(directory, selected.Subdirectory, name), Bytes: selected.Bytes, SHA256: hex.EncodeToString(sum[:]), Status: selected.Status}
	return body, artifact, nil
}

// openBatchRoot opens directory as a root, refusing a symlink or a
// directory replaced between the check and the open.
func openBatchRoot(directory string) (*os.Root, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsafe
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err == nil && !os.SameFile(info, opened) {
		err = ErrUnsafe
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

// readArtifactBody reads the named artifact from the batch root or its
// subdirectory without following symlinks.
func readArtifactBody(ctx context.Context, root *os.Root, subdirectory, name string) ([]byte, error) {
	if subdirectory != "tftp" {
		return readBatchFile(ctx, root, name, batchreport.MaxArtifactBytes)
	}
	// Anchor the intermediate directory too: NOFOLLOW on tftp/name alone
	// would still follow an inward-pointing tftp directory symlink.
	dir, err := root.OpenFile("tftp", os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	return readBatchContents(ctx, file, batchreport.MaxArtifactBytes)
}

// batchBasename returns the file name a stored artifact path refers to,
// rejecting traversal and control characters.
func batchBasename(path string) (string, error) {
	if path == "" || strings.Contains(path, "\\") || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return "", ErrBatchEvidence
	}
	if slices.Contains(strings.Split(filepath.ToSlash(path), "/"), "..") {
		return "", ErrBatchEvidence
	}
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || !filepath.IsAbs(path) && name != path {
		return "", ErrBatchEvidence
	}
	return name, nil
}

// readBatchFile anchors all resolution to the opened batch and refuses symlinks.
func readBatchFile(ctx context.Context, root *os.Root, name string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBatchContents(ctx, f, limit)
}

func readBatchContents(ctx context.Context, f *os.File, limit int64) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrUnsafe
	}
	if info.Size() > limit {
		return nil, ErrLimit
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, r: f}, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrLimit
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, ErrBatchEvidence
	}
	return data, ctx.Err()
}

// ExportBatchSaved preserves bytes and returns an independent destination receipt.
func ExportBatchSaved(ctx context.Context, directory, host, destination string) (BatchArtifact, Receipt, error) {
	body, artifact, err := ReadBatchSaved(ctx, directory, host)
	if err != nil {
		return BatchArtifact{}, Receipt{}, err
	}
	if err = PrepareDirectory(filepath.Dir(destination)); err != nil {
		return artifact, Receipt{}, err
	}
	receipt, err := Write(ctx, destination, body)
	return artifact, receipt, err
}
