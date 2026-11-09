package connector

import (
	"testing"
)

func TestNativeTransferMaySkipPrivilege(t *testing.T) {
	p := fixtureDriver()
	p.Grammar.PrivilegeCommand = "enable"
	p.Grammar.PrivilegePrompt = testPattern(`sw#`)
	d, sent := fixtureDevice(t, p, []string{"copy\r\ncomplete\r\nsw#"})
	result, e := d.ExecuteCommand(t.Context(), Command{Text: "copy"}, ExecuteOptions{SkipPrivilege: true})
	d.Close()
	if e != nil || len(result.Steps()) != 1 || len(*sent) != 1 || (*sent)[0] != "copy" {
		t.Fatal(result, e, *sent)
	}
}
