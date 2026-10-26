package connector

import (
	"context"
	"io"
	"net"
	"reflect"
	"testing"
)

func TestKeyPassphraseAndExplicitLineEnding(t *testing.T) {
	done := make(chan error, 1)
	d, err := New(fixtureProfile(), ConnectionConfig{Host: "fixture", IdentityFile: "/keys/device key", KeyPassphrase: "passphrase-value", LineEnding: "\r\n"}, WithTransportFactory(func(_ context.Context, cfg ConnectionConfig) (io.ReadWriteCloser, error) {
		if cfg.IdentityFile != "/keys/device key" {
			t.Error("identity setting lost")
		}
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			if _, err := io.WriteString(b, "Enter passphrase for key '/keys/device key': "); err != nil {
				done <- err
				return
			}
			want := "passphrase-value\r\n"
			got := make([]byte, len(want))
			_, err := io.ReadFull(b, got)
			if err == nil && string(got) != want {
				t.Error("wrong passphrase bytes")
			}
			if err == nil {
				_, err = io.WriteString(b, "sw#")
			}
			if err == nil {
				want = "show\r\n"
				got = make([]byte, len(want))
				_, err = io.ReadFull(b, got)
				if err == nil && string(got) != want {
					t.Error("wrong command line ending")
				}
			}
			if err == nil {
				_, err = io.WriteString(b, "done\nsw#")
			}
			done <- err
		}()
		return a, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	r, err := d.ExecuteCommand(t.Context(), Command{Text: "show"}, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Attempts()[0].Written() != len("show\r\n") {
		t.Fatal("wrong write progress")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestSSHIdentityArgumentsDoNotContainPassphrase(t *testing.T) {
	args := sshArguments(ConnectionConfig{Host: "fixture", IdentityFile: "/keys/device key", KeyPassphrase: "private", HostKeyPolicy: "strict"}, "/trust/snapshot", 22)
	found := false
	for i, arg := range args {
		if arg == "private" {
			t.Fatal("passphrase in argv")
		}
		if arg == "-i" && i+1 < len(args) && args[i+1] == "/keys/device key" {
			found = true
		}
	}
	if !found {
		t.Fatal("identity not passed as literal argument")
	}
	if !reflect.DeepEqual(args[len(args)-2:], []string{"--", "fixture"}) {
		t.Fatal("host delimiter lost")
	}
}
