package fleet

import (
	"errors"
	"testing"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/transfer"
)

func TestTransferDiagnosticIntegration(t *testing.T) {
	for _, tc := range []struct {
		err               error
		code, kind, phase string
	}{{&transfer.Error{Code: "transfer_unconfirmed", Phase: "tftp_backup"}, "transfer_unconfirmed", "Net::Connector::DeviceError", "tftp_backup"}, {archive.ErrArchive, "tftp_archive_failed", "Net::Connector::Netdisco::Tftp::Archive::Failure", "tftp_backup"}, {errors.Join(archive.ErrArchive, errors.New("private details")), "tftp_archive_failed", "Net::Connector::Netdisco::Tftp::Archive::Failure", "tftp_backup"}} {
		d := diagnoseError(tc.err, Artifact{})
		if d.ErrorCode != tc.code || d.ErrorType != tc.kind || d.Phase != tc.phase {
			t.Fatal(d)
		}
	}
	d := diagnoseError(&transfer.Error{Code: "transfer_finalize_failed", Phase: "remote_backup"}, Artifact{Transfer: &transfer.Receipt{Verification: "device_reported"}})
	if d.ErrorType != "Net::Connector::RemoteBackup::CompletionError" || d.ArtifactState != "reported_uploaded" {
		t.Fatal(d)
	}
}
