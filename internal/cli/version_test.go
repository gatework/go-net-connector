package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionRemainsPureAndReportsBuildIdentity(t *testing.T) {
	var output bytes.Buffer
	_, err := parse([]string{"--config", "/does/not/exist", "--version"}, "net-backup", &output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "go-net-connector development revision unknown") {
		t.Fatalf("missing release identity: %q", output.String())
	}
}
