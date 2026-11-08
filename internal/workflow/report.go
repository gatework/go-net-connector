package workflow

import (
	"github.com/gatework/go-net-connector/fleet"
)

func artifactView(a fleet.Artifact) any {
	if b := a.Backup; b != nil {
		metadata := b.Clone()
		return map[string]any{"path": b.Path, "bytes": b.Bytes, "sha256": b.SHA256, "collected_at": b.CollectedAt, "change": b.Change, "previous_sha256": nullableString(b.PreviousSHA256), "source": metadata.Source, "encoding": nullableString(metadata.Encoding), "text_warning": metadata.TextWarning}
	}
	if t := a.Transfer; t != nil {
		values := map[string]any{"server": t.Server, "path": nullableString(t.Path), "requested_path": t.RequestedPath, "completed_at": t.CompletedAt, "configuration_kind": t.ConfigurationKind, "source_file": nullableString(t.SourceFile), "format": t.Format, "verification": t.Verification, "server_sha256": nil, "server_bytes": int64(0), "local_file": nil, "server_archive_file": nil}
		if r := a.Archive; r != nil {
			values["archive"] = r.Clone()
			if r.Server != nil {
				values["server_archive_file"] = nullableString(r.Server.ArchivePath)
			}
			if r.Verified() {
				values["verification"] = "server_verified"
				values["server_sha256"] = r.Server.SHA256
				values["server_bytes"] = r.Server.Bytes
			}
			if r.Local.Committed() {
				values["local_file"] = nullableString(r.Local.Path)
			}
		}
		return values
	}
	if a.Archive != nil {
		return map[string]any{"archive": a.Archive.Clone()}
	}
	return nil
}
