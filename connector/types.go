// Package connector provides bounded network-device CLI operations.
package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrBusy           = errors.New("connector: session busy")
	ErrDevice         = errors.New("connector: device rejected command")
	ErrAuthentication = errors.New("connector: authentication failed")
	ErrConnection     = errors.New("connector: connection failed")
	ErrOutputLimit    = errors.New("connector: output limit exceeded")
	ErrScriptLimit    = errors.New("connector: script output limit exceeded")
	ErrInvalid        = errors.New("connector: invalid input")
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
func fail(code, phase string, c Command, cause error) error {
	return &Error{code, phase, c.Source, c.Line, cause}
}

type Interaction struct {
	Pattern            *regexp.Regexp
	Response           string
	Respond            func(context.Context, string) (string, error)
	Limit              int // zero is unlimited; credentials are configured with one.
	Capture, Sensitive bool
}
type Command struct {
	Text                       string
	Timeout                    *time.Duration // nil inherits; zero explicitly polls.
	Prompt                     *regexp.Regexp
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
		return fail("invalid_command", "parse", c, ErrInvalid)
	}
	if c.Timeout != nil && *c.Timeout < 0 {
		return fail("invalid_timeout", "parse", c, ErrInvalid)
	}
	if c.Prompt != nil && c.Prompt.MatchString("") {
		return fail("invalid_prompt", "parse", c, ErrInvalid)
	}
	for _, r := range c.Interactions {
		if r.Pattern == nil || r.Pattern.MatchString("") || r.Limit < 0 {
			return fail("invalid_interaction", "parse", c, ErrInvalid)
		}
	}
	return nil
}
func validateInteractions(replies []Interaction) error {
	for _, r := range replies {
		if r.Pattern == nil || r.Pattern.MatchString("") || r.Limit < 0 {
			return ErrInvalid
		}
	}
	return nil
}
func clonePattern(r *regexp.Regexp) *regexp.Regexp {
	if r == nil {
		return nil
	}
	return regexp.MustCompile(r.String())
}
func cloneInteractions(in []Interaction) []Interaction {
	out := append([]Interaction(nil), in...)
	for i := range out {
		out[i].Pattern = clonePattern(out[i].Pattern)
	}
	return out
}
func (c Command) clone() Command {
	c.Prompt = clonePattern(c.Prompt)
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

type StepResult struct {
	command             Command
	raw, output, prompt string
	duration            time.Duration
}

func (s StepResult) Command() Command        { return s.command.clone() }
func (s StepResult) Raw() string             { return s.raw }
func (s StepResult) Output() string          { return s.output }
func (s StepResult) Prompt() string          { return s.prompt }
func (s StepResult) Duration() time.Duration { return s.duration }
func (s StepResult) String() string          { return fmt.Sprintf("connector.StepResult{bytes:%d}", len(s.raw)) }
func (s StepResult) GoString() string        { return s.String() }
func (s StepResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Bytes    int     `json:"bytes"`
		Duration float64 `json:"duration"`
	}{len(s.raw), s.duration.Seconds()})
}

type Result struct {
	steps  []StepResult
	config *string
}

func (r Result) Steps() []StepResult {
	out := append([]StepResult(nil), r.steps...)
	for i := range out {
		out[i].command = out[i].command.clone()
	}
	return out
}
func (r Result) Output() string {
	if r.config != nil {
		return *r.config
	}
	var b strings.Builder
	for _, s := range r.steps {
		b.WriteString(s.output)
	}
	return b.String()
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
	// SkipPrivilege preserves the current view for native upload operations.
	SkipPrivilege  bool
	OnReceipt      func(context.Context, StepResult) error
	OnStep         func(context.Context, StepResult) error
	MaxOutputBytes int64
}
type Executor interface {
	Execute(context.Context, Command) (StepResult, error)
	Get(string) string
	Set(string, string)
}
type Profile struct {
	// LegacySSHArguments are used only by opted-in RSA compatibility recovery.
	// Nil defaults to SSH1 (-1); an explicit empty slice disables this recovery.
	LegacySSHArguments                                                                 []string
	Name                                                                               string
	LoginPrompt, CommandPrompt, UsernamePrompt, PasswordPrompt, PrivilegePrompt, Pager *regexp.Regexp
	PagerResponse, PrivilegeCommand                                                    string
	AuthenticationErrors, CommandErrors                                                []*regexp.Regexp
	LoginInteractions, Interactions                                                    []Interaction
	ConfigCommands, SaveCommands                                                       []string
	CommandTimeout                                                                     time.Duration
	Prepare                                                                            func(context.Context, Command, Executor) (Command, bool, error)
	AfterCommand                                                                       func(context.Context, Command, StepResult, Executor) error
	AfterLogin                                                                         func(StepResult) map[string]string
	ConfigStrategy                                                                     func() ConfigStrategy
	Capabilities                                                                       Capabilities
}
type ConfigStrategy interface {
	Prompt(command, current string) string
	Validate(context.Context, Command, StepResult, Executor) error
	Finish(Result) (string, error)
}
type Capabilities struct{ Save, TFTP, RemoteBackup, Neighbors, Descriptions, TopologyWrite bool }

func (p Profile) clone() (Profile, error) {
	if p.Name == "" || p.LoginPrompt == nil || p.CommandPrompt == nil {
		return Profile{}, ErrInvalid
	}
	for _, r := range []*regexp.Regexp{p.LoginPrompt, p.CommandPrompt, p.UsernamePrompt, p.PasswordPrompt, p.PrivilegePrompt, p.Pager} {
		if r != nil && r.MatchString("") {
			return Profile{}, ErrInvalid
		}
	}
	if p.PrivilegeCommand != "" {
		if e := (Command{Text: p.PrivilegeCommand}).validate(); e != nil {
			return Profile{}, e
		}
		if p.PrivilegePrompt == nil {
			return Profile{}, ErrInvalid
		}
	}
	for _, replies := range [][]Interaction{p.LoginInteractions, p.Interactions} {
		if e := validateInteractions(replies); e != nil {
			return Profile{}, e
		}
	}
	p.LoginPrompt = clonePattern(p.LoginPrompt)
	p.CommandPrompt = clonePattern(p.CommandPrompt)
	p.UsernamePrompt = clonePattern(p.UsernamePrompt)
	p.PasswordPrompt = clonePattern(p.PasswordPrompt)
	p.PrivilegePrompt = clonePattern(p.PrivilegePrompt)
	p.Pager = clonePattern(p.Pager)
	p.LoginInteractions = cloneInteractions(p.LoginInteractions)
	p.Interactions = cloneInteractions(p.Interactions)
	for _, list := range []*[]*regexp.Regexp{&p.AuthenticationErrors, &p.CommandErrors} {
		*list = append([]*regexp.Regexp(nil), (*list)...)
		for i, r := range *list {
			if r == nil || r.MatchString("") {
				return Profile{}, ErrInvalid
			}
			(*list)[i] = clonePattern(r)
		}
	}
	if p.LegacySSHArguments == nil {
		p.LegacySSHArguments = []string{"-1"}
	} else {
		p.LegacySSHArguments = append([]string{}, p.LegacySSHArguments...)
	}
	for _, arg := range p.LegacySSHArguments {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return Profile{}, ErrInvalid
		}
	}
	p.ConfigCommands = append([]string(nil), p.ConfigCommands...)
	p.SaveCommands = append([]string(nil), p.SaveCommands...)
	for _, text := range append(append([]string(nil), p.ConfigCommands...), p.SaveCommands...) {
		if e := (Command{Text: text}).validate(); e != nil {
			return Profile{}, e
		}
	}
	if p.CommandTimeout < 0 {
		return Profile{}, ErrInvalid
	}
	if p.CommandTimeout == 0 {
		p.CommandTimeout = 25 * time.Second
	}
	if p.UsernamePrompt == nil {
		p.UsernamePrompt = regexp.MustCompile(`(?i)(?:login|username|name):\s*\z`)
	}
	if p.PasswordPrompt == nil {
		p.PasswordPrompt = regexp.MustCompile(`(?i)password:\s*\z`)
	}
	if p.Pager == nil {
		p.Pager = regexp.MustCompile(`(?im)(?:^|[\r\n])[ \t]*(?:--More--|---- More ----)[ \t\x00\x08]*(?:\r|\n|\z)`)
	}
	if p.PagerResponse == "" {
		p.PagerResponse = " "
	}
	if len(p.AuthenticationErrors) == 0 {
		p.AuthenticationErrors = []*regexp.Regexp{regexp.MustCompile(`(?i)(?:authentication failed|permission denied|access denied|login incorrect)`)}
	}
	return p, nil
}

type ConnectionConfig struct {
	Host, Username, Password, EnablePassword, Protocol string
	Port                                               int
	LoginTimeout, CommandTimeout, WriteTimeout         time.Duration
	MaxOutputBytes, MaxScriptOutputBytes               int64
	KnownHosts, HostKeyPolicy                          string
	SSHPath, TelnetPath                                string
	LegacySSHArguments                                 []string
	AllowTelnetFallback                                bool
	AllowLegacySSH                                     bool
	replaceHostKey                                     bool
	Challenges                                         []Interaction
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
		Host, Protocol string
		Port           int
	}{c.Host, c.Protocol, c.Port})
}

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
