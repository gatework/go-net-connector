package archive

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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
		return ErrArchive
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
		return Proof{}, ErrArchive
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
	p.phase = phase
	p.last = f
	proof := Proof{Bytes: f.Bytes, SHA256: f.SHA256, ArchivePath: f.ArchivePath}
	if phase == "archived" && (proof.Bytes < 1 || proof.Bytes > PayloadLimit || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(proof.SHA256)) {
		return Proof{}, ErrArchive
	}
	return proof, nil
}

// Receive drains the full body and validates server delivery even after a local sink fails.
// A verified proof survives local persistence errors; callers must not replay upload.
func (p *Protocol) Receive(ctx context.Context, proof Proof, sink io.Writer) (Proof, error) {
	if p.phase != "archived" || proof.Bytes < 1 || proof.Bytes > PayloadLimit || sink == nil {
		return proof, ErrArchive
	}
	digest := sha256.New()
	remaining := proof.Bytes
	buffer := make([]byte, ChunkSize)
	var sinkError error
	for remaining > 0 {
		stop, e := p.bounded(ctx, true)
		if e != nil {
			return proof, errors.Join(e, sinkError)
		}
		size := min(int64(len(buffer)), remaining)
		n, e := io.ReadFull(p.reader, buffer[:size])
		stop()
		if e != nil {
			return proof, errors.Join(ErrArchive, sinkError)
		}
		digest.Write(buffer[:n])
		remaining -= int64(n)
		if sinkError == nil {
			sinkError = writeAll(sink, buffer[:n])
		}
	}
	if _, e := p.readPhase(ctx, "delivered"); e != nil {
		return proof, errors.Join(e, sinkError)
	}
	if hex.EncodeToString(digest.Sum(nil)) != proof.SHA256 {
		return proof, errors.Join(ErrArchive, sinkError)
	}
	proof.Verified = true
	return proof, sinkError
}
