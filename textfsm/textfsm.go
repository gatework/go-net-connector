// Package textfsm parses device output with independent, embedded TextFSM templates.
package textfsm

import (
	"bufio"
	"embed"
	"encoding/csv"
	"errors"
	"io"
	"io/fs"
	"maps"
	"regexp"
	"strings"
	"sync"
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

type indexEntry struct {
	vendor, command *regexp.Regexp
	template        string
}
type catalog struct {
	index     []indexEntry
	templates map[string]func() (fsm.TextFSM, error)
}

var embeddedCatalog = sync.OnceValues(loadCatalog)

func loadCatalog() (*catalog, error) {
	c := &catalog{templates: make(map[string]func() (fsm.TextFSM, error))}
	err := fs.WalkDir(templates, "templates", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".textfsm") {
			return nil
		}
		c.templates[strings.TrimPrefix(path, "templates/")] = sync.OnceValues(func() (fsm.TextFSM, error) {
			data, err := templates.ReadFile(path)
			if err != nil {
				return fsm.TextFSM{}, ErrTemplateMissing
			}
			return compileTemplate(string(data))
		})
		return nil
	})
	if err != nil {
		return nil, ErrTemplateInvalid
	}
	data, err := templates.ReadFile("templates/index")
	if err != nil {
		return nil, ErrTemplateInvalid
	}
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil || len(header) != 3 || header[0] != "Template" || header[1] != "Vendor" || header[2] != "Command" {
		return nil, ErrTemplateInvalid
	}
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return c, nil
		}
		if err != nil || len(row) != 3 {
			return nil, ErrTemplateInvalid
		}
		vendor, err := regexp.Compile(strings.TrimSpace(row[1]))
		if err != nil {
			return nil, ErrTemplateInvalid
		}
		command, err := regexp.Compile(strings.TrimSpace(row[2]))
		if err != nil {
			return nil, ErrTemplateInvalid
		}
		c.index = append(c.index, indexEntry{vendor, command, strings.TrimSpace(row[0])})
	}
}

// Parse selects a template using the complete vendor key and command.
func Parse(vendor, command, output string) ([]map[string]any, error) {
	c, err := embeddedCatalog()
	if err != nil {
		return nil, err
	}
	for _, entry := range c.index {
		if entry.vendor.MatchString(vendor) && entry.command.MatchString(command) {
			return ParseTemplate(entry.template, output)
		}
	}
	return nil, ErrTemplateMissing
}

// compileTemplate contains dependency panic paths at the untrusted input boundary.
func compileTemplate(source string) (machine fsm.TextFSM, err error) {
	defer func() {
		if recover() != nil {
			machine = fsm.TextFSM{}
			err = ErrTemplateInvalid
		}
	}()
	if !utf8.ValidString(source) {
		return machine, ErrEncoding
	}
	if err = machine.ParseScanner(newScanner(source)); err != nil {
		return fsm.TextFSM{}, ErrTemplateInvalid
	}
	return machine, nil
}

// ParseTemplate accepts TextFSM source or an embedded template path (for example
// cisco/show_running_config.textfsm). It does not read arbitrary filesystem paths.
// Errors deliberately discard dependency messages containing device text.
func ParseTemplate(template, output string) (rows []map[string]any, err error) {
	if !utf8.ValidString(output) {
		return nil, ErrEncoding
	}
	var machine fsm.TextFSM
	if !strings.Contains(template, "\n") && strings.HasSuffix(template, ".textfsm") {
		c, e := embeddedCatalog()
		if e != nil {
			return nil, e
		}
		load, ok := c.templates[template]
		if !ok {
			return nil, ErrTemplateMissing
		}
		machine, err = load()
	} else {
		machine, err = compileTemplate(template)
	}
	if err != nil {
		return nil, err
	}
	// gotextfsm mutates Values during parsing. The cached pristine values and
	// read-only states/regexps are never handed to its parser directly.
	machine.Values = maps.Clone(machine.Values)
	// The dependency has panic paths for malformed custom expressions; keep that
	// implementation detail from escaping this untrusted-input boundary.
	defer func() {
		if recover() != nil {
			rows = nil
			err = ErrTemplateInvalid
		}
	}()
	text, e := terminal.RenderString(output, true)
	if e != nil {
		if errors.Is(e, terminal.ErrEncoding) {
			return nil, ErrEncoding
		}
		return nil, ErrIncomplete
	}
	var parsed fsm.ParserOutput
	scanner := newScanner(text)
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
