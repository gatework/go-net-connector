package connector

import (
	"testing"
)

func TestProfileCommandAccessorsReturnCopies(t *testing.T) {
	p := Terminal{Name: "test", LoginPrompt: testPattern(`#`), CommandPrompt: testPattern(`#`), ConfigCommands: []string{"show"}, SaveCommands: []string{"save"}}
	d, e := New(StaticDriver{Grammar: p}, ConnectionConfig{Host: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	d.ConfigCommands()[0] = "changed"
	d.SaveCommands()[0] = "changed"
	if d.ConfigCommands()[0] != "show" || d.SaveCommands()[0] != "save" {
		t.Fatal("accessor modified device profile")
	}
}
