package connector

import (
	"errors"
	"strings"
)

// SingleAccepted returns the only accepted main command when its text exactly
// matches expectedCommand. Hook attempts do not count as main commands.
// It reports acceptance, not overall execution success: callers must separately
// handle the execution error, including errors returned after command acceptance.
// An absent, ambiguous or mismatched receipt returns ErrInvalidResult.
func (r Result) SingleAccepted(expectedCommand string) (StepResult, error) {
	if strings.TrimSpace(expectedCommand) == "" || len(r.steps) != 1 {
		return StepResult{}, invalidResult(Command{}, nil)
	}
	step := r.steps[0]
	if step.command.Text != expectedCommand {
		return StepResult{}, invalidResult(step.command, nil)
	}
	return step, nil
}

// Body returns strictly decoded, terminal-visible text without the confirmed
// prompt and an optional matching first-line command echo. It preserves all
// other whitespace, including leading newlines in the matched prompt, which
// separate the body from the prompt. It does not apply vendor configuration
// cleanup or treat command-like lines later in the body as echoes.
// Missing verified text, backup-only preservation views and inconsistent prompts
// return ErrInvalidResult; any text conversion cause remains in the error chain.
// Error formatting never includes command or response text.
func (s StepResult) Body() (string, error) {
	text, err := s.response.Rendered()
	if err != nil {
		return "", invalidResult(s.command, err)
	}
	prompt, err := s.response.Prompt()
	if err != nil {
		return "", invalidResult(s.command, err)
	}
	if !s.response.data.verifiedText {
		return "", invalidResult(s.command, ErrIncomplete)
	}
	prompt = strings.TrimLeft(prompt, "\n")
	if prompt == "" {
		return "", invalidResult(s.command, nil)
	}
	body, ok := strings.CutSuffix(text, prompt)
	if !ok {
		return "", invalidResult(s.command, nil)
	}
	first, rest, _ := strings.Cut(body, "\n")
	if strings.TrimSpace(first) == strings.TrimSpace(s.command.Text) {
		return rest, nil
	}
	return body, nil
}

func invalidResult(command Command, cause error) error {
	return newError("invalid_command_result", "parse", command, errors.Join(ErrInvalidResult, cause))
}
