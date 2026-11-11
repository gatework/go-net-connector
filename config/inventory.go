package config

import "strings"

// InventoryEnvironment projects the public inventory environment into a policy
// document. It ignores unrelated variables and credentials. A field present in
// higher masks the environment value before decoding, including false, null,
// empty lists, and an explicitly replaced parent section. No files are read.
func InventoryEnvironment(values map[string]string, higher map[string]any) (map[string]any, error) {
	return environmentValues(values, higher, inventoryPath)
}

// ValidateInventory validates an inventory policy without discovering files,
// resolving paths, applying defaults, or reading credentials. Only the netdisco
// and inventory sections are accepted. The document is never modified.
func ValidateInventory(document map[string]any) error {
	for section := range document {
		if section != "netdisco" && section != "inventory" {
			return ErrInvalid
		}
	}
	owned, err := copyPlain(document)
	if err != nil {
		return err
	}
	values, _ := owned.(map[string]any)
	return validateValues(values)
}

func inventoryPath(path string) bool {
	return strings.HasPrefix(path, "netdisco.") || strings.HasPrefix(path, "inventory.")
}

func environmentValues(values map[string]string, higher map[string]any, include func(string) bool) (map[string]any, error) {
	if err := rejectRemovedEnvironment(values, include); err != nil {
		return nil, err
	}
	result := map[string]any{}
	remaining := MaxBytes
	for path, field := range schema {
		if include != nil && !include(path) || hasPath(higher, path) {
			continue
		}
		value, present := values[field.Env]
		if !present {
			continue
		}
		if len(value) > remaining {
			return nil, ErrInvalid
		}
		remaining -= len(value)
		decoded, err := decodeEnv(value, field.Kind)
		if err != nil {
			return nil, ErrInvalid
		}
		setPath(result, path, decoded)
	}
	return result, nil
}
