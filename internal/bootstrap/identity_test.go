package bootstrap

import (
	"encoding/json"
	"testing"
)

func TestPostgresIdentityPreservesRubyOrderAndTypes(t *testing.T) {
	raw, e := orderedConnectionIdentity(map[string]any{"host": "db", "dbname": "inventory", "user": "reader", "port": 5432, "connect_timeout": 10, "sslmode": "verify-full"})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal([]any{"postgres", raw, map[string]any{"query": "select 1", "query_params": []any{}}})
	want := `["postgres",{"port":5432,"connect_timeout":10,"sslmode":"verify-full","host":"db","dbname":"inventory","user":"reader"},{"query":"select 1","query_params":[]}]`
	if string(b) != want {
		t.Fatalf("%s", b)
	}
}
