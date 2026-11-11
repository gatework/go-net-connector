// Package config loads immutable public policy and separate credential snapshots.
package config

import (
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

var ErrInvalid = errors.New("config: invalid configuration")

const MaxBytes = 16 << 20

const maxDepth = 128
const maxNodes = 100000

func ParseYAML(data []byte) (map[string]any, error) {
	return parseYAML(data, false)
}

// ParseYAMLMapping requires an explicit root mapping, including an empty {}.
// Empty documents and root nulls are invalid. All other YAML restrictions and
// resource budgets are shared with ParseYAML.
func ParseYAMLMapping(data []byte) (map[string]any, error) {
	return parseYAML(data, true)
}

func parseYAML(data []byte, requireMapping bool) (map[string]any, error) {
	if len(data) > MaxBytes {
		return nil, ErrInvalid
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if e := dec.Decode(&root); e != nil {
		if errors.Is(e, io.EOF) && !requireMapping {
			return map[string]any{}, nil
		}
		return nil, ErrInvalid
	}
	var extra yaml.Node
	if e := dec.Decode(&extra); !errors.Is(e, io.EOF) {
		return nil, ErrInvalid
	}
	remaining := maxNodes
	var parseNode func(*yaml.Node, int) (any, error)
	parseNode = func(n *yaml.Node, depth int) (any, error) {
		remaining--
		if depth > maxDepth || remaining < 0 || n.Kind == yaml.AliasNode || n.Anchor != "" {
			return nil, ErrInvalid
		}
		switch n.Kind {
		case yaml.DocumentNode:
			if len(n.Content) != 1 {
				return nil, ErrInvalid
			}
			return parseNode(n.Content[0], depth+1)
		case yaml.MappingNode:
			if n.Tag != "!!map" {
				return nil, ErrInvalid
			}
			m := map[string]any{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				remaining--
				if remaining < 0 || k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Anchor != "" {
					return nil, ErrInvalid
				}
				if _, ok := m[k.Value]; ok {
					return nil, ErrInvalid
				}
				v, e := parseNode(n.Content[i+1], depth+1)
				if e != nil {
					return nil, e
				}
				m[k.Value] = v
			}
			return m, nil
		case yaml.SequenceNode:
			if n.Tag != "!!seq" {
				return nil, ErrInvalid
			}
			a := make([]any, 0, len(n.Content))
			for _, c := range n.Content {
				v, e := parseNode(c, depth+1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			return a, nil
		case yaml.ScalarNode:
			switch n.Tag {
			case "!!str":
				return n.Value, nil
			case "!!null":
				return nil, nil //nolint:nilnil // YAML null decodes to a nil value
			case "!!bool":
				var v bool
				if n.Decode(&v) != nil {
					return nil, ErrInvalid
				}
				return v, nil
			case "!!int":
				var v int64
				if n.Decode(&v) != nil {
					return nil, ErrInvalid
				}
				return int(v), nil
			case "!!float":
				var v float64
				if n.Decode(&v) != nil || math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, ErrInvalid
				}
				return v, nil
			}
			return nil, ErrInvalid
		case yaml.AliasNode: // rejected before the switch; aliases could expand without bound
			return nil, ErrInvalid
		}
		return nil, ErrInvalid
	}
	v, e := parseNode(&root, 0)
	if e != nil {
		return nil, e
	}
	if v == nil && !requireMapping {
		return map[string]any{}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ErrInvalid
	}
	return m, nil
}
func validEnvironmentKey(key string) bool {
	if key == "" {
		return false
	}
	for i, c := range key {
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// ParseEnvironment never executes shell syntax or consults the process environment.
func ParseEnvironment(text string, inherited, process map[string]string) (map[string]string, error) {
	if len(text) > MaxBytes {
		return nil, ErrInvalid
	}
	local := map[string]string{}
	total, err := environmentSize(inherited)
	if err != nil {
		return nil, err
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lookup := func(k string) string {
		if v, ok := process[k]; ok {
			return v
		}
		if v, ok := local[k]; ok {
			return v
		}
		return inherited[k]
	}
	for len(text) > 0 {
		text = strings.TrimLeft(text, " \t\r\n")
		if text == "" {
			break
		}
		if text[0] == '#' {
			_, text, _ = strings.Cut(text, "\n")
			continue
		}
		if strings.HasPrefix(text, "export ") {
			text = strings.TrimLeft(text[7:], " \t")
		}
		equal := strings.IndexByte(text, '=')
		if equal < 0 || strings.Contains(text[:equal], "\n") {
			return nil, ErrInvalid
		}
		key := strings.TrimSpace(text[:equal])
		if !validEnvironmentKey(key) {
			return nil, ErrInvalid
		}
		text = strings.TrimLeft(text[equal+1:], " \t")
		quote := byte(0)
		if len(text) > 0 && (text[0] == '\'' || text[0] == '"') {
			quote = text[0]
			text = text[1:]
		}
		// Reserve the effective snapshot's other entries before expanding this value.
		base := total
		if old, ok := local[key]; ok {
			base -= len(key) + len(old)
		} else if old, ok := inherited[key]; ok {
			base -= len(key) + len(old)
		}
		if len(key) > MaxBytes-base {
			return nil, ErrInvalid
		}
		base += len(key)
		var b strings.Builder
		write := func(value string) error {
			if len(value) > MaxBytes-b.Len() || len(value) > MaxBytes-base-b.Len() {
				return ErrInvalid
			}
			b.WriteString(value)
			return nil
		}
		writeByte := func(value byte) error {
			if b.Len() >= MaxBytes || b.Len() >= MaxBytes-base {
				return ErrInvalid
			}
			b.WriteByte(value)
			return nil
		}
		closed := quote == 0
		for len(text) > 0 {
			c := text[0]
			text = text[1:]
			if quote != 0 && c == quote {
				closed = true
				break
			}
			if quote == 0 && (c == '\n' || c == '#') {
				if c == '#' {
					_, text, _ = strings.Cut(text, "\n")
				}
				break
			}
			if c == '\\' && quote != '\'' && len(text) > 0 {
				next := text[0]
				if next == '$' || next == '"' || next == '\\' || next == '#' || next == ' ' {
					if writeByte(next) != nil {
						return nil, ErrInvalid
					}
					text = text[1:]
					continue
				}
			}
			if c == '$' && quote != '\'' {
				if strings.HasPrefix(text, "(") {
					return nil, ErrInvalid
				}
				var name string
				if strings.HasPrefix(text, "{") {
					end := strings.IndexByte(text, '}')
					if end < 0 {
						return nil, ErrInvalid
					}
					name = text[1:end]
					text = text[end+1:]
					if !validEnvironmentKey(name) {
						return nil, ErrInvalid
					}
				} else {
					n := 0
					for n < len(text) && (text[n] == '_' || text[n] >= 'A' && text[n] <= 'Z' || text[n] >= 'a' && text[n] <= 'z' || n > 0 && text[n] >= '0' && text[n] <= '9') {
						n++
					}
					name = text[:n]
					text = text[n:]
				}
				if name != "" {
					if write(lookup(name)) != nil {
						return nil, ErrInvalid
					}
					continue
				}
			}
			if writeByte(c) != nil {
				return nil, ErrInvalid
			}
		}
		if !closed {
			return nil, ErrInvalid
		}
		value := b.String()
		if quote == 0 {
			value = strings.TrimRightFunc(value, unicode.IsSpace)
		} else {
			line, rest, _ := strings.Cut(text, "\n")
			if tail := strings.TrimSpace(line); tail != "" && !strings.HasPrefix(tail, "#") {
				return nil, ErrInvalid
			}
			text = rest
		}
		if strings.IndexByte(value, 0) >= 0 {
			return nil, ErrInvalid
		}
		local[key] = value
		total = base + len(value)
	}
	return local, nil
}

// environmentSize bounds an effective environment snapshot, including key bytes.
func environmentSize(values map[string]string) (int, error) {
	total := 0
	for key, value := range values {
		if len(key) > MaxBytes-total {
			return 0, ErrInvalid
		}
		total += len(key)
		if len(value) > MaxBytes-total {
			return 0, ErrInvalid
		}
		total += len(value)
	}
	return total, nil
}

// copyPlain owns canonical maps and slices and rejects opaque mutable values.
func copyPlain(v any) (any, error) {
	nodes, bytes := maxNodes, MaxBytes
	active := map[uintptr]bool{}
	var copyValue func(any, int) (any, error)
	copyValue = func(v any, depth int) (any, error) {
		nodes--
		if depth > maxDepth || nodes < 0 {
			return nil, ErrInvalid
		}
		switch x := v.(type) {
		case map[string]any:
			id := uintptr(reflect.ValueOf(x).UnsafePointer())
			if active[id] || len(x) > nodes/2 {
				return nil, ErrInvalid
			}
			active[id] = true
			defer delete(active, id)
			m := make(map[string]any, len(x))
			for k, item := range x {
				nodes--
				if nodes < 0 || len(k) > bytes {
					return nil, ErrInvalid
				}
				bytes -= len(k)
				c, e := copyValue(item, depth+1)
				if e != nil {
					return nil, e
				}
				m[k] = c
			}
			return m, nil
		case []any:
			id := reflect.ValueOf(x).Pointer()
			if len(x) > nodes || (len(x) > 0 && active[id]) {
				return nil, ErrInvalid
			}
			if len(x) > 0 {
				active[id] = true
				defer delete(active, id)
			}
			a := make([]any, len(x))
			for i, item := range x {
				c, e := copyValue(item, depth+1)
				if e != nil {
					return nil, e
				}
				a[i] = c
			}
			return a, nil
		case string:
			if len(x) > bytes {
				return nil, ErrInvalid
			}
			bytes -= len(x)
			return x, nil
		case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return v, nil
		case float64:
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return nil, ErrInvalid
			}
			return x, nil
		default:
			return nil, ErrInvalid
		}
	}
	return copyValue(v, 0)
}

// cloneValue is only used after inputs and the complete snapshot passed copyPlain.
func cloneValue(v any) any {
	value, _ := copyPlain(v)
	return value
}
func mergeValues(destination, source map[string]any) {
	for key, value := range source {
		existing, existingIsMap := destination[key].(map[string]any)
		incoming, incomingIsMap := value.(map[string]any)
		if existingIsMap && incomingIsMap {
			mergeValues(existing, incoming)
		} else {
			destination[key] = cloneValue(value)
		}
	}
}
func integerValue(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		if n >= math.MinInt && n <= math.MaxInt {
			return int(n), true
		}
	case uint:
		if n <= math.MaxInt {
			return int(n), true
		}
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		if uint64(n) <= uint64(math.MaxInt) {
			return int(n), true
		}
	case uint64:
		if n <= uint64(math.MaxInt) {
			return int(n), true
		}
	}
	return 0, false
}
func decodeEnv(s string, kind FieldKind) (any, error) {
	switch kind {
	case KindInteger, KindOptionalInteger:
		n, e := strconv.Atoi(s)
		return n, e
	case KindNumber:
		n, e := strconv.ParseFloat(s, 64)
		return n, e
	case KindBoolean:
		if s != "true" && s != "false" {
			return nil, ErrInvalid
		}
		return s == "true", nil
	case KindList:
		a := []any{}
		if s == "" {
			return a, nil
		}
		for v := range strings.SplitSeq(s, ",") {
			a = append(a, strings.TrimSpace(v))
		}
		return a, nil
	case KindJSON, KindJSONArray:
		return decodeJSON(s)
	default:
		return s, nil
	}
}
