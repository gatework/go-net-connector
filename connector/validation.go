package connector

import (
	"regexp"
	"strings"

	expect "github.com/gatework/go-expect"
)

// Validate checks profile declarations without applying defaults or invoking hooks.
func (p Profile) Validate() error {
	if p.Name == "" || p.LoginPrompt.IsZero() || p.CommandPrompt.IsZero() || p.CommandTimeout < 0 {
		return ErrInvalid
	}
	for _, r := range []expect.Pattern{p.LoginPrompt, p.CommandPrompt, p.UsernamePrompt, p.PasswordPrompt, p.PrivilegePrompt, p.Pager} {
		if !r.IsZero() && r.ValidateText() != nil {
			return ErrInvalid
		}
	}
	if p.PrivilegeCommand != "" {
		if e := (Command{Text: p.PrivilegeCommand}).validate(); e != nil {
			return e
		}
		if p.PrivilegePrompt.IsZero() {
			return ErrInvalid
		}
	}
	for _, replies := range [][]Interaction{p.LoginInteractions, p.Interactions} {
		if e := validateInteractions(replies); e != nil {
			return e
		}
	}
	for _, list := range [][]*regexp.Regexp{p.AuthenticationErrors, p.CommandErrors} {
		for _, r := range list {
			if r == nil || r.MatchString("") {
				return ErrInvalid
			}
		}
	}
	for _, arg := range p.LegacySSHArguments {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return ErrInvalid
		}
	}
	for _, commands := range [][]string{p.ConfigCommands, p.SaveCommands} {
		for _, text := range commands {
			if e := (Command{Text: text}).validate(); e != nil {
				return e
			}
		}
	}
	return nil
}

// Validate checks the target and all connection options without allocating a Device.
func (c ConnectionConfig) Validate() error {
	if strings.TrimSpace(c.Host) == "" || strings.ContainsAny(c.Host, "\r\n\x00\t ") || strings.HasPrefix(c.Host, "-") {
		return ErrInvalid
	}
	return c.ValidateOptions()
}

// ValidateOptions checks reusable options without requiring a bound Host.
func (c ConnectionConfig) ValidateOptions() error {
	if c.Logger != nil && isNilValue(c.Logger) {
		return ErrInvalid
	}
	for _, value := range []string{c.Username, c.Password, c.EnablePassword, c.KeyPassphrase, c.IdentityFile, c.KnownHosts, c.SSHPath, c.TelnetPath} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return ErrInvalid
		}
	}
	if c.Port < 0 || c.Port > 65535 {
		return ErrInvalid
	}
	if c.LineEnding != "" && c.LineEnding != "\n" && c.LineEnding != "\r" && c.LineEnding != "\r\n" {
		return ErrInvalid
	}
	if c.Protocol != "" && c.Protocol != ProtocolSSH && c.Protocol != ProtocolTelnet {
		return ErrInvalid
	}
	switch c.HostKeyPolicy {
	case "", HostKeyStrict, HostKeyAcceptNew:
	case HostKeyReplace:
		if strings.TrimSpace(c.KnownHosts) == "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if c.LogLevel != "" && logPriority(c.LogLevel) < 0 {
		return ErrInvalid
	}
	if c.LoginTimeout < 0 || c.CommandTimeout < 0 || c.WriteTimeout < 0 || c.MaxOutputBytes < 0 || c.MaxScriptOutputBytes < 0 {
		return ErrInvalid
	}
	return validateInteractions(c.Challenges)
}
