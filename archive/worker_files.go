package archive

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type directoryLink struct {
	parent int
	name   string
	fd     int
}
type directories struct {
	root  int
	path  string
	fds   []int
	links []directoryLink
}

func openDirectories(root string) (*directories, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, ErrArchive
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	for part := range strings.SplitSeq(root, "/") {
		if part == "" {
			continue
		}
		child, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = child
	}
	return &directories{root: fd, path: root, fds: []int{fd}}, nil
}
func (d *directories) close() {
	for _, v := range slices.Backward(d.fds) {
		unix.Close(v)
	}
}
func sameEntry(parent int, name string, fd int) error {
	var a, b unix.Stat_t
	if unix.Fstat(fd, &a) != nil || unix.Fstatat(parent, name, &b, unix.AT_SYMLINK_NOFOLLOW) != nil || a.Dev != b.Dev || a.Ino != b.Ino || b.Mode&unix.S_IFMT == unix.S_IFLNK {
		return ErrArchive
	}
	return nil
}
func (d *directories) verify() error {
	if sameEntry(unix.AT_FDCWD, d.path, d.root) != nil {
		return ErrArchive
	}
	resolved, e := filepath.EvalSymlinks(d.path)
	if e != nil || resolved != d.path {
		return ErrArchive
	}
	for _, link := range d.links {
		var s unix.Stat_t
		if unix.Fstat(link.fd, &s) != nil || s.Mode&unix.S_IFMT != unix.S_IFDIR || s.Uid != uint32(os.Geteuid()) || s.Mode&0777 != 0700 || sameEntry(link.parent, link.name, link.fd) != nil {
			return ErrArchive
		}
	}
	return nil
}
func (d *directories) private(parent int, name string) (int, error) {
	if !isSafeName(name) {
		return -1, ErrArchive
	}
	e := unix.Mkdirat(parent, name, 0700)
	if e != nil && !errors.Is(e, unix.EEXIST) {
		return -1, e
	}
	if e == nil {
		if e = unix.Fsync(parent); e != nil {
			return -1, e
		}
	}
	fd, e := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return -1, e
	}
	d.fds = append(d.fds, fd)
	d.links = append(d.links, directoryLink{parent, name, fd})
	return fd, d.verify()
}

type fingerprint struct {
	dev, ino           uint64
	size, mtime, ctime int64
}

func statFD(fd int) (unix.Stat_t, error) { var s unix.Stat_t; e := unix.Fstat(fd, &s); return s, e }
func fingerprintOf(s unix.Stat_t) fingerprint {
	return fingerprint{deviceID(s), s.Ino, s.Size, mtime(s), ctime(s)}
}
func readFile(ctx context.Context, parent int, name string, missingOK bool, consume func([]byte) error) (string, *fingerprint, error) {
	fd, e := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(e, unix.ENOENT) && missingOK {
		return "", nil, nil
	}
	if e != nil {
		return "", nil, e
	}
	f := os.NewFile(uintptr(fd), "archive-source")
	defer f.Close()
	before, e := statFD(fd)
	if e != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Size > PayloadLimit {
		return "", nil, ErrArchive
	}
	digest := sha256.New()
	var size int64
	buf := make([]byte, ChunkSize)
	for {
		if e = ctx.Err(); e != nil {
			return "", nil, e
		}
		n, e := f.Read(buf)
		if n > 0 {
			size += int64(n)
			if size > PayloadLimit {
				return "", nil, ErrArchive
			}
			digest.Write(buf[:n])
			if consume != nil {
				if e := consume(buf[:n]); e != nil {
					return "", nil, e
				}
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", nil, e
		}
	}
	after, e := statFD(fd)
	a, b := fingerprintOf(before), fingerprintOf(after)
	if e != nil || a != b || size != after.Size || sameEntry(parent, name, fd) != nil {
		return "", nil, ErrArchive
	}
	var current unix.Stat_t
	if unix.Fstatat(parent, name, &current, unix.AT_SYMLINK_NOFOLLOW) != nil || fingerprintOf(current) != a {
		return "", nil, ErrArchive
	}
	return hex.EncodeToString(digest.Sum(nil)), &a, nil
}
func temporary(parent int, prefix string) (*os.File, string, error) {
	for {
		var bytes [16]byte
		if _, e := rand.Read(bytes[:]); e != nil {
			return nil, "", e
		}
		name := prefix + hex.EncodeToString(bytes[:])
		fd, e := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if errors.Is(e, unix.EEXIST) {
			continue
		}
		if e != nil {
			return nil, "", e
		}
		return os.NewFile(uintptr(fd), "archive-temporary"), name, nil
	}
}
func dailyBatch(batch string) bool {
	v, e := time.Parse("2006-01-02", batch)
	return e == nil && v.Format("2006-01-02") == batch
}
func persistMode(ctx context.Context, parent int, name string, source int, sourceName string, expected fingerprint, digest string, replace bool) error {
	f, pending, e := temporary(parent, ".net-connector-")
	if e != nil {
		return e
	}
	defer func() { _ = unix.Unlinkat(parent, pending, 0) }() // best-effort removal of the pending name
	copied, current, e := readFile(ctx, source, sourceName, false, func(b []byte) error { return writeAll(f, b) })
	if e == nil && (current == nil || *current != expected || copied != digest) {
		e = ErrArchive
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if replace {
		if _, _, e = readFile(ctx, parent, name, true, nil); e != nil {
			return e
		}
		e = unix.Renameat(parent, pending, parent, name)
	} else {
		e = unix.Linkat(parent, pending, parent, name, 0)
	}
	if e != nil {
		return e
	}
	if e = unix.Fsync(parent); e != nil {
		return e
	}
	return ctx.Err()
}
func verifyLock(parent int, name string, fd int) error {
	s, e := statFD(fd)
	if e != nil || s.Mode&unix.S_IFMT != unix.S_IFREG || s.Mode&0777 != 0600 || s.Uid != uint32(os.Geteuid()) || s.Nlink != 1 || sameEntry(parent, name, fd) != nil {
		return ErrArchive
	}
	return nil
}
func lockArchiveTarget(ctx context.Context, parent int, name string) (int, error) {
	fd, e := unix.Openat(parent, name, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_CREAT|unix.O_EXCL, 0600)
	if errors.Is(e, unix.EEXIST) {
		fd, e = unix.Openat(parent, name, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	}
	if e != nil {
		return -1, e
	}
	if e = verifyLock(parent, name, fd); e != nil {
		unix.Close(fd)
		return -1, e
	}
	for {
		if e := ctx.Err(); e != nil {
			unix.Close(fd)
			return -1, e
		}
		e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			return fd, nil
		}
		if !errors.Is(e, unix.EWOULDBLOCK) && !errors.Is(e, unix.EAGAIN) {
			unix.Close(fd)
			return -1, e
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			unix.Close(fd)
			return -1, ctx.Err()
		case <-timer.C:
		}
	}
}
