package config

import "strings"

func valueAtPath(values map[string]any, path string) any {
	var value any = values
	for part := range strings.SplitSeq(path, ".") {
		child, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = child[part]
	}
	return value
}

// initializationValues omits inherited defaults and environment values. Explicit
// YAML/CLI choices, including false and empty lists, must never be pruned by value.
func (s Snapshot) initializationValues() map[string]any {
	out := map[string]any{}
	for pointer, source := range s.fields {
		if source != "cli" && !strings.HasSuffix(source, ".yml") && !strings.HasSuffix(source, ".yaml") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
		var value any = s.data
		valid := true
		for i, part := range parts {
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
			m, ok := value.(map[string]any)
			if !ok {
				valid = false
				break
			}
			value, ok = m[parts[i]]
			if !ok {
				valid = false
				break
			}
		}
		if valid {
			setValueAtPath(out, parts, cloneValue(value))
		}
	}
	return out
}

func setValueAtPath(values map[string]any, parts []string, value any) {
	if len(parts) == 1 {
		values[parts[0]] = value
		return
	}
	child, ok := values[parts[0]].(map[string]any)
	if !ok {
		child = map[string]any{}
		values[parts[0]] = child
	}
	setValueAtPath(child, parts[1:], value)
}
