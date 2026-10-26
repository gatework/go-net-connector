package cli

import (
	"context"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/netdisco"
	"testing"
)

func TestReplacePreflightBeforeInventoryAndCredentials(t *testing.T) {
	r, _, errout := fixtureRuntime(t)
	r.Args = []string{"--all", "--non-interactive"}
	r.Environment["NC_HOST_KEY_POLICY"] = "replace"
	calls := 0
	r.Source = netdisco.SourceFunc(func(context.Context) ([]inventory.Device, error) { calls++; return nil, nil })
	if code := r.Run(t.Context(), "net-backup"); code != 2 || calls != 0 {
		t.Fatalf("code=%d inventory=%d error=%s", code, calls, errout.String())
	}
}
