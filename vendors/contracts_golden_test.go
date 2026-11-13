package vendors

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestGoldenPlatformCommands(t *testing.T) {
	var fixture struct {
		Profiles map[string]struct {
			Config, Save []string
			Privilege    *string
			Timeout      float64
		}
	}
	b, e := os.ReadFile("../testdata/device-contracts.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &fixture); e != nil {
		t.Fatal(e)
	}
	registry := NewBuiltinRegistry()
	if len(fixture.Profiles) != len(registry.Names()) {
		t.Fatal("vendor count changed")
	}
	for name, want := range fixture.Profiles {
		t.Run(name, func(t *testing.T) {
			p := lookupPlatform(t, name)
			config := []string{}
			config = append(config, p.Terminal().ConfigCommands...)
			save := []string{}
			save = append(save, p.Terminal().SaveCommands...)
			if !reflect.DeepEqual(config, want.Config) || !reflect.DeepEqual(save, want.Save) {
				t.Fatal("commands differ", config, save)
			}
			privilege := ""
			if want.Privilege != nil {
				privilege = *want.Privilege
			}
			if p.Terminal().PrivilegeCommand != privilege || p.Terminal().CommandTimeout != time.Duration(want.Timeout*float64(time.Second)) {
				t.Fatal("profile policy differs")
			}
		})
	}
}
