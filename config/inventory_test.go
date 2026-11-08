package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestInventoryEnvironmentMasksBeforeDecoding(t *testing.T) {
	values := map[string]string{
		"ND_CACHE_ENABLED": "invalid", "ND_QUERY_PARAMS": "invalid",
		"ND_PAGE_SIZE": "invalid", "NC_INCLUDE_HOSTS": "invalid",
		"ND_URL": "https://inventory.example/api", "ND_PASSWORD": "secret",
		"NC_PARALLEL": "invalid", "NC_PROTOCOL": "invalid",
	}
	higher := map[string]any{
		"netdisco":  map[string]any{"cache_enabled": false, "query_params": []any{}, "page_size": nil},
		"inventory": nil,
	}
	got, err := InventoryEnvironment(values, higher)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"netdisco": map[string]any{"url": values["ND_URL"]}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected projection: %#v", got)
	}
	if _, err = InventoryEnvironment(map[string]string{"ND_PAGE_SIZE": "invalid"}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unmasked invalid environment accepted: %v", err)
	}
}

func TestValidateInventoryUsesSharedSchemaWithoutMutating(t *testing.T) {
	document := map[string]any{
		"netdisco":  map[string]any{"source": "http", "cache_enabled": false, "page_size": 20},
		"inventory": map[string]any{"include_hosts": []any{}},
	}
	before := cloneValue(document)
	if err := ValidateInventory(document); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(document, before) {
		t.Fatal("validation changed the caller's policy")
	}
	for name, invalid := range map[string]map[string]any{
		"other section":     {"ssh": map[string]any{"protocol": "ssh"}},
		"unknown field":     {"netdisco": map[string]any{"unknown": true}},
		"noninteger":        {"netdisco": map[string]any{"page_size": 1.5}},
		"malformed section": {"inventory": false},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateInventory(invalid); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid policy accepted: %v", err)
			}
		})
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	if err := ValidateInventory(map[string]any{"inventory": map[string]any{"host_overrides": cycle}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cyclic policy accepted: %v", err)
	}
}

func TestInventoryEnvironmentBoundsOnlyEffectiveValues(t *testing.T) {
	values := map[string]string{"ND_QUERY_PARAMS": strings.Repeat("x", MaxBytes+1)}
	if _, err := InventoryEnvironment(values, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	higher := map[string]any{"netdisco": map[string]any{"query_params": []any{}}}
	if projected, err := InventoryEnvironment(values, higher); err != nil || len(projected) != 0 {
		t.Fatal("masked value should not be decoded or budgeted", err)
	}
}
