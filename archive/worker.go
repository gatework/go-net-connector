// Package archive implements the versioned, single-transaction archive protocol.
package archive

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const Version = 1
const ControlLimit = 16 << 10
const PayloadLimit = 64 << 20
const ChunkSize = 64 << 10

var ErrArchive = errors.New("tftp_archive_failed")
var capabilities = []string{"target_lock", "preserve_previous", "stable_sha256", "binary_delivery", "confirmed_cleanup", "daily_latest"}
var archiveBatchPattern = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}-[0-9]{2}-[0-9]{2}(?:_[0-9]{2,})*|[0-9_-]+-ssh-[a-f0-9]{16})$`)

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
			_ = writeFrame(output, frame{OK: false, Error: "tftp_archive_failed"})
		}
	}()
	reader := bufio.NewReaderSize(input, ControlLimit+1)
	req, e := readRequest(reader)
	if e != nil {
		return e
	}
	if req.Action == "probe" {
		return writeFrame(output, frame{OK: true, Phase: "ready", Version: Version, Capabilities: append([]string(nil), capabilities...)})
	}
	daily := dailyBatch(req.Batch)
	if !isSafeName(req.Filename) || !isSafeName(req.ArchiveName) || !daily && !archiveBatchPattern.MatchString(req.Batch) {
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
		return writeFrame(output, frame{OK: true, Phase: "ready", Version: Version, Capabilities: append([]string(nil), capabilities...)})
	}
	if req.Action != "" && req.Action != "upload" {
		return ErrArchive
	}
	lockName := fmt.Sprintf("%x", sha256.Sum256([]byte(norm.NFC.String(cases.Fold().String(norm.NFC.String(req.Filename))))))
	lockFD, e := lockArchiveTarget(ctx, locks, lockName)
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
		if e = persistMode(ctx, prior, req.ArchiveName, dirs.root, req.Filename, *previous, old, daily); e != nil {
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
	if e = writeFrame(output, frame{OK: true, Phase: "ready"}); e != nil {
		return e
	}
	next, e := readRequest(reader)
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
	if e = persistMode(ctx, batch, req.ArchiveName, dirs.root, req.Filename, *current, digest, daily); e != nil {
		return e
	}
	archived, saved, e := readFile(ctx, batch, req.ArchiveName, false, nil)
	if e != nil || saved == nil || archived != digest || saved.size != current.size {
		return ErrArchive
	}
	if e = verify(); e != nil {
		return e
	}
	if e = writeFrame(output, frame{OK: true, Phase: "archived", Bytes: current.size, SHA256: digest, ArchivePath: filepath.Join(req.Root, "archive", req.Batch, req.ArchiveName)}); e != nil {
		return e
	}
	delivered, check, e := readFile(ctx, batch, req.ArchiveName, false, func(b []byte) error { return writeAll(output, b) })
	if e != nil || check == nil || *check != *saved || delivered != digest {
		return ErrArchive
	}
	if e = verify(); e != nil {
		return e
	}
	if e = writeFrame(output, frame{OK: true, Phase: "delivered"}); e != nil {
		return e
	}
	action, e := readRequest(reader)
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
		return writeFrame(output, frame{OK: true, Phase: "complete"})
	}
	return nil
}
