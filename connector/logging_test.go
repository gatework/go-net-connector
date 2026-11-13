package connector

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

type eventFailWriter struct {
	bytes.Buffer
	event   string
	failure error
}

func (w *eventFailWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("event="+w.event)) {
		if w.failure == nil {
			w.failure = errors.New("PRIVATE_SINK_CAUSE")
		}
		return 0, w.failure
	}
	return w.Buffer.Write(p)
}
func textLogDevice(t *testing.T, level LogLevel, sink io.Writer, response string) *Device {
	t.Helper()
	d, err := New(fixtureDriver(), ConnectionConfig{Host: "fixture", LogLevel: level, Logger: sink}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			_, _ = io.WriteString(b, "sw#")
			if _, e := bufio.NewReader(b).ReadString('\n'); e == nil {
				_, _ = io.WriteString(b, response)
			}
			_, _ = io.Copy(io.Discard, b)
		}()
		return a, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}
func TestTextLogCompletionFailureRetainsReceipt(t *testing.T) {
	sink := &eventFailWriter{event: "command_complete"}
	d := textLogDevice(t, "info", sink, "ok\r\nsw#")
	receipts := 0
	r, err := d.ExecuteCommand(t.Context(), Command{Text: "change"}, ExecuteOptions{OnReceipt: func(context.Context, StepResult) error { receipts++; return nil }})
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != "log_error" || ce.Phase != "logging" || len(r.Steps()) != 1 || receipts != 1 {
		t.Fatalf("err=%v steps=%d receipts=%d", err, len(r.Steps()), receipts)
	}
}
func TestTextLogFailurePreservesPrimaryDeviceError(t *testing.T) {
	sink := &eventFailWriter{event: "command_complete"}
	d := textLogDevice(t, "debug", sink, "% Invalid PRIVATE_RESPONSE\r\nsw#")
	r, err := d.ExecuteCommand(t.Context(), Command{Text: "change"}, ExecuteOptions{})
	if !errors.Is(err, ErrDevice) || !errors.Is(err, sink.failure) || len(r.Steps()) != 0 {
		t.Fatalf("primary=%v steps=%d", err, len(r.Steps()))
	}
}
func TestTextLogThresholdsRetainFailures(t *testing.T) {
	for _, level := range []LogLevel{LogWarn, LogError} {
		t.Run(string(level), func(t *testing.T) {
			var sink bytes.Buffer
			d := textLogDevice(t, level, &sink, "% Invalid PRIVATE_RESPONSE\r\nsw#")
			_, err := d.ExecuteCommand(t.Context(), Command{Text: "change"}, ExecuteOptions{})
			if !errors.Is(err, ErrDevice) {
				t.Fatal(err)
			}
			log := sink.String()
			if !strings.Contains(log, "ERROR") || !strings.Contains(log, "event=command_complete") || strings.Contains(log, "PRIVATE_RESPONSE") || strings.Contains(log, " INFO ") {
				t.Fatal(log)
			}
		})
	}
}
func TestTextLogSensitiveCommand(t *testing.T) {
	var sink bytes.Buffer
	d := textLogDevice(t, "debug", &sink, "PRIVATE_RESPONSE\r\nsw#")
	_, err := d.ExecuteCommand(t.Context(), Command{Text: "PRIVATE_COMMAND", Sensitive: true}, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sink.String(), "PRIVATE_") {
		t.Fatal(sink.String())
	}
}

func TestTextLogConnectMessageRedactsAndCleansHost(t *testing.T) {
	for _, host := range []string{"prefixPRIVATE_HOSTsuffix", "\x1b[31mfixture"} {
		t.Run(host, func(t *testing.T) {
			var sink bytes.Buffer
			d, err := New(fixtureDriver(), ConnectionConfig{Host: host, Password: "PRIVATE_HOST", Logger: &sink, LogLevel: "info"}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) { return nil, ErrConnection }))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if err = d.Connect(t.Context()); !errors.Is(err, ErrConnection) {
				t.Fatal(err)
			}
			if strings.Contains(sink.String(), "PRIVATE_HOST") || strings.ContainsRune(sink.String(), '\x1b') {
				t.Errorf("unsafe connection event: %q", sink.String())
			}
			if !strings.Contains(sink.String(), "event=connect") {
				t.Error("connection event absent")
			}
		})
	}
}
