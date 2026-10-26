package terminal

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestFrozenRubyRenderedBytesAndHash(t *testing.T) {
	var fixture struct {
		Renderings []struct{ Input, Output, SHA256 string }
	}
	b, e := os.ReadFile("../../testdata/ruby-contracts.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &fixture); e != nil {
		t.Fatal(e)
	}
	for _, c := range fixture.Renderings {
		got, e := Render([]byte(c.Input), true)
		if e != nil || got != c.Output || fmt.Sprintf("%x", sha256.Sum256([]byte(got))) != c.SHA256 {
			t.Fatalf("%q got %q want %q error %v", c.Input, got, c.Output, e)
		}
	}
}
