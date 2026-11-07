package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFailedAttemptRetainsPartialResponse(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"private-prefix"})
	timeout := 20 * time.Millisecond
	r, err := d.ExecuteCommand(t.Context(), Command{Text: "show", Timeout: &timeout}, ExecuteOptions{})
	if !errors.Is(err, context.DeadlineExceeded) || len(r.Steps()) != 0 {
		t.Fatalf("steps=%d err=%v", len(r.Steps()), err)
	}
	attempts := r.Attempts()
	if len(attempts) != 1 || attempts[0].Complete() || attempts[0].Written() != len("show\n") || attempts[0].Response().Raw() != "private-prefix" || !errors.Is(attempts[0].Err(), context.DeadlineExceeded) {
		t.Fatalf("attempt evidence: %v", attempts)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", r, attempts), "private-prefix") {
		t.Fatal("implicit diagnostics expose response")
	}
}
func TestRejectedAttemptIsNotSuccessfulStep(t *testing.T) {
	d, _ := fixtureDevice(t, fixtureProfile(), []string{"% Invalid command\nsw#"})
	r, err := d.ExecuteCommand(t.Context(), Command{Text: "change"}, ExecuteOptions{})
	if !errors.Is(err, ErrDevice) || len(r.Steps()) != 0 || len(r.Attempts()) != 1 || !r.Attempts()[0].Complete() {
		t.Fatalf("steps=%d attempts=%v err=%v", len(r.Steps()), r.Attempts(), err)
	}
	if !strings.Contains(r.Attempts()[0].Response().Raw(), "Invalid") {
		t.Fatal("rejection evidence lost")
	}
}
