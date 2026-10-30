package transfer

import (
	"regexp"
	"strings"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/terminal"
)

var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)
var failures = regexp.MustCompile(`(?i)error|failed|failure|aborted|denied|timed out|timeout|not found|no such file|unable to|network is unreachable|can't open`)

func word(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}
func failed(s string) bool {
	for _, loc := range failures.FindAllStringIndex(s, -1) {
		a, b := loc[0], loc[1]
		if a > 0 && (word(s[a-1]) || strings.ContainsRune("./-", rune(s[a-1]))) {
			continue
		}
		if b < len(s) && (word(s[b]) || strings.ContainsRune("/-", rune(s[b])) || s[b] == '.' && b+1 < len(s) && word(s[b+1])) {
			continue
		}
		return true
	}
	return false
}
func evidenceLines(step connector.StepResult, output string) []string {
	prompt, _ := terminal.Render([]byte(step.Prompt()), true)
	prompt = strings.TrimSpace(prompt)
	command := step.Command()
	echoes := map[string]bool{command.Text: true, prompt + command.Text: true, prompt: true}
	for _, reply := range command.Interactions {
		if reply.Respond == nil {
			echoes[strings.TrimSuffix(reply.Reply, "\n")] = true
		}
	}
	out := []string{}
	for _, line := range strings.FieldsFunc(output, func(c rune) bool { return c == '\r' || c == '\n' }) {
		line = strings.TrimSpace(sgr.ReplaceAllString(line, ""))
		if line != "" && !echoes[line] {
			out = append(out, line)
		}
	}
	return out
}
func evidence(steps []connector.StepResult) (lines, outputs []string, err error) {
	for i, step := range steps {
		raw := step.Output()
		rendered, e := terminal.Render([]byte(raw), true)
		if e != nil {
			return nil, nil, e
		}
		history, e := terminal.Render([]byte(strings.ReplaceAll(raw, "\r", "\n")), true)
		if e != nil {
			return nil, nil, e
		}
		for _, output := range []string{raw, history, rendered} {
			outputs = append(outputs, strings.Join(evidenceLines(step, output), "\n"))
		}
		if i == len(steps)-1 {
			prompt, _ := terminal.Render([]byte(step.Prompt()), true)
			if prompt != "" {
				rendered = strings.TrimSuffix(rendered, prompt)
			}
			lines = evidenceLines(step, rendered)
		}
	}
	return lines, outputs, nil
}

var copied = regexp.MustCompile(`(?i)^[1-9]\d*\s+bytes?\s+copied(?:\s+in\s+[\d.]+\s+(?:secs?|seconds?)(?:\s+\([\d.]+\s+bytes/sec\))?)?[.!]?$`)

var completionPatterns = map[string][]*regexp.Regexp{
	"cisco_ios":  {regexp.MustCompile(`(?i)^\[OK(?:\s*-\s*[1-9]\d*\s+bytes)?\]$`)},
	"cisco_nxos": {regexp.MustCompile(`(?i)^(?:transfer|copy)(?:\s+operation)?\s+(?:success(?:ful(?:ly)?)?|succeeded|complete(?:d)?(?:\s+successfully)?)[.!]?$`)},
	"fortinet":   {regexp.MustCompile(`(?i)^Send config file to tftp server OK\.?$`)},
	"centec":     {regexp.MustCompile(`(?i)^Sent\s+[1-9]\d*\s+bytes\s+in\s+[\d.]+\s+seconds?[.!]?$`)},
	"hillstone":  {regexp.MustCompile(`(?i)^Export\s+ok\s*,\s*target\s+file\s+name\s+\S+$`)},
	"palo_alto":  {regexp.MustCompile(`(?i)^Sent\s+[1-9]\d*\s+bytes(?:\s+in\s+[\d.]+\s+(?:secs?|seconds?))?(?:\s+\[\d+(?:\.\d+)?\s+bit/s\])?[.!]?$`)},
	"radware":    {regexp.MustCompile(`(?i)^Current\s+config\s+successfully\s+transferred\s+to\s+[^\s:]+:\S+$`), regexp.MustCompile(`(?i)^(?:Current\s+)?(?:configuration|config)\s+(?:successfully\s+(?:uploaded|tftp'd)|(?:uploaded|tftp'd)(?:\s+successfully)?|complete(?:d)?(?:\s+successfully)?)[.!]?$`)},
}

var fileCompletionPatterns = []*regexp.Regexp{regexp.MustCompile(`(?i)^(?:File\s+)?(?:transfer(?:red)?|upload(?:ed)?)(?:\s+(?:is|was))?\s+(?:success(?:ful(?:ly)?)?|succeeded|complete(?:d)?(?:\s+successfully)?)[.!]?$`), regexp.MustCompile(`(?i)^(?:TFTP:\s*)?[1-9]\d*\s+bytes?\s+sent(?:\s+in\s+[\d.]+\s+(?:secs?|seconds?|second\(s\)))?[.!]?$`)}
var fileCompletionProgress = regexp.MustCompile(`(?i)^100\s+([1-9]\d*(?:\.\d+)?[kMGT]?)\s+0\s+0\s+100\s+([1-9]\d*(?:\.\d+)?[kMGT]?)(?:\s+[\d.:kMGT-]+)*$`)

func completed(vendor string, lines []string) bool {
	file := vendor == "h3c" || vendor == "h3c_wireless" || vendor == "huawei"
	patterns := completionPatterns[vendor]
	if file {
		patterns = fileCompletionPatterns
	}
	for _, line := range lines {
		if (vendor == "cisco_ios" || vendor == "cisco_nxos") && copied.MatchString(line) {
			return true
		}
		for _, pattern := range patterns {
			if pattern.MatchString(line) {
				return true
			}
		}
		if file {
			m := fileCompletionProgress.FindStringSubmatch(line)
			if m != nil && m[1] == m[2] {
				return true
			}
		}
	}
	return false
}
