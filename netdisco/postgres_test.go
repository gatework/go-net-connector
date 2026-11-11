package netdisco

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
	"time"
)

func TestPostgresTemporaryReadOnlyCursor(t *testing.T) {
	bin := os.Getenv("NC_TEST_POSTGRES_BIN")
	if bin == "" {
		t.Skip("set NC_TEST_POSTGRES_BIN for an isolated temporary PostgreSQL server")
	}
	root, e := os.MkdirTemp("/tmp", "ncpg-") //nolint:usetesting // PostgreSQL socket paths must stay short
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	data := filepath.Join(root, "data")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(filepath.Join(bin, args[0]), args[1:]...)
		if b, e := cmd.CombinedOutput(); e != nil {
			serverLog, _ := os.ReadFile(filepath.Join(root, "server.log"))
			t.Fatalf("postgres setup: %v: %s\n%s", e, b, serverLog)
		}
	}
	run("initdb", "-D", data, "--auth=trust", "--no-locale", "-E", "UTF8")
	run("pg_ctl", "-D", data, "-l", filepath.Join(root, "server.log"), "-o", fmt.Sprintf("-h '' -k '%s' -p 5432", root), "-w", "start")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if b, e := exec.CommandContext(ctx, filepath.Join(bin, "pg_ctl"), "-D", data, "-m", "immediate", "-w", "stop").CombinedOutput(); e != nil {
			t.Errorf("postgres cleanup: %v %s", e, b)
		}
	})
	u, _ := user.Current()
	dsn := fmt.Sprintf("host=%s port=5432 user=%s dbname=postgres sslmode=disable", root, u.Username)
	for _, tc := range []struct {
		name, query string
		limits      Limits
		count       int
		code        string
	}{
		{name: "short_final_page", query: "SELECT '192.0.2.1'::text AS ip", limits: Limits{PageSize: 500, MaxPages: 1}, count: 1},
		{name: "full_final_page_unproven", query: "SELECT '192.0.2.1'::text AS ip", limits: Limits{PageSize: 1, MaxPages: 1}, code: "max_pages"},
		{name: "response_budget", query: "SELECT '192.0.2.1'::text AS ip", limits: Limits{MaxResponseBytes: 2}, code: "max_response_bytes"},
		{name: "inventory_budget", query: "SELECT '192.0.2.1'::text AS ip", limits: Limits{MaxInventoryBytes: 2}, code: "max_inventory_bytes"},
		{name: "optional_null", query: "SELECT '192.0.2.1' AS ip, NULL::text AS name, NULL::text AS serial", count: 1},
		{name: "null_ip", query: "SELECT NULL::text AS ip", code: "invalid_inventory"},
		{name: "blank_ip", query: "SELECT '  ' AS ip", code: "invalid_inventory"},
		{name: "case_sensitive", query: "SELECT '192.0.2.1' AS \"IP\"", code: "invalid_inventory"},
		{name: "unknown_budget", query: "SELECT '192.0.2.1' AS ip, repeat('x', 100) AS ignored", limits: Limits{MaxResponseBytes: 50}, code: "max_response_bytes"},
		{name: "later_invalid_row", query: "SELECT CASE WHEN x=1 THEN '192.0.2.1' END AS ip FROM generate_series(1,2) x", limits: Limits{PageSize: 1}, code: "invalid_inventory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, e := NewPostgres(PostgresOptions{Connection: dsn, Query: tc.query, Limits: tc.limits})
			if e != nil {
				t.Fatal(e)
			}
			rows, e := source.Devices(t.Context())
			if tc.code == "" {
				if e != nil || len(rows) != tc.count {
					t.Fatalf("rows=%v err=%v", rows, e)
				}
			} else if rows != nil || errorCode(e) != tc.code {
				t.Fatalf("rows=%v err=%v want=%s", rows, e, tc.code)
			}
		})
	}
	source, e := NewPostgres(PostgresOptions{Connection: dsn, Query: "SELECT '192.0.2.' || x AS ip, $1::text AS name FROM generate_series(1,3) x", Parameters: []any{"fixture"}, Limits: Limits{PageSize: 2}})
	if e != nil {
		t.Fatal(e)
	}
	rows, e := source.Devices(t.Context())
	if e != nil || len(rows) != 3 || rows[2].Name != "fixture" {
		t.Fatalf("%v %v", rows, e)
	}
	for _, query := range []string{"SELECT '192.0.2.1' AS ip; SELECT 2", "DELETE FROM pg_class RETURNING '192.0.2.1' AS ip", "SELECT 'x' AS ip, 'y' AS ip"} {
		s, e := NewPostgres(PostgresOptions{Connection: dsn, Query: query})
		if e != nil {
			continue
		}
		if rows, e = s.Devices(t.Context()); e == nil || rows != nil {
			t.Fatalf("query was not rejected: %s", query)
		}
	}
}
