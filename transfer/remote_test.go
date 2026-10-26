package transfer

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRemoteExactCompletionAndFailurePriority(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{"Wrote 4 lines of output to 'scp://backup@192.0.2.9/site/config.conf'", true},
		{"Wrote 4 lines of output to 'scp://backup@192.0.2.9/other.conf'", false},
		{"Wrote 0 lines of output to 'scp://backup@192.0.2.9/site/config.conf'", false},
		{"Wrote 4 lines of output to 'scp://backup@192.0.2.9/site/config.conf'\r\nerror: write failed", false},
	} {
		command := "show configuration | save scp://backup@192.0.2.9/site/config.conf"
		d := transferDevice(t, "juniper", [][2]string{{command, "Password: "}, {"secret", tc.body + "\r\nsw#"}})
		r, e := RemoteBackup(t.Context(), d, RemoteOptions{Server: "192.0.2.9", Path: "site/config.conf", Username: "backup", Password: "secret"})
		if (e == nil) != tc.ok || r.Receipt.Reported() != tc.ok {
			t.Fatal(r, e)
		}
		if strings.Contains(fmt.Sprintf("%#v %v", r, e), "secret") {
			t.Fatal("secret disclosed")
		}
	}
}
func TestRemoteRejectsBeforeDeviceIO(t *testing.T) {
	for _, o := range []RemoteOptions{{Server: "bad;host", Path: "config", Username: "u"}, {Server: "host", Path: "../config", Username: "u"}, {Server: "host", Path: "config", Username: "u", Password: "x\ny"}, {Server: "host", Path: "config", Username: "u", Protocol: "sftp"}} {
		if e := ValidateRemote(o); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
}
