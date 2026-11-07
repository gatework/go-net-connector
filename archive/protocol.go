package archive

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"time"
)

type Proof struct {
	Bytes               int64
	SHA256, ArchivePath string
	Verified            bool
}
type Protocol struct {
	last     frame
	input    io.Writer
	output   io.Reader
	reader   *bufio.Reader
	timeout  time.Duration
	deadline time.Time
	phase    string
}

var archiveHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func NewProtocol(input io.Writer, output io.Reader, timeout time.Duration) *Protocol {
	if timeout <= 0 {
		timeout = 280 * time.Second
	}
	return &Protocol{input: input, output: output, reader: bufio.NewReaderSize(output, ControlLimit+1), timeout: timeout}
}
func (p *Protocol) bounded(ctx context.Context, read bool) (func(), error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	deadline := p.deadline
	if deadline.IsZero() {
		deadline = time.Now().Add(p.timeout)
	}
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if time.Now().After(deadline) {
		return nil, context.DeadlineExceeded
	}
	var set func(time.Time) error
	if read {
		if x, ok := p.output.(interface{ SetReadDeadline(time.Time) error }); ok {
			set = x.SetReadDeadline
		}
	} else {
		if x, ok := p.input.(interface{ SetWriteDeadline(time.Time) error }); ok {
			set = x.SetWriteDeadline
		}
	}
	if set == nil {
		return nil, ErrArchive
	}
	if e := set(deadline); e != nil {
		return nil, e
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = set(time.Now()); close(done) })
	return func() {
		if !stop() {
			<-done
		}
	}, nil
}
func (p *Protocol) Send(ctx context.Context, request Request) error {
	stop, e := p.bounded(ctx, false)
	if e != nil {
		return e
	}
	defer stop()
	b, e := json.Marshal(request)
	if e != nil || len(b)+1 > ControlLimit {
		return ErrArchive
	}
	if e = writeAll(p.input, append(b, '\n')); e != nil {
		return protocolFailure(ctx, e)
	}
	return ctx.Err()
}
func (p *Protocol) Expect(ctx context.Context, phase string) (Proof, error) {
	p.deadline = time.Now().Add(p.timeout)
	return p.readPhase(ctx, phase)
}
func (p *Protocol) readPhase(ctx context.Context, phase string) (Proof, error) {
	stop, e := p.bounded(ctx, true)
	if e != nil {
		return Proof{}, e
	}
	defer stop()
	line, e := p.reader.ReadSlice('\n')
	if e != nil || len(line) > ControlLimit {
		return Proof{}, protocolFailure(ctx, e)
	}
	if e = ctx.Err(); e != nil {
		return Proof{}, e
	}
	if time.Now().After(p.deadline) {
		return Proof{}, context.DeadlineExceeded
	}
	var f frame
	if json.Unmarshal(line, &f) != nil || !f.OK || f.Phase != phase {
		return Proof{}, ErrArchive
	}
	proof := Proof{Bytes: f.Bytes, SHA256: f.SHA256, ArchivePath: f.ArchivePath}
	if phase == "archived" && (proof.Bytes < 1 || proof.Bytes > PayloadLimit || proof.ArchivePath == "" || !archiveHashPattern.MatchString(proof.SHA256)) {
		return Proof{}, ErrArchive
	}
	p.phase = phase
	p.last = f
	return proof, nil
}

// Receive drains the full body and validates server delivery even after a local sink fails.
// A verified proof survives local persistence errors; callers must not replay upload.
func (p *Protocol) Receive(ctx context.Context, proof Proof, sink io.Writer) (Proof, error) {
	proof.Verified = false
	if p.phase != "archived" || p.last.Phase != "archived" || proof.Bytes < 1 || proof.Bytes > PayloadLimit || sink == nil || proof.Bytes != p.last.Bytes || proof.SHA256 != p.last.SHA256 || proof.ArchivePath != p.last.ArchivePath {
		return proof, ErrArchive
	}
	archivedFrame := p.last
	p.phase = "receiving"
	fail := func(err error) (Proof, error) {
		p.phase = "failed"
		proof.Verified = false
		return proof, err
	}
	digest := sha256.New()
	remaining := proof.Bytes
	buffer := make([]byte, ChunkSize)
	var sinkError error
	for remaining > 0 {
		stop, e := p.bounded(ctx, true)
		if e != nil {
			return fail(errors.Join(e, sinkError))
		}
		size := min(int64(len(buffer)), remaining)
		n, e := io.ReadFull(p.reader, buffer[:size])
		stop()
		if e != nil {
			return fail(protocolFailure(ctx, errors.Join(e, sinkError)))
		}
		digest.Write(buffer[:n])
		remaining -= int64(n)
		if sinkError == nil {
			sinkError = writeAll(sink, buffer[:n])
		}
	}
	if _, e := p.readPhase(ctx, "delivered"); e != nil {
		return fail(errors.Join(e, sinkError))
	}
	if hex.EncodeToString(digest.Sum(nil)) != proof.SHA256 {
		p.last = archivedFrame
		return fail(errors.Join(ErrArchive, sinkError))
	}
	proof.Verified = true
	return proof, sinkError
}

// Error reports safe archive metadata while preserving all underlying causes.
type Error struct{ cause error }

func (e *Error) Error() string    { return "archive: operation failed" }
func (e *Error) GoString() string { return e.Error() }
func (e *Error) Unwrap() error    { return e.cause }
func protocolFailure(ctx context.Context, cause error) error {
	var deadline error
	if errors.Is(cause, os.ErrDeadlineExceeded) {
		deadline = context.DeadlineExceeded
	}
	return &Error{cause: errors.Join(ErrArchive, ctx.Err(), deadline, cause)}
}
