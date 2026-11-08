// Package connector provides bounded network-device CLI operations.
package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	expect "github.com/gatework/go-expect"
)

var (
	ErrClosed         = errors.New("connector: device closed")
	ErrNotConnected   = errors.New("connector: explicit reconnect required")
	ErrBusy           = errors.New("connector: session busy")
	ErrDevice         = errors.New("connector: device rejected command")
	ErrAuthentication = errors.New("connector: authentication failed")
	ErrConnection     = errors.New("connector: connection failed")
	ErrUnconfirmed    = errors.New("connector: save not confirmed")
	ErrResponseLimit  = errors.New("connector: response limit exceeded")
	ErrOutputLimit    = errors.New("connector: output limit exceeded")
	ErrScriptLimit    = errors.New("connector: script output limit exceeded")
	ErrInvalid        = errors.New("connector: invalid input")
	ErrInvalidResult  = errors.New("connector: invalid command result")
	ErrUnsupported    = errors.New("connector: unsupported operation")
	ErrIncomplete     = errors.New("connector: incomplete configuration")
	ErrScope          = errors.New("connector: operation scope ended")
)

// Error carries safe metadata. Explicit unwrapping retains the original cause.
type Error struct {
	Code, Phase, Source string
	Line                int
	cause               error
}

func (e *Error) Error() string    { return "connector: " + e.Code + " (" + e.Phase + ")" }
func (e *Error) GoString() string { return e.Error() }
func (e *Error) Unwrap() error    { return e.cause }
func newError(code, phase string, command Command, cause error) error {
	return &Error{code, phase, command.Source, command.Line, cause}
}

type Interaction struct {
	Pattern            expect.Pattern
	Reply              string
	Respond            func(context.Context, string) (string, error)
	Limit              int  // zero means one reply; negative values are invalid.
	Unlimited          bool // explicit legacy/pager repetition, still bounded by the operation deadline.
	Capture, Sensitive bool
}
type Command struct {
	Text                       string
	Timeout                    *time.Duration // nil inherits; zero disables the relative timer.
	Poll                       bool           // explicit immediate OS readiness; incompatible with a positive Timeout.
	Prompt                     expect.Pattern
	Interactions               []Interaction
	Sensitive, OutputSensitive bool
	Source                     string
	Line                       int
}

func (c Command) String() string   { return fmt.Sprintf("connector.Command{line:%d}", c.Line) }
func (c Command) GoString() string { return c.String() }
func (c Command) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Source string `json:"source,omitempty"`
		Line   int    `json:"line,omitempty"`
	}{c.Source, c.Line})
}
func (c Command) validate() error {
	if strings.TrimSpace(c.Text) == "" || strings.ContainsAny(c.Text, "\r\n\x00") || !utf8.ValidString(c.Text) {
		return newError("invalid_command", "parse", c, ErrInvalid)
	}
	if c.Timeout != nil && (*c.Timeout < 0 || c.Poll && *c.Timeout > 0) {
		return newError("invalid_timeout", "parse", c, ErrInvalid)
	}
	if !c.Prompt.IsZero() && c.Prompt.ValidateText() != nil {
		return newError("invalid_prompt", "parse", c, ErrInvalid)
	}
	for _, r := range c.Interactions {
		if r.Pattern.ValidateText() != nil || r.Limit < 0 || r.Unlimited && r.Limit > 0 {
			return newError("invalid_interaction", "parse", c, ErrInvalid)
		}
	}
	return nil
}
func validateInteractions(replies []Interaction) error {
	for _, r := range replies {
		if r.Pattern.ValidateText() != nil || r.Limit < 0 || r.Unlimited && r.Limit > 0 {
			return ErrInvalid
		}
	}
	return nil
}
func clonePattern(r *regexp.Regexp) *regexp.Regexp {
	if r == nil {
		return nil
	}
	// Copy isolates Longest/POSIX semantics; recompiling String would lose them.
	//nolint:staticcheck // SA1019: independent Longest configuration is intentional.
	return r.Copy()
}
func cloneInteractions(in []Interaction) []Interaction {
	out := append([]Interaction(nil), in...)
	return out
}
func (c Command) clone() Command {
	c.Interactions = cloneInteractions(c.Interactions)
	if c.Timeout != nil {
		v := *c.Timeout
		c.Timeout = &v
	}
	return c
}

type Script struct {
	Name     string
	Commands []Command
}

func ParseScript(text, name string) (Script, error) {
	s := Script{Name: name}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		c := Command{Text: line, Source: name, Line: i + 1}
		if e := c.validate(); e != nil {
			return Script{}, e
		}
		s.Commands = append(s.Commands, c)
	}
	return s, nil
}
func (s Script) clone() (Script, error) {
	out := Script{Name: s.Name, Commands: make([]Command, len(s.Commands))}
	for i, c := range s.Commands {
		if e := c.validate(); e != nil {
			return Script{}, e
		}
		out.Commands[i] = c.clone()
	}
	return out, nil
}

// StepResult retains an accepted command response, including later callback failures.
type StepResult struct {
	response Response
	command  Command
	duration time.Duration
}

func (s StepResult) Command() Command        { return s.command.clone() }
func (s StepResult) Response() Response      { return s.response }
func (s StepResult) Duration() time.Duration { return s.duration }
func (s StepResult) String() string {
	return fmt.Sprintf("connector.StepResult{bytes:%d}", len(s.response.Raw()))
}
func (s StepResult) GoString() string { return s.String() }
func (s StepResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Bytes    int     `json:"bytes"`
		Duration float64 `json:"duration"`
	}{len(s.response.Raw()), s.duration.Seconds()})
}

type Result struct {
	attempts []Attempt
	steps    []StepResult
	config   *string
}

// Attempt records command-write progress and received evidence, including failure.
// Complete observes a prompt, not device acceptance or persistence.
type Attempt struct {
	command  Command
	response Response
	written  int
	complete bool
	err      error
}

func (a Attempt) Response() Response { return a.response }
func (a Attempt) Command() Command   { return a.command.clone() }
func (a Attempt) Written() int       { return a.written }
func (a Attempt) Complete() bool     { return a.complete }
func (a Attempt) Err() error         { return a.err }
func (a Attempt) String() string {
	return fmt.Sprintf("connector.Attempt{written:%d complete:%t failed:%t}", a.written, a.complete, a.err != nil)
}
func (a Attempt) GoString() string { return a.String() }
func (a Attempt) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Written  int  `json:"written"`
		Complete bool `json:"complete"`
		Failed   bool `json:"failed"`
	}{a.written, a.complete, a.err != nil})
}

// Attempts returns detached progress for main and hook-issued command attempts.
// Steps and their JSON format continue to include only accepted main commands.
func (r Result) Attempts() []Attempt { return append([]Attempt(nil), r.attempts...) }

func (r Result) Steps() []StepResult {
	out := append([]StepResult(nil), r.steps...)
	return out
}

// JoinedOutput joins verified accepted command outputs, never configuration or raw bytes.
func (r Result) JoinedOutput() (string, error) {
	var parts []string
	for _, step := range r.steps {
		text, err := step.Response().Output()
		if err != nil {
			return "", err
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, ""), nil
}
func (r Result) Config() (string, bool) {
	if r.config == nil {
		return "", false
	}
	return *r.config, true
}
func (r Result) String() string   { return fmt.Sprintf("connector.Result{steps:%d}", len(r.steps)) }
func (r Result) GoString() string { return r.String() }
func (r Result) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Steps     []StepResult `json:"steps"`
		HasConfig bool         `json:"has_config"`
	}{r.Steps(), r.config != nil})
}

type ExecuteOptions struct {
	// PreserveSessionOnRejection keeps a borrowed session after one command's
	// complete, valid device rejection. It requires WithBorrowedOperation and a
	// single command; every other failure still retires the session.
	PreserveSessionOnRejection bool
	textMode                   configTextMode
	// SkipPrivilege preserves the current view for native upload operations.
	SkipPrivilege  bool
	OnReceipt      func(context.Context, StepResult) error
	OnStep         func(context.Context, StepResult) error
	MaxOutputBytes int64
}
type Executor interface {
	Execute(context.Context, Command) (StepResult, error)
	Value(string) string
	SetValue(string, string)
}
type Profile struct {
	// ValidateSave requires affirmative device evidence; nil leaves saving unconfirmed.
	ValidateSave func(context.Context, Result) error
	// LegacySSHArguments are used only by opted-in RSA compatibility recovery.
	// Nil defaults to SSH1 (-1); an explicit empty slice disables this recovery.
	LegacySSHArguments                                                                 []string
	Name                                                                               string
	LoginPrompt, CommandPrompt, UsernamePrompt, PasswordPrompt, PrivilegePrompt, Pager expect.Pattern
	PagerResponse, PrivilegeCommand                                                    string
	AuthenticationErrors, CommandErrors                                                []*regexp.Regexp
	LoginInteractions, Interactions                                                    []Interaction
	ConfigCommands, SaveCommands                                                       []string
	CommandTimeout                                                                     time.Duration
	Prepare                                                                            func(context.Context, Command, Executor) (Command, bool, error)
	AfterCommand                                                                       func(context.Context, Command, StepResult, Executor) error
	AfterLogin                                                                         func(context.Context, StepResult) (LoginState, error)
	ConfigStrategy                                                                     func() ConfigStrategy
}

// LoginState contains the device state discovered by AfterLogin.
type LoginState struct{ HAState string }

type ConfigStrategy interface {
	MatchPrompt(command, rawCurrent string) (expect.Pattern, error)
	Validate(context.Context, Command, StepResult, Executor) error
	// Finish receives the operation's Close-linked context and must cooperate
	// with cancellation. Each collection owns its own strategy instance.
	Finish(context.Context, Result) (string, error)
}

func (p Profile) clone() (Profile, error) {
	if e := p.Validate(); e != nil {
		return Profile{}, e
	}
	p.LoginInteractions = cloneInteractions(p.LoginInteractions)
	p.Interactions = cloneInteractions(p.Interactions)
	for _, list := range []*[]*regexp.Regexp{&p.AuthenticationErrors, &p.CommandErrors} {
		*list = append([]*regexp.Regexp(nil), (*list)...)
		for i, r := range *list {
			(*list)[i] = clonePattern(r)
		}
	}
	if p.LegacySSHArguments == nil {
		p.LegacySSHArguments = []string{"-1"}
	} else {
		p.LegacySSHArguments = append([]string{}, p.LegacySSHArguments...)
	}
	p.ConfigCommands = append([]string(nil), p.ConfigCommands...)
	p.SaveCommands = append([]string(nil), p.SaveCommands...)
	if p.CommandTimeout == 0 {
		p.CommandTimeout = 25 * time.Second
	}
	if p.UsernamePrompt.IsZero() {
		p.UsernamePrompt = defaultUsername
	}
	if p.PasswordPrompt.IsZero() {
		p.PasswordPrompt = defaultPassword
	}
	if p.Pager.IsZero() {
		p.Pager = defaultPager
	}
	if p.PagerResponse == "" {
		p.PagerResponse = " "
	}
	if len(p.AuthenticationErrors) == 0 {
		p.AuthenticationErrors = []*regexp.Regexp{defaultAuthError}
	}
	return p, nil
}

// Protocol identifies the explicitly selected system client.
type Protocol string

const (
	ProtocolSSH    Protocol = "ssh"
	ProtocolTelnet Protocol = "telnet"
)

// HostKeyPolicy controls explicit host-key trust behavior.
type HostKeyPolicy string

const (
	HostKeyStrict    HostKeyPolicy = "strict"
	HostKeyAcceptNew HostKeyPolicy = "accept_new"
	HostKeyReplace   HostKeyPolicy = "replace"
)

// SessionEvent contains only operation metadata; it never contains command or response text.
type SessionEvent struct{ Host, Name, Status string }

type ConnectionConfig struct {
	// OnSessionEvent runs synchronously under the operation lease, independently
	// of session logging. Callbacks must cooperate with context and not reenter.
	OnSessionEvent func(context.Context, SessionEvent) error

	IdentityFile, KeyPassphrase, LineEnding    string
	Host, Username, Password, EnablePassword   string
	Protocol                                   Protocol
	Port                                       int
	LoginTimeout, CommandTimeout, WriteTimeout time.Duration
	MaxOutputBytes, MaxScriptOutputBytes       int64
	KnownHosts                                 string
	HostKeyPolicy                              HostKeyPolicy
	SSHPath, TelnetPath                        string
	LegacySSHArguments                         []string
	AllowTelnetFallback                        bool
	AllowLegacySSH                             bool
	replaceHostKey                             bool
	Challenges                                 []Interaction
	// Logger is an explicitly injected cooperative writer. With empty LogLevel,
	// it receives raw rendered/redacted response bytes (the low-level API).
	// LogLevel selects structured text events; only debug includes responses.
	// info, warn and error exclude all response bodies. Sensitive exchanges are
	// suppressed at every level. A blocked writer must cooperate with its owner.
	Logger   io.Writer
	LogLevel string
}

func (c ConnectionConfig) String() string   { return "connector.ConnectionConfig" }
func (c ConnectionConfig) GoString() string { return c.String() }
func (c ConnectionConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Host     string
		Protocol Protocol
		Port     int
	}{c.Host, c.Protocol, c.Port})
}

// TransportFactory transfers every non-nil transport to Device, even on error.
// Close must interrupt concurrent I/O; failed cleanup may be retried. A transport
// may implement Authenticated(context.Context) error to persist authentication
// evidence after login. That hook receives the admitted login context and must
// synchronize with Close. The legacy Authenticated() error hook is rejected.
type TransportFactory func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error)
type Option func(*options) error
type options struct{ factory TransportFactory }

func WithTransportFactory(f TransportFactory) Option {
	return func(o *options) error {
		if f == nil {
			return ErrInvalid
		}
		o.factory = f
		return nil
	}
}

func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// cleanupError keeps diagnostics safe without hiding a primary Error's metadata.
type cleanupError struct{ cause error }

func (e *cleanupError) Error() string    { return "connector: cleanup failed" }
func (e *cleanupError) GoString() string { return e.Error() }
func (e *cleanupError) Unwrap() error    { return e.cause }
func joinCleanup(primary, cleanup error) error {
	if cleanup == nil {
		return primary
	}
	return &cleanupError{cause: errors.Join(primary, cleanup)}
}

// joinedFailure retains both causes without formatting arbitrary callback errors.
// Primary metadata is first in the errors.Is/As traversal.
type joinedFailure struct{ primary, secondary error }

func (e *joinedFailure) Error() string {
	var classified *Error
	if errors.As(e.primary, &classified) {
		return classified.Error()
	}
	return "connector: multiple failures"
}
func (e *joinedFailure) GoString() string { return e.Error() }
func (e *joinedFailure) Unwrap() []error  { return []error{e.primary, e.secondary} }
func joinFailures(primary, secondary error) error {
	if primary == nil {
		return secondary
	}
	if secondary == nil {
		return primary
	}
	return &joinedFailure{primary, secondary}
}
