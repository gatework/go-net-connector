package config

import (
	"maps"
	"slices"
	"strings"
)

// removedFields maps configuration keys removed after v0.4.0 to their
// replacements. They are rejected instead of ignored so an old configuration
// cannot silently fall back to a platform default source file.
var removedFields = map[string]struct{ env, replacement string }{
	"tftp.h3c_source_file":          {"NC_H3C_TFTP_SOURCE_FILE", "tftp.source_files.h3c"},
	"tftp.h3c_wireless_source_file": {"NC_H3C_WIRELESS_TFTP_SOURCE_FILE", "tftp.source_files.h3c_wireless"},
	"tftp.huawei_source_file":       {"NC_HUAWEI_TFTP_SOURCE_FILE", "tftp.source_files.huawei"},
}

// rejectRemovedFields reports the first removed key present in a parsed layer.
func rejectRemovedFields(data map[string]any) error {
	for _, path := range sortedRemovedFields() {
		if hasPath(data, path) {
			return &FieldError{Field: path, Problem: "removed", Replacement: removedFields[path].replacement}
		}
	}
	return nil
}

// rejectRemovedEnvironment reports the first removed variable that is set and
// belongs to the projection selected by include (nil selects every field).
func rejectRemovedEnvironment(values map[string]string, include func(string) bool) error {
	for _, path := range sortedRemovedFields() {
		if include != nil && !include(path) {
			continue
		}
		if _, present := values[removedFields[path].env]; present {
			return &FieldError{Field: removedFields[path].env, Problem: "removed", Replacement: removedFields[path].replacement}
		}
	}
	return nil
}

func sortedRemovedFields() []string {
	return slices.Sorted(maps.Keys(removedFields))
}

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
