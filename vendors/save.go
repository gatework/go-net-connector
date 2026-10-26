package vendors

import (
	"context"
	"strings"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/terminal"
)

// Keep the established topology persistence proof at the shared profile boundary.
func validateSave(ctx context.Context, vendor string, result connector.Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pattern := `(?i)^(?:\[OK\]|Copy complete\.)$`
	switch vendor {
	case "h3c", "h3c_wireless":
		pattern = `(?i)^Saved the current configuration to mainboard device successfully\.$`
	case "hillstone":
		pattern = `(?i)^Saving configuration is finished\.?$`
	case "huawei":
		pattern = `(?i)^(?:Info:[ \t]*)?(?:Save the configuration successfully\.|The configuration has been saved successfully\.|Configuration is saved to device successfully\.)$`
	case "cisco_ios", "cisco_nxos", "centec":
	default:
		return connector.ErrUnconfirmed // no verified firmware-specific success policy
	}
	steps := result.Steps()
	if len(steps) == 0 {
		return connector.ErrUnconfirmed
	}
	failed := re(`(?i)\b(?:error|failed|failure|denied|cannot|unable|unsuccessful|aborted)\b`)
	for _, step := range steps {
		raw := strings.TrimSuffix(step.Raw(), step.Prompt())
		history := re(`\x1b\[[0-9;:]*m`).ReplaceAllString(raw, "")
		history = strings.ReplaceAll(history, "\r", "\n")
		rendered, err := terminal.Render([]byte(raw), false)
		if err != nil {
			return connector.ErrUnconfirmed
		}
		historyRendered, err := terminal.Render([]byte(history), false)
		if err != nil || failed.MatchString(history) || failed.MatchString(historyRendered) || failed.MatchString(rendered) {
			return connector.ErrUnconfirmed
		}
		confirmed := false
		for _, line := range strings.Split(rendered, "\n") {
			if re(pattern).MatchString(strings.TrimSpace(line)) {
				confirmed = true
			}
		}
		if !confirmed {
			return connector.ErrUnconfirmed
		}
	}
	return ctx.Err()
}
