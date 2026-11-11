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

const Version = 2
const ControlLimit = 16 << 10
const PayloadLimit = 64 << 20
const ChunkSize = 64 << 10

var ErrArchive = errors.New("tftp_archive_failed")
var capabilities = []string{"target_lock", "preserve_previous", "stable_sha256", "binary_delivery", "confirmed_cleanup", "daily_latest", "stage_timeouts"}
var archiveBatchPattern = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}-[0-9]{2}-[0-9]{2}(?:_[0-9]{2,})*|[0-9_-]+-ssh-[a-f0-9]{16})$`)

// Serve owns one transaction. Readers/writers must unblock on disconnect or context cancellation.
// A failure never removes an unconfirmed staging upload or replays a device operation.
func Serve(ctx context.Context, input io.Reader, output io.Writer) (err error) {
	return serve(ctx, input, output, controlTimeout)
}

func serve(parent context.Context, input io.Reader, output io.Writer, control time.Duration) (err error) {
	clock := newStageClock(parent, input, output)
	defer clock.stop()
	if e := clock.stage(control); e != nil {
		return e
	}
	defer func() {
		if err != nil && clock.ctx.Err() == nil {
			_ = writeFrame(output, frame{OK: false, Error: "tftp_archive_failed"})
		}
	}()
	w := &worker{clock: clock, output: output, reader: bufio.NewReaderSize(input, ControlLimit+1), control: control, lockFD: -1}
	defer w.close()
	return w.run()
}

// stageClock bounds each protocol stage with a deadline that cancels the
// stage context and expires the transport's read and write deadlines.
type stageClock struct {
	parent  context.Context //nolint:containedctx // every stage deadline derives from the transaction context
	ctx     context.Context //nolint:containedctx // the current stage context, replaced at each stage
	setters []func(time.Time) error
	finish  func()
}

// newStageClock returns a clock for parent that also drives the deadlines
// of input and output when they support them.
func newStageClock(parent context.Context, input io.Reader, output io.Writer) *stageClock {
	c := &stageClock{parent: parent, ctx: parent, finish: func() {}}
	if d, ok := input.(interface{ SetReadDeadline(t time.Time) error }); ok {
		c.setters = append(c.setters, d.SetReadDeadline)
	}
	if d, ok := output.(interface{ SetWriteDeadline(t time.Time) error }); ok {
		c.setters = append(c.setters, d.SetWriteDeadline)
	}
	return c
}

// stage starts a stage lasting timeout unless the current one has ended.
func (c *stageClock) stage(timeout time.Duration) error {
	if e := c.ctx.Err(); e != nil {
		return e
	}
	return c.until(time.Now().Add(timeout))
}

// until replaces the current stage with one ending at deadline.
func (c *stageClock) until(deadline time.Time) error {
	c.finish()
	// A setter can fail before the new cancellation callback is installed.
	// Never retain an already-stopped callback whose done channel cannot close.
	c.finish = func() {}
	ctx, cancel := context.WithDeadline(c.parent, deadline)
	c.ctx = ctx
	deadline, _ = ctx.Deadline()
	for _, set := range c.setters {
		if e := set(deadline); e != nil {
			cancel()
			return e
		}
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		for _, set := range c.setters {
			_ = set(time.Now())
		}
		close(stopped)
	})
	c.finish = func() {
		if !stop() {
			<-stopped
		}
		cancel()
	}
	return ctx.Err()
}

// stop ends the current stage and waits for its deadline callback.
func (c *stageClock) stop() { c.finish() }

// worker serves one archive transaction over a control stream.
type worker struct {
	clock    *stageClock
	output   io.Writer
	reader   *bufio.Reader
	control  time.Duration
	req      Request
	daily    bool
	dirs     *directories
	batch    int
	locks    int
	lockName string
	lockFD   int
}

// run reads the opening request and dispatches it.
func (w *worker) run() error {
	req, e := readRequest(w.reader)
	if e != nil {
		return e
	}
	if req.Action == "probe" {
		return w.writeReady()
	}
	w.req = req
	var timeouts Timeouts
	if req.Action != "preflight" {
		if req.Timeouts == nil {
			return ErrArchive
		}
		if timeouts, e = req.Timeouts.decode(); e != nil {
			return e
		}
		if e = w.clock.stage(timeouts.Prepare); e != nil {
			return e
		}
	}
	if e = w.open(); e != nil {
		return e
	}
	if req.Action == "preflight" {
		if _, e = w.markerTime(); e != nil {
			return e
		}
		if e = w.dirs.verify(); e != nil {
			return e
		}
		return w.writeReady()
	}
	if req.Action != "" && req.Action != "upload" {
		return ErrArchive
	}
	return w.transfer(timeouts)
}

// close releases the target lock and the directories.
func (w *worker) close() {
	if w.lockFD >= 0 {
		unix.Close(w.lockFD)
	}
	if w.dirs != nil {
		w.dirs.close()
	}
}

// writeReady announces the protocol version and capabilities.
func (w *worker) writeReady() error {
	return writeFrame(w.output, frame{OK: true, Phase: "ready", Version: Version, Capabilities: append([]string(nil), capabilities...)})
}

// open validates the request names and opens the root, archive, batch and
// lock directories.
func (w *worker) open() error {
	req := w.req
	w.daily = dailyBatch(req.Batch)
	if !isSafeName(req.Filename) || !isSafeName(req.ArchiveName) || !w.daily && !archiveBatchPattern.MatchString(req.Batch) {
		return ErrArchive
	}
	dirs, e := openDirectories(req.Root)
	if e != nil {
		return e
	}
	w.dirs = dirs
	archive, e := dirs.private(dirs.root, "archive")
	if e != nil {
		return e
	}
	if w.batch, e = dirs.private(archive, req.Batch); e != nil {
		return e
	}
	w.locks, e = dirs.private(dirs.root, ".net-connector-locks")
	return e
}

// markerTime returns the file system's current modification time by
// creating and removing a marker file in the root.
func (w *worker) markerTime() (int64, error) {
	f, name, e := temporary(w.dirs.root, ".net-connector-upload-")
	if e != nil {
		return 0, e
	}
	s, e := statFD(int(f.Fd()))
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	ue := unix.Unlinkat(w.dirs.root, name, 0)
	if e == nil {
		e = ce
	}
	if e == nil {
		e = ue
	}
	return mtime(s), e
}

// verify confirms the stage is live, the directories are unchanged and the
// target lock is still held.
func (w *worker) verify() error {
	if e := w.clock.ctx.Err(); e != nil {
		return e
	}
	if e := w.dirs.verify(); e != nil {
		return e
	}
	return verifyLock(w.locks, w.lockName, w.lockFD)
}

// transfer runs an upload transaction: prepare, ready, verify and archive,
// deliver, then cleanup or abort.
func (w *worker) transfer(timeouts Timeouts) error {
	previous, started, e := w.prepareTarget()
	if e != nil {
		return e
	}
	// Once ready is published the client owns a full upload window. Neither a
	// slow ready write nor scheduling after delivery may consume that window.
	// The client enforces the upload budget; the worker keeps a bounded watchdog.
	if e = writeFrame(w.output, frame{OK: true, Phase: "ready"}); e != nil {
		return e
	}
	if e = w.clock.until(time.Now().Add(timeouts.Upload + w.control)); e != nil {
		return e
	}
	next, e := readRequest(w.reader)
	if e != nil {
		return e
	}
	if next.Action != "verify" {
		if next.Action == "abort" {
			return nil
		}
		return ErrArchive
	}
	if e = w.clock.stage(timeouts.Verify); e != nil {
		return e
	}
	current, digest, e := w.archiveUpload(previous, started)
	if e != nil {
		return e
	}
	verifyDeadline, _ := w.clock.ctx.Deadline()
	if e = writeFrame(w.output, frame{OK: true, Phase: "delivered"}); e != nil {
		return e
	}
	// Local fsync/rename/directory sync still belong to verification. Only the
	// next control frame gets handoff allowance; no file work uses this margin.
	if e = w.clock.until(verifyDeadline.Add(w.control)); e != nil {
		return e
	}
	action, e := readRequest(w.reader)
	if e != nil {
		return e
	}
	switch action.Action {
	case "cleanup":
		if e = w.clock.stage(timeouts.Cleanup); e != nil {
			return e
		}
		return w.cleanup(action, current, digest)
	case "abort":
		return nil
	default:
		return ErrArchive
	}
}

// prepareTarget locks the upload target, preserves any previous file and
// returns its fingerprint with the time the upload window opens.
func (w *worker) prepareTarget() (previous *fingerprint, started int64, err error) {
	ctx, req := w.clock.ctx, w.req
	w.lockName = fmt.Sprintf("%x", sha256.Sum256([]byte(norm.NFC.String(cases.Fold().String(norm.NFC.String(req.Filename))))))
	if w.lockFD, err = lockArchiveTarget(ctx, w.locks, w.lockName); err != nil {
		w.lockFD = -1
		return nil, 0, err
	}
	if err = w.verify(); err != nil {
		return nil, 0, err
	}
	old, previous, err := readFile(ctx, w.dirs.root, req.Filename, true, nil)
	if err != nil {
		return nil, 0, err
	}
	if previous != nil {
		prior, e := w.dirs.private(w.batch, "previous")
		if e != nil {
			return nil, 0, e
		}
		if e = persistMode(ctx, prior, req.ArchiveName, w.dirs.root, req.Filename, *previous, old, w.daily); e != nil {
			return nil, 0, e
		}
	}
	if started, err = w.markerTime(); err != nil {
		return nil, 0, err
	}
	return previous, started, w.verify()
}

// archiveUpload checks the uploaded file is new, archives it into the
// batch and streams the archived copy back to the client.
func (w *worker) archiveUpload(previous *fingerprint, started int64) (current *fingerprint, digest string, err error) {
	ctx, req := w.clock.ctx, w.req
	if err = w.verify(); err != nil {
		return nil, "", err
	}
	digest, current, err = readFile(ctx, w.dirs.root, req.Filename, false, nil)
	if err != nil || current == nil || current.size == 0 || previous != nil && *previous == *current || current.mtime < started {
		return nil, "", ErrArchive
	}
	if err = persistMode(ctx, w.batch, req.ArchiveName, w.dirs.root, req.Filename, *current, digest, w.daily); err != nil {
		return nil, "", err
	}
	archived, saved, err := readFile(ctx, w.batch, req.ArchiveName, false, nil)
	if err != nil || saved == nil || archived != digest || saved.size != current.size {
		return nil, "", ErrArchive
	}
	if err = w.verify(); err != nil {
		return nil, "", err
	}
	if err = writeFrame(w.output, frame{OK: true, Phase: "archived", Bytes: current.size, SHA256: digest, ArchivePath: filepath.Join(req.Root, "archive", req.Batch, req.ArchiveName)}); err != nil {
		return nil, "", err
	}
	delivered, check, err := readFile(ctx, w.batch, req.ArchiveName, false, func(b []byte) error { return writeAll(w.output, b) })
	if err != nil || check == nil || *check != *saved || delivered != digest {
		return nil, "", ErrArchive
	}
	return current, digest, w.verify()
}

// cleanup removes the staging upload once the client confirms the digest
// and the file is still the one that was archived.
func (w *worker) cleanup(action Request, current *fingerprint, digest string) error {
	if action.SHA256 != digest {
		return ErrArchive
	}
	if e := w.verify(); e != nil {
		return e
	}
	uploaded, check, e := readFile(w.clock.ctx, w.dirs.root, w.req.Filename, false, nil)
	if e != nil || check == nil || *check != *current || uploaded != digest {
		return ErrArchive
	}
	if e = w.verify(); e != nil {
		return e
	}
	if e = unix.Unlinkat(w.dirs.root, w.req.Filename, 0); e != nil {
		return e
	}
	if e = unix.Fsync(w.dirs.root); e != nil {
		return e
	}
	if e = w.clock.ctx.Err(); e != nil {
		return e
	}
	return writeFrame(w.output, frame{OK: true, Phase: "complete"})
}
