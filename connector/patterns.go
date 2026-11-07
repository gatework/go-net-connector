package connector

import (
	"regexp"

	expect "github.com/gatework/go-expect"
)

var (
	defaultUsername       = mustPattern(`(?i)(?:login|username|name):\s*\z`)
	defaultPassword       = mustPattern(`(?i)password:\s*\z`)
	defaultPager          = mustPattern(`(?im)(?:^|[\r\n])[ \t]*(?:--More--|---- More ----)[ \t\x00\x08]*(?:\r|\n|\z)`)
	defaultAuthError      = regexp.MustCompile(`(?i)(?:authentication failed|permission denied|access denied|login incorrect)`)
	hostKeyQuestion       = mustPattern(`(?i)\(yes/no(?:/\[fingerprint\])?\)\?\s*\z`)
	keyPassphraseQuestion = mustPattern(`(?i)Enter passphrase for key[^\r\n]*:[ \t]*\z`)
	loginAuthError        = mustPattern(`(?i)(?:REMOTE HOST IDENTIFICATION|Host key verification failed|Connection refused|No route to host|Connection reset|(?:Operation|Connection) timed out)`)
	loginFailures         = []struct {
		pattern expect.Pattern
		cause   error
	}{
		{mustPattern(`(?i)Connection refused`), errConnectionRefused},
		{mustPattern(`(?i)(?:REMOTE HOST IDEN|IDENTIFICATION CHANGED)`), errHostKeyChanged},
		{mustPattern(`(?i)RSA modulus too small`), errRSATooSmall},
		{mustPattern(`(?i)Selected cipher type <unknown> not supported by server`), errUnsupportedCipher},
	}
)

func mustPattern(text string) expect.Pattern {
	p, e := expect.Compile(text)
	if e != nil {
		panic(e)
	}
	return p
}
