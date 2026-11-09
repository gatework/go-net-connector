// Package driver contains reusable terminal and session mechanics for vendor
// dialects. Platform-specific commands, prompts and exceptions stay with the
// family packages.
package driver

import (
	"context"
	"regexp"
	"time"

	expect "github.com/gatework/go-expect"

	"github.com/gatework/go-net-connector/connector"
)

// Regex compiles a fixed dialect expression during profile construction.
func Regex(expression string) *regexp.Regexp { return regexp.MustCompile(expression) }

// Regexps compiles an ordered set of fixed dialect expressions.
func Regexps(expressions ...string) []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, len(expressions))
	for i, expression := range expressions {
		patterns[i] = Regex(expression)
	}
	return patterns
}

// Pattern compiles a go-expect pattern during profile construction.
func Pattern(expression string) expect.Pattern {
	pattern, err := expect.Compile(expression)
	if err != nil {
		panic(err)
	}
	return pattern
}

// Interaction declares an unbounded command-session reply rule.
func Interaction(expression, response string) connector.Interaction {
	return connector.Interaction{Pattern: Pattern(expression), Reply: response, Capture: true, Unlimited: true}
}

// LoginInteraction declares a reply limited to one login event.
func LoginInteraction(expression, response string) connector.Interaction {
	interaction := Interaction(expression, response)
	interaction.Limit = 1
	interaction.Unlimited = false
	return interaction
}

// BaseProfile supplies stable prompt, pager and authentication defaults.
func BaseProfile(name string) connector.Profile {
	return connector.Profile{
		Name:                 name,
		UsernamePrompt:       Pattern(`(?i)(?:login|username|name):\s*\z`),
		PasswordPrompt:       Pattern(`(?i)password:\s*\z`),
		PrivilegePrompt:      Pattern(`#\s*\z`),
		Pager:                Pattern(`(?i)(?:\A|[\r\n])[ \t]*(?:--More--|---- More ----)[ \t\x00\x08]*(?:\r|\n|\z)`),
		PagerResponse:        " ",
		AuthenticationErrors: Regexps(`(?i)(?:authentication failed|permission denied|access denied|login incorrect)`),
	}
}

// FinishProfile adds command timeout, configuration collection and save proof.
func FinishProfile(profile connector.Profile, timeout time.Duration, configuration func() connector.ConfigStrategy, saveConfirmation *regexp.Regexp) connector.Profile {
	profile.CommandTimeout = timeout
	profile.ValidateSave = func(ctx context.Context, result connector.Result) error {
		return validateSaveConfirmation(ctx, result, saveConfirmation)
	}
	profile.ConfigStrategy = configuration
	return profile
}

// ProfiledPlatform provides the stable Platform methods for a concrete family
// type. Each call returns detached mutable declarations.
type ProfiledPlatform struct{ profile connector.Profile }

// NewProfiledPlatform stores an isolated copy of the connector profile.
func NewProfiledPlatform(profile connector.Profile) ProfiledPlatform {
	return ProfiledPlatform{profile: copyProfile(profile)}
}

func (p ProfiledPlatform) Name() string { return p.profile.Name }

func (p ProfiledPlatform) Profile() connector.Profile {
	return copyProfile(p.profile)
}

func copyProfile(profile connector.Profile) connector.Profile {
	copyPattern := func(pattern *regexp.Regexp) *regexp.Regexp {
		if pattern == nil {
			return nil
		}
		return pattern.Copy() //nolint:staticcheck // Copy preserves POSIX/Longest semantics.
	}
	for _, list := range []*[]*regexp.Regexp{&profile.AuthenticationErrors, &profile.CommandErrors} {
		*list = append([]*regexp.Regexp(nil), (*list)...)
		for i, pattern := range *list {
			(*list)[i] = copyPattern(pattern)
		}
	}
	for _, list := range []*[]connector.Interaction{&profile.LoginInteractions, &profile.Interactions} {
		*list = append([]connector.Interaction(nil), (*list)...)
	}
	if profile.LegacySSHArguments != nil {
		profile.LegacySSHArguments = append([]string{}, profile.LegacySSHArguments...)
	}
	profile.ConfigCommands = append([]string(nil), profile.ConfigCommands...)
	profile.SaveCommands = append([]string(nil), profile.SaveCommands...)
	return profile
}

// PositionMarkerCommandErrors returns the rejection rules of CLIs that report
// errors with "% ... found at '^' position." plus a bare caret marker line.
func PositionMarkerCommandErrors() []*regexp.Regexp {
	return Regexps(
		`(?im)^[ \t]*%[ \t]+(?:(?:Unrecognized|Ambiguous|Incomplete)\s+command|Too\s+many\s+parameters)\s+found\s+at\s+'\^'\s+position\.`,
		`(?im)^[ \t]*error:`,
		`(?im)^[ \t]*Permission\s+denied\.`,
		`(?m)^\s*\^`,
	)
}

// YesNoConfirmations returns the replies for [Y/N] confirmations, the
// "press the enter key" pause and the default *.cfg file-name prompt.
func YesNoConfirmations() []connector.Interaction {
	return []connector.Interaction{
		Interaction(`(?i)Are\s+you\s+sure\?\s*\[Y/N\]`, "y\n"),
		Interaction(`(?i)press\s+the\s+enter\s+key\)`, "\n"),
		Interaction(`(?i)Continue\?\s*\[Y/N\]`, "y\n"),
		Interaction(`(?i)Please\s+input\s+the\s+file\s+name\(\*\.cfg\)\[[^\]]+\]\s*\z`, "\n"),
		Interaction(`(?i)overwrite\?\s*\[Y/N\]`, "y\n"),
	}
}
