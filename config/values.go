package config

import "strings"

// normalizeSourceFiles makes legacy inputs participate in the same layer merge
// as structured inputs. An explicit structured value in the same layer wins.
func normalizeSourceFiles(data map[string]any) {
	tftp, _ := data["tftp"].(map[string]any)
	for _, vendor := range []string{"h3c", "h3c_wireless", "huawei"} {
		key := vendor + "_source_file"
		value, exists := tftp[key]
		if !exists {
			continue
		}
		structured, provided := tftp["source_files"]
		files, ok := structured.(map[string]any)
		if provided && !ok {
			delete(tftp, key)
			continue
		}
		if files == nil {
			files = map[string]any{}
			tftp["source_files"] = files
		}
		if _, exists := files[vendor]; !exists {
			files[vendor] = value
		}
		delete(tftp, key)
	}
}

func canonicalPath(path string) string {
	for _, vendor := range []string{"h3c", "h3c_wireless", "huawei"} {
		if path == "tftp."+vendor+"_source_file" {
			return "tftp.source_files." + vendor
		}
	}
	return path
}

func valueAtPath(values map[string]any, path string) any {
	var value any = values
	for _, part := range strings.Split(path, ".") {
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
