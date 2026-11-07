package bootstrap

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gatework/go-net-connector/netdisco"
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

func TestHTTPIdentityExcludesSecretsAndIncludesResultQuery(t *testing.T) {
	identity := func(options netdisco.HTTPOptions) string {
		b, err := httpSourceIdentity(options, "")
		if err != nil {
			t.Fatal(err)
		}
		return netdisco.Identity(b)
	}

	first := identity(netdisco.HTTPOptions{URL: "https://inventory.example/base", APIKey: "first-secret", Limits: netdisco.Limits{PageSize: 500}})
	if first != identity(netdisco.HTTPOptions{URL: "https://inventory.example/base", APIKey: "rotated-secret", Limits: netdisco.Limits{PageSize: 500}}) {
		t.Fatal("same HTTP source settings must have a stable identity")
	}
	if first == identity(netdisco.HTTPOptions{URL: "https://inventory.example/base", APIKey: "first-secret", Limits: netdisco.Limits{PageSize: 100}}) {
		t.Fatal("pagination query settings must affect the cache identity")
	}
	if identity(netdisco.HTTPOptions{URL: "https://inventory.example/base", Username: "reader-a", Password: "first-secret", Limits: netdisco.Limits{PageSize: 500}}) == identity(netdisco.HTTPOptions{URL: "https://inventory.example/base", Username: "reader-b", Password: "rotated-secret", Limits: netdisco.Limits{PageSize: 500}}) {
		t.Fatal("HTTP basic-auth principals must not share cache identity")
	}
	if identity(netdisco.HTTPOptions{URL: "https://inventory.example/base", Username: "reader", Password: "first-secret", Limits: netdisco.Limits{PageSize: 500}}) != identity(netdisco.HTTPOptions{URL: "https://inventory.example/base", Username: "reader", Password: "rotated-secret", Limits: netdisco.Limits{PageSize: 500}}) {
		t.Fatal("rotating a basic-auth password must not change the cache identity")
	}
	options := netdisco.HTTPOptions{URL: "https://inventory.example/base", APIKey: "do-not-store-this", Limits: netdisco.Limits{PageSize: 500}}
	encoded, err := httpSourceIdentity(options, "inventory-account")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), options.APIKey) || strings.Contains(string(encoded), "password") {
		t.Fatalf("identity contains authentication material: %s", encoded)
	}
	firstAccount, err := httpSourceIdentity(options, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	rotatedKey, err := httpSourceIdentity(netdisco.HTTPOptions{URL: options.URL, APIKey: "rotated-secret", Limits: options.Limits}, "account-a")
	if err != nil {
		t.Fatal(err)
	}
	otherAccount, err := httpSourceIdentity(options, "account-b")
	if err != nil {
		t.Fatal(err)
	}
	if netdisco.Identity(firstAccount) != netdisco.Identity(rotatedKey) || netdisco.Identity(firstAccount) == netdisco.Identity(otherAccount) {
		t.Fatal("non-secret namespace must isolate API-key principals and survive key rotation")
	}
}
