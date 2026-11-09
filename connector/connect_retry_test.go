package connector

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedTransport writes one banner to every new connection.
func scriptedTransport(banner string, dials *atomic.Int32) TransportFactory {
	return func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
		dials.Add(1)
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			_, _ = io.WriteString(b, banner)
			_, _ = io.Copy(io.Discard, b)
		}()
		return a, nil
	}
}

func withFastRetry(t *testing.T) {
	t.Helper()
	previous := connectRetryDelay
	connectRetryDelay = time.Millisecond
	t.Cleanup(func() { connectRetryDelay = previous })
}

func TestConnectRetriesOnlyUnreachableBeforeLogin(t *testing.T) {
	withFastRetry(t)
	var dials atomic.Int32
	d, err := New(fixtureDriver(), ConnectionConfig{Host: "fixture", ConnectRetries: 2}, WithTransportFactory(scriptedTransport("ssh: connect to host fixture port 22: Connection refused\r\n", &dials)))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	err = d.Connect(t.Context())
	classified, ok := errors.AsType[*Error](err)
	if !ok || classified.Code != "connection_refused" || dials.Load() != 3 {
		t.Fatalf("dials=%d err=%v", dials.Load(), err)
	}
}

func TestConnectNeverRetriesAuthentication(t *testing.T) {
	withFastRetry(t)
	var dials atomic.Int32
	d, err := New(fixtureDriver(), ConnectionConfig{Host: "fixture", ConnectRetries: 5}, WithTransportFactory(scriptedTransport("Permission denied (publickey,password).\r\n", &dials)))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = d.Connect(t.Context()); err == nil || dials.Load() != 1 {
		t.Fatalf("authentication retried: dials=%d err=%v", dials.Load(), err)
	}
	if classified, ok := errors.AsType[*Error](err); !ok || classified.Code != "authentication_error" {
		t.Fatalf("classification: %v", err)
	}
}

func TestConnectRetryStopsOnCancellation(t *testing.T) {
	previous := connectRetryDelay
	connectRetryDelay = time.Hour
	t.Cleanup(func() { connectRetryDelay = previous })
	var dials atomic.Int32
	d, err := New(fixtureDriver(), ConnectionConfig{Host: "fixture", ConnectRetries: 3}, WithTransportFactory(scriptedTransport("No route to host\r\n", &dials)))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err = d.Connect(ctx); !errors.Is(err, context.DeadlineExceeded) || dials.Load() != 1 {
		t.Fatalf("dials=%d err=%v", dials.Load(), err)
	}
}

func TestJumpHostArgumentsAndValidation(t *testing.T) {
	args := sshArguments(ConnectionConfig{Host: "192.0.2.1", Username: "ops", JumpHost: "admin@bastion.example:2222", LoginTimeout: time.Second}, "/tmp/known_hosts", 22)
	if i := slices.Index(args, "-J"); i < 0 || args[i+1] != "admin@bastion.example:2222" || args[len(args)-2] != "--" || args[len(args)-1] != "192.0.2.1" {
		t.Fatalf("args = %q", args)
	}
	if args := sshArguments(ConnectionConfig{Host: "192.0.2.1"}, "/tmp/known_hosts", 22); slices.Contains(args, "-J") {
		t.Fatalf("jump host without configuration: %q", args)
	}
	for name, cfg := range map[string]ConnectionConfig{
		"option injection": {Host: "h", JumpHost: "-oProxyCommand=evil"},
		"spaces":           {Host: "h", JumpHost: "a b"},
		"telnet":           {Host: "h", JumpHost: "bastion", Protocol: ProtocolTelnet},
		"telnet fallback":  {Host: "h", JumpHost: "bastion", AllowTelnetFallback: true},
		"too many retries": {Host: "h", ConnectRetries: 6},
		"negative retries": {Host: "h", ConnectRetries: -1},
	} {
		if err := cfg.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	for _, jump := range []string{"bastion", "ops@10.0.0.1", "ops@[2001:db8::1]:22", "jump-1.example.com:2200"} {
		if err := (ConnectionConfig{Host: "h", JumpHost: jump}).Validate(); err != nil {
			t.Errorf("%s rejected: %v", jump, err)
		}
	}
}
