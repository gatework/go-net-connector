package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/inventory"
	"github.com/gatework/go-net-connector/storage"
	"github.com/gatework/go-net-connector/transfer"
	"github.com/gatework/go-net-connector/vendors"
)

func TestArchiveUploadReportAndExport(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	pool, err := archive.NewPool(archive.PoolOptions{Root: root, Directory: directory, Parallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	rules, err := NewRules(RuleOptions{}, vendors.NewBuiltinRegistry())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}}, rules, PlanOptions{Mode: "tftp", Target: func(Record) (transfer.Target, error) {
		return transfer.Target{TFTPOptions: transfer.TFTPOptions{Server: "192.0.2.50", Path: "running.cfg"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := Run(t.Context(), plan, RunOptions{Workers: 1, Run: func(ctx context.Context, _ Task) (Artifact, error) {
		result, err := pool.Upload(ctx, "running.cfg", "device.cfg", func(_ context.Context, name string) (transfer.Receipt, error) {
			err := os.WriteFile(filepath.Join(root, name), []byte("中文配置\n"), 0600)
			return transfer.Receipt{Server: "192.0.2.50", Path: name, Verification: transfer.VerificationDeviceReported}, err
		})
		return Artifact{Transfer: &result.Transfer, Archive: &result.Receipt}, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewReport(batch, "verified")
	if err != nil || !report.Success() {
		t.Fatal(err, report.Statistics())
	}
	report = WriteReport(t.Context(), report, directory, plan, 1)
	if !report.Success() {
		t.Fatal(report.Statistics())
	}
	data, err := os.ReadFile(filepath.Join(directory, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	devices := document["devices"].([]any)
	row := devices[0].(map[string]any)
	if row["verification"] != "server_verified" || row["archive"].(map[string]any)["cleanup"] != "complete" {
		t.Fatal(row)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err = os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	body, artifact, err := storage.ReadBatchSaved(t.Context(), moved, "192.0.2.1")
	if err != nil || string(body) != "中文配置\n" || artifact.Path != filepath.Join(moved, "tftp", "device.cfg") {
		t.Fatal(string(body), artifact, err)
	}
	_, receipt, err := storage.ExportBatchSaved(t.Context(), moved, "192.0.2.1", filepath.Join(t.TempDir(), "export.cfg"))
	if err != nil || !receipt.Durable() {
		t.Fatal(receipt, err)
	}
}

func TestArchiveDiagnosticPreservesIndependentFactsAndSafeErrors(t *testing.T) {
	a := Artifact{Transfer: &transfer.Receipt{Verification: transfer.VerificationDeviceReported}, Archive: &archive.Receipt{
		Server: &archive.Proof{Verified: true, SHA256: strings.Repeat("a", 64), Bytes: 10, ArchivePath: "/safe/archive"},
		Local:  storage.Receipt{Path: "/private/device", State: storage.StateCommitted, Phase: "directory_sync"}, Cleanup: archive.CleanupNotRequested}}
	failure := &storage.WriteError{Receipt: a.Archive.Local, Cause: errors.New("PRIVATE_CAUSE")}
	d := diagnoseError(failure, a)
	if d.ArtifactState != "committed" || d.ArtifactPhase != "directory_sync" || d.Verification != "server_verified" {
		t.Fatal(d)
	}
	clone := a.clone()
	clone.Archive.Server.SHA256 = "mutated"
	if a.Archive.Server.SHA256 == "mutated" {
		t.Fatal("proof not cloned")
	}
	// Cleanup uncertainty must fail every policy even though the independent file
	// verification remains true and the local artifact remains exportable.
	a.Archive.Local.State = storage.StateDurable
	a.Archive.Cleanup = archive.CleanupUnconfirmed
	b := Batch{mode: modeTFTP, outcomes: []Outcome{{Device: Record{Host: "192.0.2.1"}, Status: string(outcomeStatusReportedWithError), Artifact: a, Diagnostic: d}}}
	for _, policy := range []string{"strict", "selected", "verified"} {
		report, err := NewReport(b, policy)
		if err != nil || report.Success() {
			t.Fatal(policy, err)
		}
		encoded, err := json.Marshal(report)
		if err != nil || strings.Contains(string(encoded), "PRIVATE_CAUSE") {
			t.Fatal(err, string(encoded))
		}
	}
}

func TestRealArchiveErrorKeepsPrimaryFailureClassification(t *testing.T) {
	for _, uploadFailed := range []bool{false, true} {
		t.Run(map[bool]string{false: "local_write", true: "upload_then_local_write"}[uploadFailed], func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			pool, err := archive.NewPool(archive.PoolOptions{Root: root, Directory: directory, Parallel: 1, Timeouts: archive.Timeouts{Upload: 20 * time.Millisecond}})
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if err := pool.Prepare(t.Context()); err != nil {
				t.Fatal(err)
			}
			result, err := pool.Upload(t.Context(), "running.cfg", "device.cfg", func(ctx context.Context, name string) (transfer.Receipt, error) {
				if err := os.WriteFile(filepath.Join(root, name), []byte("configuration\n"), 0600); err != nil {
					return transfer.Receipt{}, err
				}
				if err := os.RemoveAll(filepath.Join(directory, "tftp")); err != nil {
					return transfer.Receipt{}, err
				}
				receipt := transfer.Receipt{Path: name, Verification: transfer.VerificationDeviceReported}
				if uploadFailed {
					<-ctx.Done()
					return receipt, ctx.Err()
				}
				return receipt, nil
			})
			if err == nil || !result.Receipt.Verified() {
				t.Fatal(result, err)
			}
			diagnostic := diagnoseError(err, Artifact{Transfer: &result.Transfer, Archive: &result.Receipt})
			if uploadFailed {
				if diagnostic.Phase != "upload" || diagnostic.ErrorCode != "tftp_archive_failed" || diagnostic.UnderlyingType != "Timeout::Error" {
					t.Fatal(diagnostic)
				}
			} else if diagnostic.ErrorCode != "file_write_failed" || diagnostic.Phase != "verify" || diagnostic.ArtifactPhase != "resolve_parent" {
				t.Fatal(diagnostic)
			}
		})
	}
}

func TestLocalCancellationIsNotBatchInterruption(t *testing.T) {
	rules, err := NewRules(RuleOptions{}, vendors.NewBuiltinRegistry())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan([]inventory.Device{{IP: "192.0.2.1", Vendor: "h3c"}}, rules, PlanOptions{Mode: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := Run(t.Context(), plan, RunOptions{Run: func(context.Context, Task) (Artifact, error) {
		return Artifact{}, errors.Join(context.DeadlineExceeded, context.Canceled)
	}})
	if err != nil || batch.Status() != "incomplete" || batch.Outcomes()[0].Status != "failed" {
		t.Fatalf("batch=%s outcomes=%+v err=%v", batch.Status(), batch.Outcomes(), err)
	}
}

func TestProtocolTimeoutRetainsDiagnosticCause(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	protocol := archive.NewProtocol(client, client)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := protocol.Expect(ctx, "complete")
	diagnostic := diagnoseError(err, Artifact{})
	if !errors.Is(err, context.DeadlineExceeded) || diagnostic.ErrorCode != "tftp_archive_failed" || diagnostic.UnderlyingType != "Timeout::Error" {
		t.Fatal(diagnostic, err)
	}
}
