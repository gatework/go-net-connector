package transfer

import "testing"

func FuzzTargetValidation(f *testing.F) {
	f.Add("server", "site/config.cfg", "flash:/startup.cfg")
	f.Add("x;echo", "../bad", "\x00")
	f.Fuzz(func(t *testing.T, server, path, source string) {
		if len(server)+len(path)+len(source) > 1<<16 {
			t.Skip()
		}
		_ = Validate("h3c", "192.0.2.1", TFTPOptions{Server: server, Path: path, SourceFile: source})
		_ = ValidateRemote(RemoteOptions{Server: server, Path: path, Username: "backup"})
	})
}
