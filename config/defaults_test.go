package config

import "testing"

func TestDefaultsCoverRuntimeLimitsAndConnections(t *testing.T) {
	defaults := Defaults("backup")
	for path, want := range map[string]any{
		"netdisco.max_pages":           10000,
		"netdisco.max_devices":         100000,
		"netdisco.max_response_bytes":  16 << 20,
		"netdisco.max_inventory_bytes": 128 << 20,
		"netdisco.db_port":             5432,
		"netdisco.db_connect_timeout":  10,
		"backup.limit_per_platform":    0,
	} {
		if got := valueAtPath(defaults, path); got != want {
			t.Errorf("%s = %#v, want %#v", path, got, want)
		}
	}
}
