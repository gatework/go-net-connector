package connector

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	expect "github.com/gatework/go-expect"
	"github.com/gatework/go-net-connector/internal/terminal"
	"github.com/gatework/go-net-connector/internal/textcodec"
)

// Device owns one conversation. Close is terminal and cancels admitted work.
// A failed operation requires explicit Connect before another command; no replay.
// Do not copy Device. Callbacks and borrowed writers must cooperate with cancellation.
type Device struct {
	lifeMu     sync.Mutex
	closed     bool
	cancel     context.CancelFunc
	retired    atomic.Bool
	profile    Profile
	config     ConnectionConfig
	factory    TransportFactory
	lease      atomic.Bool
	connected  atomic.Bool
	privileged atomic.Bool
	haState    atomic.Value
	prompt     atomic.Value
	session    *expect.Session
	transport  io.ReadWriteCloser
	owner      *closeOwner
	log        *logWriter
}

func New(profile Profile, cfg ConnectionConfig, opts ...Option) (*Device, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	p, e := profile.clone()
	if e != nil {
		return nil, e
	}
	if cfg.LineEnding == "" {
		cfg.LineEnding = "\n"
	}
	if cfg.Protocol == "" {
		cfg.Protocol = ProtocolSSH
	}
	if cfg.HostKeyPolicy == "" {
		cfg.HostKeyPolicy = HostKeyStrict
	}
	if cfg.LoginTimeout == 0 {
		cfg.LoginTimeout = 10 * time.Second
	}
	if cfg.CommandTimeout == 0 {
		cfg.CommandTimeout = p.CommandTimeout
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	if cfg.MaxOutputBytes == 0 {
		cfg.MaxOutputBytes = 32 << 20
	}
	if e = validateInteractions(cfg.Challenges); e != nil {
		return nil, e
	}
	cfg.Challenges = cloneInteractions(cfg.Challenges)
	cfg.LegacySSHArguments = append([]string(nil), cfg.LegacySSHArguments...)
	o := options{factory: openProcess}
	for _, opt := range opts {
		if opt == nil {
			return nil, ErrInvalid
		}
		if e = opt(&o); e != nil {
			return nil, e
		}
	}
	d := &Device{profile: p, config: cfg, factory: o.factory}
	d.prompt.Store(promptState{err: ErrIncomplete})
	d.haState.Store("")
	d.log = &logWriter{target: cfg.Logger, level: cfg.LogLevel, host: cfg.Host, encoding: "auto", paused: true}
	return d, nil
}
func (d *Device) ProfileName() string { return d.profile.Name }
func (d *Device) Host() string        { return d.config.Host }
func (d *Device) Connected() bool     { return d.connected.Load() }

type promptState struct {
	raw, text string
	err       error
}

func (d *Device) RawPrompt() string       { return d.prompt.Load().(promptState).raw }
func (d *Device) Prompt() (string, error) { s := d.prompt.Load().(promptState); return s.text, s.err }
func (d *Device) setPrompt(response Response) {
	raw := strings.Trim(response.RawPrompt(), " \t\r\n\x00")
	// A privilege acknowledgement may be part of the match. Only its final
	// wire line identifies the next command boundary.
	if i := strings.LastIndexAny(raw, "\r\n"); i >= 0 {
		raw = strings.Trim(raw[i+1:], " \t\x00")
	}
	text, _, err := textcodec.Decode(raw, "auto")
	if err == nil {
		text, err = terminal.RenderString(text, true)
	}
	d.prompt.Store(promptState{raw: raw, text: text, err: err})
}
func (d *Device) acquire() error {
	d.lifeMu.Lock()
	defer d.lifeMu.Unlock()
	if d.closed {
		return ErrClosed
	}
	if !d.lease.CompareAndSwap(false, true) {
		return ErrBusy
	}
	return nil
}
func (d *Device) release() { d.lease.Store(false) }

// operationContext links all callbacks and I/O in an admitted operation to Close.
func (d *Device) operationContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	d.lifeMu.Lock()
	if d.closed {
		cancel()
	} else {
		d.cancel = cancel
	}
	d.lifeMu.Unlock()
	return ctx, func() { cancel(); d.lifeMu.Lock(); d.cancel = nil; d.lifeMu.Unlock() }
}
func (d *Device) Connect(ctx context.Context) (err error) {
	if err = d.acquire(); err != nil {
		return err
	}
	defer d.release()
	ctx, cancel := d.operationContext(ctx)
	defer cancel()
	returned := false
	defer func() {
		if !returned {
			_ = d.closeResources()
		} else if err != nil {
			err = joinCleanup(err, d.closeResources())
		}
	}()
	d.retired.Store(false)
	err = d.connect(ctx)
	if err == nil {
		err = ctx.Err()
	}
	returned = true
	return
}

var errConnectionRefused = errors.New("connector: connection refused")
var errCleanup = errors.New("connector: cleanup failed")
var errHostKeyChanged = errors.New("connector: host key changed")
var errRSATooSmall = errors.New("connector: RSA modulus too small")
var errUnsupportedCipher = errors.New("connector: unsupported cipher")

func (d *Device) connect(parent context.Context) error {
	if err := parent.Err(); err != nil {
		return err
	}
	if d.retired.Load() {
		return ErrNotConnected
	}
	if d.connected.Load() {
		return parent.Err()
	}
	ctx, cancel := context.WithTimeout(parent, d.config.LoginTimeout)
	defer cancel()
	cfg := d.config
	// Legacy options are selected only after a recognized failure.
	cfg.LegacySSHArguments = nil
	cfg.replaceHostKey = false
	err := d.connectOnce(ctx, cfg)
	if err == nil || cfg.Protocol != "ssh" || errors.Is(err, errCleanup) || d.transport != nil || ctx.Err() != nil {
		return err
	}
	switch {
	case d.config.AllowTelnetFallback && errors.Is(err, errConnectionRefused):
		cfg.Protocol = "telnet"
		cfg.Port = 0
	case d.config.HostKeyPolicy == "replace" && errors.Is(err, errHostKeyChanged):
		cfg.replaceHostKey = true
	case d.config.AllowLegacySSH && errors.Is(err, errRSATooSmall) && len(d.profile.LegacySSHArguments) > 0:
		cfg.LegacySSHArguments = append([]string(nil), d.profile.LegacySSHArguments...)
	case d.config.AllowLegacySSH && errors.Is(err, errUnsupportedCipher):
		cfg.LegacySSHArguments = []string{"-c", "des"}
	default:
		return err
	}
	return d.connectOnce(ctx, cfg)
}
func (d *Device) connectOnce(ctx context.Context, cfg ConnectionConfig) (err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if d.connected.Load() {
		return nil
	}
	if d.transport != nil {
		if err = d.closeResources(); err != nil {
			return
		}
	}
	if e := d.log.scope(d.baseSecrets(), true); e != nil {
		return newError("log_error", "logging", Command{}, e)
	}
	if e := d.sessionEvent(ctx, "connect", "info", Command{}, ""); e != nil {
		return e
	}
	defer func() {
		if err != nil {
			err = joinFailures(err, d.sessionEvent(ctx, "connect_failed", "error", Command{}, "failed"))
		}
	}()
	cfg.Challenges = cloneInteractions(cfg.Challenges)
	cfg.LegacySSHArguments = append([]string(nil), cfg.LegacySSHArguments...)
	t, e := d.factory(ctx, cfg)
	// A partial factory return still transfers cleanup responsibility.
	if !isNilValue(t) {
		d.transport = t
		d.owner = &closeOwner{ReadWriteCloser: t}
	}
	if e != nil {
		return joinCleanup(newError("connection_failed", "connect", Command{}, e), d.closeResources())
	}
	if isNilValue(t) {
		return ErrConnection
	}
	if _, legacy := t.(interface{ Authenticated() error }); legacy {
		return newError("invalid_transport", "connect", Command{}, ErrInvalid)
	}
	var owned io.ReadWriteCloser = d.owner
	if conn, ok := t.(syscall.Conn); ok {
		owned = &osCloseOwner{closeOwner: d.owner, Conn: conn}
	}
	opts := []expect.SessionOption{expect.WithBufferLimit(int(d.config.MaxOutputBytes)), expect.WithDefaultTimeout(0), expect.WithWriteTimeout(0), expect.WithOutput(d.log)}
	if _, ok := t.(syscall.Conn); ok {
		opts = append(opts, expect.WithOSPolling())
	}
	session, e := expect.NewSession(owned, opts...)
	if e != nil {
		return joinCleanup(e, d.closeResources())
	}
	d.lifeMu.Lock()
	if d.closed {
		d.lifeMu.Unlock()
		return errors.Join(ErrClosed, session.Close())
	}
	d.session = session
	d.lifeMu.Unlock()
	defer func() {
		if err != nil {
			if cleanup := d.closeResources(); cleanup != nil {
				err = joinFailures(err, newError("cleanup_failed", "cleanup", Command{}, errors.Join(errCleanup, cleanup)))
			}
		}
	}()
	if e = d.log.scope(d.baseSecrets(), true); e != nil {
		return newError("log_error", "logging", Command{}, e)
	}
	authctx, cancel := context.WithTimeout(ctx, d.config.LoginTimeout)
	defer cancel()
	interactions := d.loginInteractions(d.config.Password)
	response, e := d.readResponse(authctx, Command{}, d.profile.LoginPrompt, interactions, true, false)
	if e != nil {
		return e
	}
	d.lifeMu.Lock()
	if d.closed || ctx.Err() != nil {
		d.lifeMu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrClosed
	}
	d.setPrompt(response.Response())
	d.connected.Store(true)
	d.lifeMu.Unlock()
	d.retired.Store(false)
	d.privileged.Store(strings.Contains(response.Response().RawPrompt(), "]"))
	if d.profile.AfterLogin != nil {
		// Freeze verified text before publishing the login response. A callback
		// may inspect conversion errors or choose to consume only the raw banner.
		_, _ = response.response.data.convert()
		state, err := d.profile.AfterLogin(ctx, response)
		if err != nil {
			return newError("after_login_failed", "login", Command{}, err)
		}
		d.haState.Store(state.HAState)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c, ok := t.(interface{ Authenticated(context.Context) error }); ok {
		if e = c.Authenticated(ctx); e != nil {
			return newError("host_key_commit_failed", "login", Command{}, e)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.sessionEvent(ctx, "login_complete", "info", Command{}, "ok")
}
func (d *Device) baseSecrets() []string {
	var s []string
	for _, v := range []string{d.config.Username, d.config.Password, d.config.EnablePassword, d.config.KeyPassphrase} {
		if v != "" {
			s = append(s, v)
		}
	}
	return s
}
func (d *Device) loginInteractions(secret string) []Interaction {
	hostkey := Interaction{Pattern: hostKeyQuestion, Limit: 1, Respond: func(context.Context, string) (string, error) {
		if d.config.HostKeyPolicy == "strict" {
			return "", ErrAuthentication
		}
		return "yes\n", nil
	}}
	user := Interaction{Pattern: d.profile.UsernamePrompt, Limit: 1, Sensitive: true, Respond: func(context.Context, string) (string, error) {
		if d.config.Username == "" {
			return "", ErrAuthentication
		}
		return d.config.Username + d.config.LineEnding, nil
	}}
	pass := Interaction{Pattern: d.profile.PasswordPrompt, Limit: 1, Sensitive: true, Respond: func(context.Context, string) (string, error) {
		if secret == "" {
			return "", ErrAuthentication
		}
		return secret + d.config.LineEnding, nil
	}}
	replies := []Interaction{hostkey}
	replies = append(replies, d.profile.LoginInteractions...)
	replies = append(replies, d.config.Challenges...)
	if d.config.KeyPassphrase != "" {
		replies = append(replies, Interaction{Pattern: keyPassphraseQuestion, Reply: d.config.KeyPassphrase + d.config.LineEnding, Limit: 1, Sensitive: true})
	}
	return append(replies, user, pass)
}

// Close rejects new work and interrupts admitted I/O without waiting for callbacks.
// A later Close can retry cleanup that previously failed.
func (d *Device) Close() error {
	d.lifeMu.Lock()
	d.closed = true
	d.connected.Store(false)
	if d.cancel != nil {
		d.cancel()
	}
	session := d.session
	d.lifeMu.Unlock()
	var err error
	if session != nil {
		err = session.Close()
	}
	if d.lease.CompareAndSwap(false, true) {
		defer d.release()
		err = errors.Join(err, d.closeResources())
	}
	if err != nil {
		return newError("cleanup_failed", "cleanup", Command{}, err)
	}
	return nil
}

func (d *Device) closeResources() error {
	d.retired.Store(true)
	d.connected.Store(false)
	d.privileged.Store(false)
	d.haState.Store("")
	d.prompt.Store(promptState{err: ErrIncomplete})
	var failure error
	d.lifeMu.Lock()
	session := d.session
	d.lifeMu.Unlock()
	if session != nil {
		failure = session.Close()
		d.lifeMu.Lock()
		d.session = nil
		d.lifeMu.Unlock()
	}
	if d.transport != nil {
		var e error
		if d.owner != nil {
			if !d.owner.complete() {
				e = d.owner.Close()
			}
			if d.owner.complete() {
				d.transport = nil
				d.owner = nil
			}
		} else {
			e = d.transport.Close()
			if e == nil {
				d.transport = nil
			}
		}
		failure = errors.Join(failure, e)
	}
	failure = errors.Join(failure, d.log.finish())
	return failure
}
func (d *Device) HAState() string {
	if !d.connected.Load() {
		return ""
	}
	return d.haState.Load().(string)
}
