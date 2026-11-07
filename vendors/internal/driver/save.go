package driver

import (
	"context"
	"regexp"
	"strings"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/terminal"
)

var failure = regexp.MustCompile(`(?i)\b(?:error|failed|failure|denied|cannot|unable|unsuccessful|aborted)\b`)
var color = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// validateSaveConfirmation requires positive device confirmation and rejects failure evidence
// found in raw, rendered, or carriage-return-updated output.
func validateSaveConfirmation(ctx context.Context, result connector.Result, confirmation *regexp.Regexp) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if confirmation == nil {
		return connector.ErrUnconfirmed
	}
	steps := result.Steps()
	if len(steps) == 0 {
		return connector.ErrUnconfirmed
	}
	for _, step := range steps {
		raw := strings.TrimSuffix(step.Response().Raw(), step.Response().RawPrompt())
		history := color.ReplaceAllString(raw, "")
		history = strings.ReplaceAll(history, "\r", "\n")
		rendered, err := terminal.Render([]byte(raw), false)
		if err != nil {
			return connector.ErrUnconfirmed
		}
		historyRendered, err := terminal.Render([]byte(history), false)
		if err != nil || failure.MatchString(history) || failure.MatchString(historyRendered) || failure.MatchString(rendered) {
			return connector.ErrUnconfirmed
		}
		confirmed := false
		for _, line := range strings.Split(rendered, "\n") {
			if confirmation.MatchString(strings.TrimSpace(line)) {
				confirmed = true
			}
		}
		if !confirmed {
			return connector.ErrUnconfirmed
		}
	}
	return ctx.Err()
}
