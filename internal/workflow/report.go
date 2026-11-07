package workflow

import (
	"fmt"

	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/display"
)

func artifactView(a fleet.Artifact) any {
	if b := a.Backup; b != nil {
		metadata := b.Clone()
		return map[string]any{"path": b.Path, "bytes": b.Bytes, "sha256": b.SHA256, "collected_at": b.CollectedAt, "change": b.Change, "previous_sha256": nullableString(b.PreviousSHA256), "source": metadata.Source, "encoding": nullableString(metadata.Encoding), "text_warning": metadata.TextWarning}
	}
	if t := a.Transfer; t != nil {
		return map[string]any{"server": t.Server, "path": nullableString(t.Path), "requested_path": t.RequestedPath, "completed_at": t.CompletedAt, "configuration_kind": t.ConfigurationKind, "source_file": nullableString(t.SourceFile), "format": t.Format, "verification": t.Verification, "server_sha256": nullableString(t.ServerSHA256), "server_bytes": t.ServerBytes, "local_file": nullableString(t.LocalPath), "server_archive_file": nullableString(t.ArchivePath)}
	}
	return nil
}

func BackupProgress(out fleet.Outcome) string {
	text := display.Status(out.Status)
	if b := out.Artifact.Backup; b != nil && b.TextWarning != nil && b.TextWarning.Count > 0 {
		committed, verified := false, false
		if b.TextOnly {
			committed, verified = b.HasSavedEvidence(), b.Complete()
		} else if b.Source != nil {
			committed = b.Source.File.Committed()
			verified = b.Source.File.Durable() || b.Source.NoWriteVerified
		}
		text += fmt.Sprintf("（文本编码告警：%d 个字节；%s）", b.TextWarning.Count, display.SourceState(committed, verified))
	}
	if out.ErrorCode != "" {
		text += "；" + display.Error(out.ErrorCode)
	}
	return text
}
