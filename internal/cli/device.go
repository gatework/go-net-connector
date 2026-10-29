package cli

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/storage"
)

func openDevice(ctx context.Context, s *bootstrap.Services, r fleet.Record, logs string) (*connector.Device, func() error, error) {
	if logs == "" {
		d, e := s.Device(ctx, r)
		if e != nil {
			return nil, nil, e
		}
		return d, d.Close, nil
	}
	if e := storage.PrepareDirectory(logs); e != nil {
		return nil, nil, e
	}
	log, e := storage.OpenAppend(filepath.Join(logs, strings.ReplaceAll(r.Host, ":", "_")+".log"))
	if e != nil {
		return nil, nil, e
	}
	d, e := s.DeviceWithLogger(ctx, r, log)
	if e != nil {
		return nil, nil, joinDeviceCleanup(e, log.Close())
	}
	return d, func() error { return errors.Join(d.Close(), log.Sync(), log.Close()) }, nil
}

type deviceCleanupError struct{ cause error }

func (*deviceCleanupError) Error() string      { return "net-connector: operation or cleanup failed" }
func (e *deviceCleanupError) GoString() string { return e.Error() }
func (e *deviceCleanupError) Unwrap() error    { return e.cause }
func joinDeviceCleanup(primary, cleanup error) error {
	if cleanup == nil {
		return primary
	}
	return &deviceCleanupError{cause: errors.Join(primary, cleanup)}
}
