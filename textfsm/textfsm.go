// Package textfsm parses device output with independent, embedded TextFSM templates.
package textfsm

import (
	"bufio"
	"embed"
	"encoding/csv"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gatework/go-net-connector/internal/terminal"
	fsm "github.com/sirikothe/gotextfsm"
)

var (
	ErrTemplateMissing = errors.New("textfsm: template missing")
	ErrTemplateInvalid = errors.New("textfsm: invalid template")
	ErrParse           = errors.New("textfsm: parse failed")
	ErrEncoding        = errors.New("textfsm: invalid encoding")
	ErrIncomplete      = errors.New("textfsm: incomplete output")
)

//go:embed templates
var templates embed.FS

// Parse selects a template using the complete vendor key and command.
func Parse(vendor, command, output string) ([]map[string]any, error) {
	data, e := templates.ReadFile("templates/index")
	if e != nil {
		return nil, ErrTemplateInvalid
	}
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.TrimLeadingSpace = true
	header, e := reader.Read()
	if e != nil || len(header) != 3 || header[0] != "Template" || header[1] != "Vendor" || header[2] != "Command" {
		return nil, ErrTemplateInvalid
	}
	for {
		row, e := reader.Read()
		if errors.Is(e, io.EOF) {
			return nil, ErrTemplateMissing
		}
		if e != nil || len(row) != 3 {
			return nil, ErrTemplateInvalid
		}
		vr, e := regexp.Compile(strings.TrimSpace(row[1]))
		if e != nil {
			return nil, ErrTemplateInvalid
		}
		cr, e := regexp.Compile(strings.TrimSpace(row[2]))
		if e != nil {
			return nil, ErrTemplateInvalid
		}
		if vr.MatchString(vendor) && cr.MatchString(command) {
			data, e := templates.ReadFile("templates/" + strings.TrimSpace(row[0]))
			if e != nil {
				return nil, ErrTemplateMissing
			}
			return ParseTemplate(string(data), output)
		}
	}
}

// ParseTemplate accepts TextFSM source or an embedded template path (for example
// cisco/show_running_config.textfsm). It does not read arbitrary filesystem paths.
// Errors deliberately discard dependency messages containing device text.
func ParseTemplate(template, output string) (rows []map[string]any, err error) {
	if !strings.Contains(template, "\n") && strings.HasSuffix(template, ".textfsm") {
		data, e := templates.ReadFile("templates/" + template)
		if e != nil {
			return nil, ErrTemplateMissing
		}
		template = string(data)
	}
	if !utf8.ValidString(template) || !utf8.ValidString(output) {
		return nil, ErrEncoding
	}
	// The dependency has panic paths for malformed custom expressions; keep that
	// implementation detail from escaping this untrusted-input boundary.
	defer func() {
		if recover() != nil {
			rows = nil
			err = ErrTemplateInvalid
		}
	}()
	text, e := terminal.Render([]byte(output), true)
	if e != nil {
		if errors.Is(e, terminal.ErrEncoding) {
			return nil, ErrEncoding
		}
		return nil, ErrIncomplete
	}
	var machine fsm.TextFSM
	scanner := newScanner(template)
	if e = machine.ParseScanner(scanner); e != nil {
		return nil, ErrTemplateInvalid
	}
	var parsed fsm.ParserOutput
	scanner = newScanner(text)
	if e = parsed.ParseTextScanner(scanner, machine, true); e != nil {
		if scanner.Err() != nil {
			return nil, ErrIncomplete
		}
		return nil, ErrParse
	}
	return parsed.Dict, nil
}
func newScanner(text string) *bufio.Scanner {
	s := bufio.NewScanner(strings.NewReader(text))
	s.Buffer(make([]byte, 4096), (32<<20)+1)
	return s
}
