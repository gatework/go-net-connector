package connector

import (
	"context"
	"errors"
	"io"
	"regexp"
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
	patterns   map[*regexp.Regexp]expect.Pattern
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
	d.compilePatterns()
	d.prompt.Store("")
	d.haState.Store("")
	d.log = &logWriter{target: cfg.Logger, level: cfg.LogLevel, host: cfg.Host, encoding: "auto", paused: true}
	return d, nil
}
func (d *Device) ProfileName() string        { return d.profile.Name }
func (d *Device) Host() string               { return d.config.Host }
func (d *Device) Capabilities() Capabilities { return d.profile.Capabilities }
func (d *Device) Connected() bool            { return d.connected.Load() }
func (d *Device) CurrentPrompt() string      { return d.prompt.Load().(string) }
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
		return fail("log_error", "logging", Command{}, e)
	}
	if e := d.log.event("connect", "info", Command{}, ""); e != nil {
		return e
	}
	defer func() {
		if err != nil {
			_ = d.log.event("connect_failed", "error", Command{}, "failed")
		}
	}()
	cfg.Challenges = cloneInteractions(cfg.Challenges)
	cfg.LegacySSHArguments = append([]string(nil), cfg.LegacySSHArguments...)
	t, e := d.factory(ctx, cfg)
	// A partial factory return still transfers cleanup responsibility.
	if !nilValue(t) {
		d.transport = t
		d.owner = &closeOwner{ReadWriteCloser: t}
	}
	if e != nil {
		return joinCleanup(fail("connection_failed", "connect", Command{}, e), d.closeResources())
	}
	if nilValue(t) {
		return ErrConnection
	}
	if _, legacy := t.(interface{ Authenticated() error }); legacy {
		return fail("invalid_transport", "connect", Command{}, ErrInvalid)
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
				err = joinFailures(err, fail("cleanup_failed", "cleanup", Command{}, errors.Join(errCleanup, cleanup)))
			}
		}
	}()
	if e = d.log.scope(d.baseSecrets(), true); e != nil {
		return fail("log_error", "logging", Command{}, e)
	}
	authctx, cancel := context.WithTimeout(ctx, d.config.LoginTimeout)
	defer cancel()
	interactions := d.loginInteractions(d.config.Password)
	response, e := d.read(authctx, Command{}, d.profile.LoginPrompt, interactions, true, false)
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
	d.prompt.Store(strings.TrimSpace(response.Prompt()))
	d.connected.Store(true)
	d.lifeMu.Unlock()
	d.retired.Store(false)
	d.privileged.Store(strings.Contains(response.Prompt(), "]"))
	if d.profile.AfterLogin != nil {
		// Freeze verified text before publishing the login response. A callback
		// may inspect conversion errors or choose to consume only the raw banner.
		_, _ = response.response.data.convert()
		state, err := d.profile.AfterLogin(ctx, response)
		if err != nil {
			return fail("after_login_failed", "login", Command{}, err)
		}
		d.haState.Store(state.HAState)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c, ok := t.(interface{ Authenticated(context.Context) error }); ok {
		if e = c.Authenticated(ctx); e != nil {
			return fail("host_key_commit_failed", "login", Command{}, e)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.log.event("login_complete", "info", Command{}, "ok")
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
		return fail("cleanup_failed", "cleanup", Command{}, err)
	}
	return nil
}

func (d *Device) closeResources() error {
	d.retired.Store(true)
	d.connected.Store(false)
	d.privileged.Store(false)
	d.haState.Store("")
	d.prompt.Store("")
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
func (d *Device) ExecuteCommand(ctx context.Context, c Command, opts ExecuteOptions) (Result, error) {
	return d.ExecuteScript(ctx, Script{Commands: []Command{c}}, opts)
}
func (d *Device) ExecuteScript(ctx context.Context, script Script, opts ExecuteOptions) (Result, error) {
	s, e := script.clone()
	if e != nil {
		return Result{}, e
	}
	if opts.MaxOutputBytes < 0 {
		return Result{}, ErrInvalid
	}
	if e = d.acquire(); e != nil {
		return Result{}, e
	}
	defer d.release()
	ctx, cancel := d.operationContext(ctx)
	defer cancel()
	return d.execute(ctx, s, opts, nil)
}

var ansiColor = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type execution struct {
	attempts     []Attempt
	device       *Device
	values       map[string]string
	valuesMu     sync.Mutex
	bytes, limit int64
	strategy     ConfigStrategy
}

type hookExecutor struct {
	execution *execution
	scope     *callScope
}

func (x *hookExecutor) Value(key string) string {
	x.execution.valuesMu.Lock()
	defer x.execution.valuesMu.Unlock()
	return x.execution.values[key]
}
func (x *hookExecutor) SetValue(key, value string) {
	x.execution.valuesMu.Lock()
	defer x.execution.valuesMu.Unlock()
	x.execution.values[key] = value
}
func (x *hookExecutor) Execute(ctx context.Context, c Command) (StepResult, error) {
	ctx, leave, e := x.scope.enter(ctx)
	if e != nil {
		return StepResult{}, e
	}
	defer leave()
	if e = c.validate(); e != nil {
		return StepResult{}, e
	}
	return x.execution.command(ctx, c.clone())
}
func (x *execution) hook(ctx context.Context, run func(Executor) error) (err error) {
	executor := &hookExecutor{execution: x, scope: newCallScope(ctx)}
	defer func() {
		if executor.scope.end() && err == nil {
			err = context.Canceled
		}
	}()
	return run(executor)
}
func (d *Device) execute(ctx context.Context, script Script, opts ExecuteOptions, strategy ConfigStrategy) (result Result, err error) {
	returned := false
	defer func() {
		if !returned {
			_ = d.closeResources()
			return
		}
		if e := d.log.scope(d.baseSecrets(), true); e != nil {
			err = joinFailures(err, fail("log_error", "logging", Command{}, e))
		}
		level, status := "info", "completed"
		if err != nil {
			level, status = "error", "failed"
		}
		if e := d.log.event("operation_complete", level, Command{}, status); e != nil {
			err = joinFailures(err, e)
		}
		if err != nil {
			err = joinCleanup(err, d.closeResources())
		}
	}()
	result, err = d.runScript(ctx, script, opts, strategy)
	returned = true
	return
}
func (d *Device) runScript(ctx context.Context, script Script, opts ExecuteOptions, strategy ConfigStrategy) (result Result, err error) {
	if len(script.Commands) == 0 {
		return result, ctx.Err()
	}
	if err = d.connect(ctx); err != nil {
		return
	}
	x := &execution{device: d, values: make(map[string]string), limit: d.config.MaxScriptOutputBytes, strategy: strategy}
	defer func() { result.attempts = append([]Attempt(nil), x.attempts...) }()
	if opts.MaxOutputBytes > 0 {
		x.limit = opts.MaxOutputBytes
	}

	if !opts.SkipPrivilege && d.profile.PrivilegeCommand != "" && !d.privileged.Load() {
		secret := d.config.EnablePassword
		if secret == "" {
			secret = d.config.Password
		}
		ac, cancel := context.WithTimeout(ctx, d.config.LoginTimeout)
		err = d.send(ac, d.profile.PrivilegeCommand+d.config.LineEnding, false)
		if err == nil {
			var s StepResult
			s, err = d.read(ac, Command{}, d.profile.PrivilegePrompt, d.loginInteractions(secret), true, false)
			if err == nil {
				d.prompt.Store(strings.TrimSpace(s.Prompt()))
				d.privileged.Store(true)
			}
		}
		cancel()
		if err != nil {
			return
		}
	}
	for _, original := range script.Commands {
		if err = ctx.Err(); err != nil {
			return
		}
		c := original
		if d.profile.Prepare != nil {
			var skip bool
			err = x.hook(ctx, func(ex Executor) error { var e error; c, skip, e = d.profile.Prepare(ctx, c, ex); return e })
			if err != nil {
				err = fail("prepare_failed", "script", original, err)
				return
			}
			if skip {
				continue
			}
			if err = c.validate(); err != nil {
				return
			}
		}
		var step StepResult
		step, err = x.command(ctx, c)
		if step.Prompt() != "" {
			result.steps = append(result.steps, step)
		}
		if err == nil && d.profile.AfterCommand != nil {
			e := x.hook(ctx, func(ex Executor) error { return d.profile.AfterCommand(ctx, c, step, ex) })
			if e != nil {
				err = fail("after_command_failed", "script", c, e)
			}
		}
		if err == nil && strategy != nil {
			e := x.hook(ctx, func(ex Executor) error { return strategy.Validate(ctx, c, step, ex) })
			if e != nil {
				err = configurationFailure(c, e)
			}
		}
		if step.Prompt() != "" && opts.OnReceipt != nil {
			if e := opts.OnReceipt(ctx, step); e != nil {
				err = joinFailures(err, fail("receipt_callback_failed", "script", c, e))
			}
		}
		if err != nil {
			return
		}
		if err = ctx.Err(); err != nil {
			return
		}
		if opts.OnStep != nil {
			if e := opts.OnStep(ctx, step); e != nil {
				err = fail("step_callback_failed", "script", c, e)
				return
			}
		}
		if err = ctx.Err(); err != nil {
			return
		}
	}
	return
}
func (x *execution) command(ctx context.Context, c Command) (step StepResult, err error) {
	attempted := false
	written := 0
	var response Response
	defer func() {
		if attempted {
			x.attempts = append(x.attempts, Attempt{command: c.clone(), response: response, written: written, complete: response.RawPrompt() != "", err: err})
		}
	}()

	d := x.device
	if err = ctx.Err(); err != nil {
		return step, err
	}
	secrets := d.baseSecrets()
	if c.Sensitive {
		secrets = append(secrets, c.Text)
	}
	private := c.Sensitive || c.OutputSensitive
	for _, rules := range [][]Interaction{c.Interactions, d.profile.Interactions} {
		for _, rule := range rules {
			private = private || rule.Sensitive
		}
	}
	restore, e := d.log.push(secrets, private)
	if e != nil {
		return step, fail("log_error", "logging", c, e)
	}
	defer func() {
		if e := restore(); e != nil {
			err = joinFailures(err, fail("log_error", "logging", c, e))
		}
	}()
	if e := d.log.event("command_start", "info", c, ""); e != nil {
		return step, e
	}
	defer func() {
		level, status := "info", "response_received"
		if err != nil {
			level, status = "error", "failed"
		}
		if e := d.log.event("command_complete", level, c, status); e != nil {
			err = joinFailures(err, e)
		}
	}()
	d.log.count = func(n int) { x.bytes += int64(n) }
	defer func() { d.log.count = nil }()
	if x.limit > 0 && x.bytes >= x.limit {
		return step, fail("script_output_limit_exceeded", "script", c, ErrScriptLimit)
	}
	timeout := d.config.CommandTimeout
	if c.Timeout != nil {
		timeout = *c.Timeout
	}
	if c.Poll {
		timeout = 0
	}
	cc := ctx
	cancel := func() {}
	if timeout > 0 {
		cc, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	started := time.Now()
	prompt := c.Prompt
	if prompt == nil {
		prompt = d.profile.CommandPrompt
	}
	if x.strategy != nil && c.Prompt == nil {
		literal := x.strategy.Prompt(c.Text, d.CurrentPrompt())
		if literal == "" {
			return step, ErrIncomplete
		}
		prompt = regexp.MustCompile(`(?:\A|[\r\n])[ \t\x00]*` + regexp.QuoteMeta(literal) + `[ \t\x00]*[\r\n]*\z`)
	}
	attempted = true
	if err = d.send(cc, c.Text+d.config.LineEnding, c.Poll); err != nil {
		var progress *expect.WriteError
		if errors.As(err, &progress) {
			written = progress.Written
		}
		return step, writeFailure("command", c, err)
	}
	written = len(c.Text) + len(d.config.LineEnding)
	replies := append(cloneInteractions(c.Interactions), d.profile.Interactions...)
	replies = append(replies, Interaction{Pattern: d.profile.Pager, Reply: d.profile.PagerResponse, Unlimited: true})
	step, err = d.read(cc, c, prompt, replies, false, c.Poll)
	response = step.Response()
	step.duration = time.Since(started)
	if err != nil {
		return StepResult{}, err
	}
	diagnostic, conversionErr := response.data.convert()
	if conversionErr != nil {
		return StepResult{}, fail("invalid_output_encoding", "parse", c, conversionErr)
	}
	if int64(len(step.Output())) > d.config.MaxOutputBytes {
		return step, fail("output_limit_exceeded", "parse", c, ErrOutputLimit)
	}
	if len(d.profile.CommandErrors) > 0 {
		if strings.Contains(diagnostic, "\x1b[") {
			diagnostic = ansiColor.ReplaceAllString(diagnostic, "")
		}
		diagnostic = strings.ReplaceAll(strings.ReplaceAll(diagnostic, "\r\n", "\n"), "\r", "\n")
	}
	rendered, _ := response.Rendered()
	for _, pattern := range d.profile.CommandErrors {
		if pattern.MatchString(diagnostic) || pattern.MatchString(rendered) {
			return StepResult{}, fail("device_error", "command", c, ErrDevice)
		}
	}
	d.prompt.Store(strings.TrimSpace(step.Prompt()))
	if x.limit > 0 && x.bytes > x.limit {
		return step, fail("script_output_limit_exceeded", "script", c, ErrScriptLimit)
	}
	// The prompt handler records completion before a late cancellation is returned.
	if err = cc.Err(); err != nil {
		return step, err
	}
	return step, nil
}
func (d *Device) send(ctx context.Context, text string, poll bool) error {
	if poll {
		_, e := d.session.SendAllPoll(ctx, text)
		return e
	}
	wc, cancel := context.WithTimeout(ctx, d.config.WriteTimeout)
	defer cancel()
	_, e := d.session.SendAll(wc, text)
	return e
}
func (d *Device) read(ctx context.Context, c Command, prompt *regexp.Regexp, replies []Interaction, login, poll bool) (step StepResult, err error) {
	if prompt == nil {
		return step, ErrInvalid
	}
	var raw, body strings.Builder
	var rawPrompt string
	separate := false
	defer func() {
		step.command = c.clone()
		wire := raw.String()
		output := wire
		if separate {
			output = body.String()
		}
		step.response = Response{data: &responseData{raw: wire, rawPrompt: rawPrompt, output: output,
			outputErr: ErrIncomplete, renderErr: ErrIncomplete, promptErr: ErrIncomplete}}
	}()
	completed := false
	appendSegment := func(r expect.Result, capture bool) error {
		if !capture && !separate {
			// Most responses never remove a pager marker: share raw until then.
			body.WriteString(raw.String())
			separate = true
		}
		raw.WriteString(r.Before)
		raw.WriteString(r.Match)
		if separate {
			body.WriteString(r.Before)
		}
		if capture {
			if separate {
				body.WriteString(r.Match)
			}
		} else {
			// Go regex boundaries consume delimiters that Ruby lookarounds retain.
			for _, b := range r.Match {
				if b == '\r' || b == '\n' {
					body.WriteRune(b)
				}
			}
		}
		if int64(raw.Len()) > d.config.MaxOutputBytes {
			return ErrOutputLimit
		}
		return nil
	}
	var cases []expect.Case
	add := func(p *regexp.Regexp, h expect.Handler) {
		pattern := d.pattern(p)
		cases = append(cases, expect.Case{Pattern: pattern, Handler: h})
	}
	if login {
		for _, failure := range loginFailures {
			add(failure.pattern, func(context.Context, *expect.Session, expect.Result) (expect.Action, error) {
				return expect.Return, failure.cause
			})
		}
		failures := append([]*regexp.Regexp{loginAuthError}, d.profile.AuthenticationErrors...)
		for _, p := range failures {
			add(p, func(context.Context, *expect.Session, expect.Result) (expect.Action, error) {
				return expect.Return, ErrAuthentication
			})
		}
	}
	counts := make([]int, len(replies))
	for i, reply := range replies {
		if reply.Pattern == nil {
			return step, ErrInvalid
		}
		add(reply.Pattern, func(hctx context.Context, _ *expect.Session, r expect.Result) (expect.Action, error) {
			if e := appendSegment(r, reply.Capture); e != nil {
				return expect.Return, e
			}
			counts[i]++
			if !reply.Unlimited && counts[i] > max(1, reply.Limit) {
				if login {
					return expect.Return, ErrAuthentication
				}
				return expect.Return, ErrResponseLimit
			}
			answer := reply.Reply
			if reply.Respond != nil {
				var e error
				answer, e = reply.Respond(hctx, r.Match)
				if e != nil {
					return expect.Return, e
				}
			}
			if reply.Sensitive {
				if e := d.log.protectReply(strings.TrimSuffix(answer, "\n")); e != nil {
					return expect.Return, e
				}
			}
			if e := hctx.Err(); e != nil {
				return expect.Return, e
			}
			if e := d.send(hctx, answer, poll); e != nil {
				return expect.Return, writeFailure(map[bool]string{true: "login", false: "command"}[login], c, e)
			}
			return expect.ContinueWithoutReset, nil
		})
	}
	add(prompt, func(_ context.Context, _ *expect.Session, r expect.Result) (expect.Action, error) {
		if e := appendSegment(r, true); e != nil {
			return expect.Return, e
		}
		rawPrompt = r.Match
		completed = true
		return expect.Return, nil
	})
	options := []expect.ExpectOption{expect.WithTimeout(0), expect.WithDeclarationPriority(), expect.WithStreamingFallback(func(_ context.Context, _ *expect.Session, r expect.Result) (expect.Action, error) {
		return expect.ContinueWithoutReset, appendSegment(r, true)
	})}
	var final expect.Result
	if poll {
		final, err = d.session.Poll(ctx, cases, options...)
	} else {
		final, err = d.session.Expect(ctx, cases, options...)
	}
	if err != nil && !completed && final.CaseIndex < 0 && final.Before != "" {
		// The unmatched prefix has not passed a handler or streaming fallback.
		remaining := max(0, int(d.config.MaxOutputBytes)-raw.Len())
		prefix := final.Before[:min(len(final.Before), remaining)]
		raw.WriteString(prefix)
		if separate {
			body.WriteString(prefix)
		}
	}
	if err != nil && !completed {
		phase := map[bool]string{true: "login", false: "command"}[login]
		var classified *Error
		if errors.As(err, &classified) {
			return StepResult{}, err
		}
		code := "transport_error"
		switch {
		case errors.Is(err, ErrResponseLimit):
			code = "response_limit_exceeded"
		case errors.Is(err, ErrAuthentication):
			code = "authentication_error"
		case errors.Is(err, ErrOutputLimit), errors.Is(err, expect.ErrBufferLimit):
			code = "output_limit_exceeded"
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			code = "connection_closed"
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, expect.ErrTimeout):
			switch {
			case c.Prompt != nil:
				code = "prompt_error"
			case login:
				code = "login_timeout"
			default:
				code = "command_timeout"
			}
		case errors.Is(err, context.Canceled):
			return StepResult{}, err
		}
		return StepResult{}, fail(code, phase, c, err)
	}
	if completed {
		return step, nil
	}
	return step, err
}

type logWriter struct {
	mu                    sync.Mutex
	target                io.Writer
	level, host, encoding string
	line                  strings.Builder
	redactor, rendered    *expect.Redactor
	renderer              *terminal.Renderer
	secrets               []string
	paused                bool
	count                 func(int)
}
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
func (l *logWriter) scope(secrets []string, paused bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.finishLocked(); e != nil {
		return e
	}
	return l.startLocked(secrets, paused)
}
func (l *logWriter) startLocked(secrets []string, paused bool) error {
	l.secrets = append([]string(nil), secrets...)
	l.paused = paused
	if paused || l.target == nil || l.level != "" && l.level != "debug" {
		return nil
	}
	var e error
	l.redactor, e = expect.NewRedactor(textcodec.SecretVariants(secrets, l.encoding), "[REDACTED]")
	if e != nil {
		return e
	}
	l.rendered, e = expect.NewRedactor(secrets, "[REDACTED]")
	if e != nil {
		return e
	}
	l.renderer = terminal.New(writerFunc(func(p []byte) (int, error) {
		if e := l.emit(l.rendered.Append(p)); e != nil {
			return 0, e
		}
		return len(p), nil
	}), 32<<20, false)
	l.renderer.Encoding = l.encoding
	return nil
}
func (l *logWriter) push(secrets []string, paused bool) (func() error, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	oldSecrets := append([]string(nil), l.secrets...)
	oldPaused := l.paused
	if e := l.finishLocked(); e != nil {
		return nil, e
	}
	if e := l.startLocked(secrets, paused); e != nil {
		return nil, e
	}
	return func() error { return l.scope(oldSecrets, oldPaused) }, nil
}
func (l *logWriter) protectReply(secret string) error {
	if e := l.addSecret(secret); e != nil {
		return e
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.finishLocked(); e != nil {
		return e
	}
	return l.startLocked(l.secrets, true)
}
func (l *logWriter) addSecret(secret string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if secret == "" {
		return nil
	}
	l.secrets = append(l.secrets, secret)
	if l.redactor == nil {
		return l.startLocked(l.secrets, l.paused)
	}
	tail, e := l.redactor.SetSecrets(textcodec.SecretVariants(l.secrets, l.encoding))
	if e != nil {
		return e
	}
	// Update rendered protection before passing any newly released raw suffix.
	visible, e := l.rendered.SetSecrets(l.secrets)
	if e != nil {
		return e
	}
	if l.paused {
		return nil
	}
	if e = l.emit(visible); e != nil {
		return e
	}
	_, e = l.renderer.Write(tail)
	return e
}
func (l *logWriter) emit(p []byte) error {
	if l.target == nil || len(p) == 0 {
		return nil
	}
	if l.level == "" {
		return l.writeTarget(p)
	}
	if l.level != "debug" {
		return nil
	}
	for _, b := range p {
		if b == '\n' {
			if err := l.flushLine(); err != nil {
				return err
			}
		} else {
			l.line.WriteByte(b)
		}
	}
	return nil
}
func (l *logWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.count != nil {
		l.count(len(p))
	}
	if l.paused || l.target == nil || l.level != "" && l.level != "debug" {
		return len(p), nil
	}
	if l.redactor == nil {
		return 0, errors.New("connector: missing log redactor")
	}
	if _, e := l.renderer.Write(l.redactor.Append(p)); e != nil {
		return 0, e
	}
	return len(p), nil
}
func (l *logWriter) finishLocked() error {
	if l.redactor == nil {
		return nil
	}
	raw, renderer, rendered := l.redactor, l.renderer, l.rendered
	defer func() { l.redactor = nil; l.renderer = nil; l.rendered = nil }()
	if l.paused || l.target == nil || l.level != "" && l.level != "debug" {
		return nil
	}
	if _, e := renderer.Write(raw.Finish()); e != nil {
		return e
	}
	if e := renderer.Finish(); e != nil {
		return e
	}
	if err := l.emit(rendered.Finish()); err != nil {
		return err
	}
	return l.flushLine()
}
func (l *logWriter) finish() error { l.mu.Lock(); defer l.mu.Unlock(); return l.finishLocked() }

func (d *Device) HAState() string {
	if !d.connected.Load() {
		return ""
	}
	return d.haState.Load().(string)
}

func writeFailure(phase string, c Command, err error) error {
	var classified *Error
	if errors.As(err, &classified) {
		metadata := *classified
		metadata.cause = err
		return &metadata
	}
	if errors.Is(err, expect.ErrBufferLimit) || errors.Is(err, ErrOutputLimit) {
		return fail("output_limit_exceeded", phase, c, err)
	}
	code := "transport_error"
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, expect.ErrTimeout) {
		code = "write_timeout"
	}
	return fail(code, phase, c, err)
}
