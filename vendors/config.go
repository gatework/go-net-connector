package vendors

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/terminal"
)

type configStrategy struct {
	vendor string
	diffs  int
}

func (s *configStrategy) Prompt(command, current string) string {
	text, e := terminal.Render([]byte(current), false)
	if e != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(text), "\n")
	prompt := strings.TrimSpace(parts[len(parts)-1])
	if s.vendor == "radware" && command == "/cfg/dump" {
		return radwareConfigPrompt.ReplaceAllString(prompt, "${1}Configuration#")
	}
	if s.vendor == "palo_alto" && (command == "configure" || command == "exit") {
		if strings.HasSuffix(prompt, ">") || strings.HasSuffix(prompt, "#") {
			suffix := "#"
			if command == "exit" {
				suffix = ">"
			}
			prompt = prompt[:len(prompt)-1] + suffix
		}
	}
	return prompt
}
func (s *configStrategy) Validate(_ context.Context, c connector.Command, r connector.StepResult, _ connector.Executor) error {
	if s.vendor == "palo_alto" && c.Text == "show config diff" {
		s.diffs++
		if panBody(r.Output(), c.Text, r.Prompt()) != "" {
			return fmt.Errorf("vendors: uncommitted_configuration: %w", connector.ErrIncomplete)
		}
	}
	return nil
}
func (s *configStrategy) Finish(ctx context.Context, result connector.Result) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	steps := result.Steps()
	if len(steps) == 0 {
		return "", connector.ErrIncomplete
	}
	step := steps[len(steps)-1]
	if s.vendor == "palo_alto" {
		if s.diffs != 2 {
			return "", connector.ErrIncomplete
		}
		found := false
		for _, candidate := range steps {
			if candidate.Command().Text == "show" {
				step = candidate
				found = true
				break
			}
		}
		if !found {
			return "", connector.ErrIncomplete
		}
	}
	visible, err := step.Rendered()
	if err != nil {
		return "", connector.ErrIncomplete
	}
	prompt, err := terminal.RenderString(step.Prompt(), true)
	if err != nil {
		return "", connector.ErrIncomplete
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || renderedBody(visible, step.Command().Text, prompt) == "" {
		return "", connector.ErrIncomplete
	}
	if s.vendor == "hillstone" || s.vendor == "palo_alto" {
		return s.clean(step.Output(), strings.TrimSpace(step.Prompt()))
	}
	return s.cleanRendered(visible, prompt)
}

// Remove only response boundaries when checking evidence; this does not rewrite
// the configuration returned by dialects which preserve CLI echoes and prompts.
func renderedBody(text, command, prompt string) string {
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), prompt))
	first, rest, ok := strings.Cut(text, "\n")
	if first == command || first == strings.TrimSpace(prompt)+command {
		if !ok {
			return ""
		}
		text = strings.TrimSpace(rest)
	}
	return text
}

var hillstonePaging = re(`\x00?\x08+[ \t]+\x08+`)
var radwareConfigPrompt = re(`\A(>>[ \t]+(?:.* - )?)[^\r\n#]+#\z`)

var ciscoHeader = re(`(?i)\A(?:[ \t]*\n|![^\n]*\n|(?:\S+[#])?show[ \t]+running-config[^\n]*\n|Building[ \t]+configuration[^\n]*\n|Current[ \t]+configuration[^\n]*\n)*`)
var ciscoVolatile = re(`(?im)^!.*(?:Last\s+configuration\s+change|NVRAM\s+config).*\n`)

func (s *configStrategy) clean(text, prompt string) (string, error) {
	switch s.vendor {
	case "hillstone":
		return hillstonePaging.ReplaceAllString(text, ""), nil
	case "palo_alto":
		// PAN output is deliberately parsed as set text rather than XML or terminal
		// rendering. Multiline quoted values must close before another command.
		text = panBody(text, "show", prompt)
		text = re(`(?:\A|\n)[ \t]*\[edit\][ \t]*\z`).ReplaceAllString(text, "")
		text = strings.TrimSpace(text)
		if !setCommands(text) {
			return "", fmt.Errorf("vendors: unsupported_configuration_format: %w", connector.ErrIncomplete)
		}
		return text, nil
	}
	rendered, e := terminal.RenderString(text, false)
	if e != nil {
		return "", connector.ErrIncomplete
	}
	return s.cleanRendered(rendered, prompt)
}
func (s *configStrategy) cleanRendered(rendered, prompt string) (string, error) {
	switch s.vendor {
	case "cisco_ios", "cisco_nxos":
		header := ciscoHeader.FindString(rendered)
		cleaned := ciscoVolatile.ReplaceAllString(header, "")
		if cleaned == header {
			return rendered, nil
		}
		return cleaned + rendered[len(header):], nil
	case "centec":
		rendered = re(`\A(?:`+regexp.QuoteMeta(prompt)+`)?show\s+running-config[ \t]*\n`).ReplaceAllString(rendered, "")
		rendered = re(`(?:\A|\n)`+regexp.QuoteMeta(prompt)+`[ \t]*\z`).ReplaceAllString(rendered, "\n")
	}
	return rendered, nil
}

// PAN quoted values retain internal whitespace, including multiline certificates.
func panBody(text, command, prompt string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	prompt = strings.ReplaceAll(prompt, "\r\n", "\n")
	text = strings.TrimSuffix(text, prompt)
	first, rest, ok := strings.Cut(text, "\n")
	if strings.TrimSpace(first) == command {
		if !ok {
			return ""
		}
		text = rest
	}
	return strings.TrimSpace(text)
}
func setCommands(text string) bool {
	if text == "" {
		return false
	}
	var quote byte
	for _, line := range strings.SplitAfter(text, "\n") {
		if quote == 0 {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if !strings.HasPrefix(line, "set ") {
				return false
			}
		}
		escaped := false
		for i := 0; i < len(line); i++ {
			b := line[i]
			if escaped {
				escaped = false
			} else if b == '\\' && quote != '\'' {
				escaped = true
			} else if b == quote {
				quote = 0
			} else if quote == 0 && (b == '\'' || b == '"') {
				quote = b
			}
		}
	}
	return quote == 0
}
