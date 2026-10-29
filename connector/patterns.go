package connector

import (
	"regexp"

	expect "github.com/gatework/go-expect"
)

var (
	defaultUsername       = regexp.MustCompile(`(?i)(?:login|username|name):\s*\z`)
	defaultPassword       = regexp.MustCompile(`(?i)password:\s*\z`)
	defaultPager          = regexp.MustCompile(`(?im)(?:^|[\r\n])[ \t]*(?:--More--|---- More ----)[ \t\x00\x08]*(?:\r|\n|\z)`)
	defaultAuthError      = regexp.MustCompile(`(?i)(?:authentication failed|permission denied|access denied|login incorrect)`)
	hostKeyQuestion       = regexp.MustCompile(`(?i)\(yes/no(?:/\[fingerprint\])?\)\?\s*\z`)
	keyPassphraseQuestion = regexp.MustCompile(`(?i)Enter passphrase for key[^\r\n]*:[ \t]*\z`)
	loginAuthError        = regexp.MustCompile(`(?i)(?:REMOTE HOST IDENTIFICATION|Host key verification failed|Connection refused|No route to host|Connection reset|(?:Operation|Connection) timed out)`)
	loginFailures         = []struct {
		pattern *regexp.Regexp
		cause   error
	}{
		{regexp.MustCompile(`(?i)Connection refused`), errConnectionRefused},
		{regexp.MustCompile(`(?i)(?:REMOTE HOST IDEN|IDENTIFICATION CHANGED)`), errHostKeyChanged},
		{regexp.MustCompile(`(?i)RSA modulus too small`), errRSATooSmall},
		{regexp.MustCompile(`(?i)Selected cipher type <unknown> not supported by server`), errUnsupportedCipher},
	}
)

// Only the private, snapshotted declarations are cached. Per-command patterns
// remain owned by that operation and never accumulate in a device-wide cache.
func (d *Device) compilePatterns() {
	d.patterns = make(map[*regexp.Regexp]expect.Pattern)
	add := func(r *regexp.Regexp) {
		if r != nil {
			d.patterns[r], _ = expect.FromRegexp(r)
		}
	}
	p := d.profile
	for _, r := range []*regexp.Regexp{p.LoginPrompt, p.CommandPrompt, p.UsernamePrompt, p.PasswordPrompt, p.PrivilegePrompt, p.Pager, hostKeyQuestion, keyPassphraseQuestion, loginAuthError} {
		add(r)
	}
	for _, r := range p.AuthenticationErrors {
		add(r)
	}
	for _, failure := range loginFailures {
		add(failure.pattern)
	}
	for _, rules := range [][]Interaction{p.LoginInteractions, p.Interactions, d.config.Challenges} {
		for _, r := range rules {
			add(r.Pattern)
		}
	}
}

func (d *Device) pattern(r *regexp.Regexp) expect.Pattern {
	if pattern, ok := d.patterns[r]; ok {
		return pattern
	}
	pattern, _ := expect.FromRegexp(r)
	return pattern
}
