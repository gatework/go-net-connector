// Package archive implements the versioned, single-transaction archive protocol.
package archive

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"golang.org/x/sys/unix"
)

const Version = 1
const ControlLimit = 16 << 10
const PayloadLimit = 64 << 20
const ChunkSize = 64 << 10

var ErrArchive = errors.New("tftp_archive_failed")
var capabilities = []string{"target_lock", "preserve_previous", "stable_sha256", "binary_delivery", "confirmed_cleanup"}

type Request struct {
	Action      string `json:"action"`
	Root        string `json:"root"`
	Filename    string `json:"filename"`
	ArchiveName string `json:"archive_name"`
	Batch       string `json:"batch"`
	SHA256      string `json:"sha256"`
}
type frame struct {
	OK           bool     `json:"ok"`
	Phase        string   `json:"phase,omitempty"`
	Error        string   `json:"error,omitempty"`
	Version      int      `json:"version,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Bytes        int64    `json:"bytes,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
	ArchivePath  string   `json:"archive_path,omitempty"`
}

func receive(r *bufio.Reader) (Request, error) {
	var req Request
	line, e := r.ReadSlice('\n')
	if e != nil || len(line) > ControlLimit {
		return req, ErrArchive
	}
	if json.Unmarshal(line, &req) != nil {
		return req, ErrArchive
	}
	return req, nil
}
func send(w io.Writer, f frame) error {
	b, e := json.Marshal(f)
	if e != nil {
		return ErrArchive
	}
	b = append(b, '\n')
	if len(b) > ControlLimit {
		return ErrArchive
	}
	return writeAll(w, b)
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, e := w.Write(b)
		if n < 0 || n > len(b) {
			return ErrArchive
		}
		b = b[n:]
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
func safeName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\r\n\x00") && len(s) <= 255
}

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
	for _, part := range strings.Split(root, "/") {
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
	for i := len(d.fds) - 1; i >= 0; i-- {
		unix.Close(d.fds[i])
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
	real, e := filepath.EvalSymlinks(d.path)
	if e != nil || real != d.path {
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
	if !safeName(name) {
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
	return fingerprint{uint64(s.Dev), uint64(s.Ino), s.Size, mtime(s), ctime(s)}
}
func readFile(ctx context.Context, parent int, name string, missing bool, consume func([]byte) error) (string, *fingerprint, error) {
	fd, e := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(e, unix.ENOENT) && missing {
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
func persist(ctx context.Context, parent int, name string, source int, sourceName string, expected fingerprint, digest string) error {
	f, pending, e := temporary(parent, ".net-connector-")
	if e != nil {
		return e
	}
	defer unix.Unlinkat(parent, pending, 0)
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
	if e = unix.Linkat(parent, pending, parent, name, 0); e != nil {
		return e
	}
	return unix.Fsync(parent)
}
func verifyLock(parent int, name string, fd int) error {
	s, e := statFD(fd)
	if e != nil || s.Mode&unix.S_IFMT != unix.S_IFREG || s.Mode&0777 != 0600 || s.Uid != uint32(os.Geteuid()) || s.Nlink != 1 || sameEntry(parent, name, fd) != nil {
		return ErrArchive
	}
	return nil
}
func lock(ctx context.Context, parent int, name string) (int, error) {
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
	ctx, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()
	for {
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

// Serve owns one transaction. Readers/writers must unblock on disconnect or context cancellation.
// A failure never removes an unconfirmed staging upload or replays a device operation.
func Serve(ctx context.Context, input io.Reader, output io.Writer) (err error) {
	deadline := time.Now().Add(280 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	var setters []func(time.Time) error
	if d, ok := input.(interface{ SetReadDeadline(time.Time) error }); ok {
		setters = append(setters, d.SetReadDeadline)
	}
	if d, ok := output.(interface{ SetWriteDeadline(time.Time) error }); ok {
		setters = append(setters, d.SetWriteDeadline)
	}
	for _, set := range setters {
		if e := set(deadline); e != nil {
			return ErrArchive
		}
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		for _, set := range setters {
			_ = set(time.Now())
		}
		close(stopped)
	})
	defer func() {
		if !stop() {
			<-stopped
		}
	}()

	defer func() {
		if err != nil {
			_ = send(output, frame{OK: false, Error: "tftp_archive_failed"})
		}
	}()
	reader := bufio.NewReaderSize(input, ControlLimit+1)
	req, e := receive(reader)
	if e != nil {
		return e
	}
	if req.Action == "probe" {
		return send(output, frame{OK: true, Phase: "ready", Version: Version, Capabilities: append([]string(nil), capabilities...)})
	}
	if !safeName(req.Filename) || !safeName(req.ArchiveName) || !regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}-[0-9]{2}-[0-9]{2}(?:_[0-9]{2,})*|[0-9_-]+-ssh-[a-f0-9]{16})$`).MatchString(req.Batch) {
		return ErrArchive
	}
	dirs, e := openDirectories(req.Root)
	if e != nil {
		return e
	}
	defer dirs.close()
	archive, e := dirs.private(dirs.root, "archive")
	if e != nil {
		return e
	}
	batch, e := dirs.private(archive, req.Batch)
	if e != nil {
		return e
	}
	locks, e := dirs.private(dirs.root, ".net-connector-locks")
	if e != nil {
		return e
	}
	markerTime := func() (int64, error) {
		f, name, e := temporary(dirs.root, ".net-connector-upload-")
		if e != nil {
			return 0, e
		}
		s, e := statFD(int(f.Fd()))
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		ue := unix.Unlinkat(dirs.root, name, 0)
		if e == nil {
			e = ce
		}
		if e == nil {
			e = ue
		}
		return mtime(s), e
	}
	if req.Action == "preflight" {
		if _, e = markerTime(); e != nil {
			return e
		}
		if e = dirs.verify(); e != nil {
			return e
		}
		return send(output, frame{OK: true, Phase: "ready", Version: Version, Capabilities: append([]string(nil), capabilities...)})
	}
	if req.Action != "" && req.Action != "upload" {
		return ErrArchive
	}
	lockName := fmt.Sprintf("%x", sha256.Sum256([]byte(norm.NFC.String(cases.Fold().String(norm.NFC.String(req.Filename))))))
	lockFD, e := lock(ctx, locks, lockName)
	if e != nil {
		return e
	}
	defer unix.Close(lockFD)
	verify := func() error {
		if e := dirs.verify(); e != nil {
			return e
		}
		return verifyLock(locks, lockName, lockFD)
	}
	if e = verify(); e != nil {
		return e
	}
	old, previous, e := readFile(ctx, dirs.root, req.Filename, true, nil)
	if e != nil {
		return e
	}
	if previous != nil {
		prior, e := dirs.private(batch, "previous")
		if e != nil {
			return e
		}
		if e = persist(ctx, prior, req.ArchiveName, dirs.root, req.Filename, *previous, old); e != nil {
			return e
		}
	}
	started, e := markerTime()
	if e != nil {
		return e
	}
	if e = verify(); e != nil {
		return e
	}
	if e = send(output, frame{OK: true, Phase: "ready"}); e != nil {
		return e
	}
	next, e := receive(reader)
	if e != nil {
		return e
	}
	if next.Action != "verify" {
		return nil
	}
	if e = verify(); e != nil {
		return e
	}
	digest, current, e := readFile(ctx, dirs.root, req.Filename, false, nil)
	if e != nil || current == nil || current.size == 0 || previous != nil && *previous == *current || current.mtime < started {
		return ErrArchive
	}
	if e = persist(ctx, batch, req.ArchiveName, dirs.root, req.Filename, *current, digest); e != nil {
		return e
	}
	archived, saved, e := readFile(ctx, batch, req.ArchiveName, false, nil)
	if e != nil || saved == nil || archived != digest || saved.size != current.size {
		return ErrArchive
	}
	if e = verify(); e != nil {
		return e
	}
	if e = send(output, frame{OK: true, Phase: "archived", Bytes: current.size, SHA256: digest, ArchivePath: filepath.Join(req.Root, "archive", req.Batch, req.ArchiveName)}); e != nil {
		return e
	}
	delivered, check, e := readFile(ctx, batch, req.ArchiveName, false, func(b []byte) error { return writeAll(output, b) })
	if e != nil || check == nil || *check != *saved || delivered != digest {
		return ErrArchive
	}
	if e = verify(); e != nil {
		return e
	}
	if e = send(output, frame{OK: true, Phase: "delivered"}); e != nil {
		return e
	}
	action, e := receive(reader)
	if e != nil {
		return e
	}
	if action.Action == "cleanup" {
		if action.SHA256 != digest {
			return ErrArchive
		}
		if e = verify(); e != nil {
			return e
		}
		uploaded, check, e := readFile(ctx, dirs.root, req.Filename, false, nil)
		if e != nil || check == nil || *check != *current || uploaded != digest {
			return ErrArchive
		}
		if e = verify(); e != nil {
			return e
		}
		if e = unix.Unlinkat(dirs.root, req.Filename, 0); e != nil {
			return e
		}
		if e = unix.Fsync(dirs.root); e != nil {
			return e
		}
		return send(output, frame{OK: true, Phase: "complete"})
	}
	return nil
}
