package connector

import (
	"context"
	"errors"
	"io"
	"regexp"
	"testing"
	"time"
)

// These interfaces also check that validation is available on configuration
// values, without requiring allocation of a Device.
type configurationValidator interface{ Validate() error }
type optionsValidator interface{ ValidateOptions() error }

func validateValue(t *testing.T, value any) error {
	t.Helper()
	validator, ok := value.(configurationValidator)
	if !ok {
		t.Fatalf("%T does not provide pure validation", value)
	}
	return validator.Validate()
}

func TestProfileValidationIsPure(t *testing.T) {
	p := fixtureProfile()
	p.ConfigCommands = append(make([]string, 0, 4), "show config")
	p.SaveCommands = []string{"save"}
	p.ConfigStrategy = func() ConfigStrategy { t.Fatal("validation invoked strategy"); return nil }
	p.Prepare = func(context.Context, Command, Executor) (Command, bool, error) {
		t.Fatal("validation invoked command hook")
		return Command{}, false, nil
	}
	if err := validateValue(t, p); err != nil {
		t.Fatal(err)
	}
	if p.CommandTimeout != 0 || p.UsernamePrompt != nil || p.Pager != nil || p.LegacySSHArguments != nil {
		t.Fatal("validation applied defaults to its input")
	}
	if p.ConfigCommands[:cap(p.ConfigCommands)][1] != "" {
		t.Fatal("validation appended into the caller's slice")
	}
}

func TestProfileValidationMatchesConstruction(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Profile)
	}{
		{"missing name", func(p *Profile) { p.Name = "" }},
		{"missing prompt", func(p *Profile) { p.CommandPrompt = nil }},
		{"empty prompt match", func(p *Profile) { p.LoginPrompt = regexp.MustCompile(".*") }},
		{"nil error pattern", func(p *Profile) { p.CommandErrors = []*regexp.Regexp{nil} }},
		{"empty error match", func(p *Profile) { p.AuthenticationErrors = []*regexp.Regexp{regexp.MustCompile(".*")} }},
		{"invalid privilege", func(p *Profile) { p.PrivilegeCommand = "enable" }},
		{"invalid interaction", func(p *Profile) { p.Interactions = []Interaction{{Reply: "yes"}} }},
		{"invalid command", func(p *Profile) { p.ConfigCommands = []string{"show\nchange"} }},
		{"invalid save", func(p *Profile) { p.SaveCommands = []string{"save\x00"} }},
		{"invalid recovery", func(p *Profile) { p.LegacySSHArguments = []string{""} }},
		{"negative timeout", func(p *Profile) { p.CommandTimeout = -time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := fixtureProfile()
			tt.edit(&p)
			if err := validateValue(t, p); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate = %v", err)
			}
			if _, err := New(p, ConnectionConfig{Host: "fixture"}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("New = %v", err)
			}
		})
	}
}

func TestConnectionValidationSeparatesTargetAndOptions(t *testing.T) {
	for _, host := range []string{"", "bad host", "-option", "bad\naddress"} {
		cfg := ConnectionConfig{Host: host}
		validator, ok := any(cfg).(optionsValidator)
		if !ok {
			t.Fatal("ConnectionConfig does not provide options validation")
		}
		if err := validator.ValidateOptions(); err != nil {
			t.Fatalf("options validation rejected an unbound target: %v", err)
		}
		if err := validateValue(t, cfg); !errors.Is(err, ErrInvalid) {
			t.Fatalf("full validation accepted target %q: %v", host, err)
		}
	}
	cfg := ConnectionConfig{Host: "fixture"}
	if err := validateValue(t, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Protocol != "" || cfg.LineEnding != "" || cfg.CommandTimeout != 0 || cfg.Challenges != nil {
		t.Fatal("validation mutated configuration")
	}
}

func TestConnectionValidationMatchesConstruction(t *testing.T) {
	var nilLogger *validationWriter
	tests := []struct {
		name string
		edit func(*ConnectionConfig)
	}{
		{"secret delimiter", func(c *ConnectionConfig) { c.Password = "secret\n" }},
		{"port", func(c *ConnectionConfig) { c.Port = 65536 }},
		{"line ending", func(c *ConnectionConfig) { c.LineEnding = "x" }},
		{"protocol", func(c *ConnectionConfig) { c.Protocol = "invalid" }},
		{"host key policy", func(c *ConnectionConfig) { c.HostKeyPolicy = "invalid" }},
		{"replace without file", func(c *ConnectionConfig) { c.HostKeyPolicy = HostKeyReplace }},
		{"log level", func(c *ConnectionConfig) { c.LogLevel = "trace" }},
		{"typed nil logger", func(c *ConnectionConfig) { c.Logger = nilLogger }},
		{"timeout", func(c *ConnectionConfig) { c.CommandTimeout = -time.Second }},
		{"output budget", func(c *ConnectionConfig) { c.MaxScriptOutputBytes = -1 }},
		{"challenge", func(c *ConnectionConfig) { c.Challenges = []Interaction{{Reply: "secret"}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConnectionConfig{Host: "fixture"}
			tt.edit(&cfg)
			if err := validateValue(t, cfg); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate = %v", err)
			}
			if _, err := New(fixtureProfile(), cfg); !errors.Is(err, ErrInvalid) {
				t.Fatalf("New = %v", err)
			}
		})
	}
}

type validationWriter struct{}

func (*validationWriter) Write([]byte) (int, error) { panic("validation performed I/O") }

func TestNewSnapshotsValidatedConfiguration(t *testing.T) {
	p := fixtureProfile()
	p.CommandPrompt = regexp.MustCompile("a|ab")
	p.Interactions = []Interaction{{Pattern: regexp.MustCompile("q|question"), Reply: "yes"}}
	p.LegacySSHArguments = []string{}
	cfg := ConnectionConfig{Host: "fixture", Challenges: []Interaction{{Pattern: regexp.MustCompile("p|password"), Reply: "secret"}}, LegacySSHArguments: []string{"-1"}}
	opens := 0
	d, err := New(p, cfg, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
		opens++
		return nil, ErrConnection
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if opens != 0 {
		t.Fatal("construction opened a device")
	}
	p.CommandPrompt.Longest()
	p.Interactions[0].Pattern.Longest()
	p.Interactions[0].Reply = "changed"
	p.ConfigCommands[0] = "changed"
	cfg.Challenges[0].Pattern.Longest()
	cfg.Challenges[0].Reply = "changed"
	cfg.LegacySSHArguments[0] = "changed"
	if d.profile.CommandPrompt.FindString("ab") != "a" || d.profile.Interactions[0].Pattern.FindString("question") != "q" || d.profile.Interactions[0].Reply != "yes" || d.profile.ConfigCommands[0] != "show config" {
		t.Fatal("profile snapshot follows caller mutation")
	}
	if d.config.Challenges[0].Pattern.FindString("password") != "p" || d.config.Challenges[0].Reply != "secret" || d.config.LegacySSHArguments[0] != "-1" {
		t.Fatal("connection snapshot follows caller mutation")
	}
	if d.profile.LegacySSHArguments == nil || len(d.profile.LegacySSHArguments) != 0 || d.profile.CommandTimeout != 25*time.Second || d.config.CommandTimeout != 25*time.Second {
		t.Fatal("normalization changed explicit recovery disable or timeouts")
	}
}

func TestExecutionStateIsPerScript(t *testing.T) {
	p := fixtureProfile()
	p.Prepare = func(_ context.Context, c Command, ex Executor) (Command, bool, error) {
		if ex.Value("previous") != "" {
			t.Error("hook state leaked between scripts")
		}
		ex.SetValue("previous", c.Text)
		return c, false, nil
	}
	d, _ := fixtureDevice(t, p, []string{"one\nvalue\nsw#", "two\nvalue\nsw#"})
	for _, command := range []string{"one", "two"} {
		result, err := d.ExecuteCommand(t.Context(), Command{Text: command}, ExecuteOptions{MaxOutputBytes: 16})
		if err != nil || len(result.Steps()) != 1 || len(result.Attempts()) != 1 {
			t.Fatalf("command %s: steps=%d attempts=%d error=%v", command, len(result.Steps()), len(result.Attempts()), err)
		}
	}
}
