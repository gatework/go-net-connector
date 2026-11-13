package config

import "testing"

func TestBackupMethodsShareTransferConventions(t *testing.T) {
	for _, mode := range []string{"backup", "tftp"} {
		d := Defaults(mode)
		v := d["tftp"].(map[string]any)
		if v["upload_timeout"] != 180 || v["ssh_worker_path"] != "/usr/local/bin/tftp-archive" || v["source_interfaces"] != nil || v["source_files"] != nil || v["vrfs"] != nil {
			t.Fatal(mode, v)
		}
		if v["server"] != nil {
			t.Fatal("site-specific server must remain explicit")
		}
		wantPolicy := "selected"
		if mode == "tftp" {
			wantPolicy = "verified"
		}
		if d["backup"].(map[string]any)["success_policy"] != wantPolicy {
			t.Fatal("incorrect method success policy", mode)
		}
	}
}
