package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSaveRequiresProfileEvidenceAndPreservesCompletedStep(t *testing.T) {
	for _, output := range []string{"saved\nsw#", "sw#"} {
		t.Run(output, func(t *testing.T) {
			p := fixtureProfile()
			p.SaveCommands = []string{"save"}
			p.ValidateSave = func(ctx context.Context, r Result) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if !strings.Contains(testResultOutput(r), "saved") {
					return ErrUnconfirmed
				}
				return nil
			}
			d, _ := fixtureDevice(t, p, []string{output})
			r, err := d.SaveConfig(t.Context(), ExecuteOptions{})
			if len(r.Steps()) != 1 {
				t.Fatal("completed save command lost")
			}
			if output == "sw#" && !errors.Is(err, ErrUnconfirmed) {
				t.Fatalf("prompt treated as save evidence: %v", err)
			}
			if output != "sw#" && err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestSaveWithoutEvidencePolicyRemainsUnconfirmed(t *testing.T) {
	p := fixtureProfile()
	p.SaveCommands = []string{"save"}
	d, _ := fixtureDevice(t, p, []string{"done\nsw#"})
	r, err := d.SaveConfig(t.Context(), ExecuteOptions{})
	if len(r.Steps()) != 1 || !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("steps=%d err=%v", len(r.Steps()), err)
	}
}
