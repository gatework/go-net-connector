package cli

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/netdisco"
	"github.com/gatework/go-net-connector/storage"
)

func openDevice(ctx context.Context, s *bootstrap.Services, r netdisco.Record, logs string) (*connector.Device, func() error, error) {
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
		log.Close()
		return nil, nil, e
	}
	return d, func() error { return errors.Join(d.Close(), log.Sync(), log.Close()) }, nil
}
