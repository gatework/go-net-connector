package workflow

import (
	"context"
	"io"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/bootstrap"
)

func runtimeForTest(services *bootstrap.Services) Runtime {
	return Runtime{
		Rules:        services.Rules(),
		Registry:     services.Registry(),
		Workers:      services.Workers(),
		Directory:    services.Directory(),
		LogDirectory: services.LogDirectory,
		OpenDevice: func(ctx context.Context, device fleet.Record, logger io.Writer, observer SessionObserver) (*connector.Device, error) {
			return services.WithSessionObserver(observer).DeviceWithLogger(ctx, device, logger)
		},
	}
}

func tftpSettingsForTest(services *bootstrap.Services) TFTPSettings {
	policy, _ := services.TFTPPolicy()
	workerPath, _ := services.Policy().Value("tftp.ssh_worker_path").(string)
	return TFTPSettings{
		ArchiveTimeouts: services.ArchiveTimeouts(),
		Policy:          policy, LimitPerVendor: services.Limit(),
		ArchiveWorkerPath: workerPath, ArchivePassword: services.ArchivePassword,
	}
}

func topologySettingsForTest(policy config.Snapshot, services *bootstrap.Services, mode string) TopologySettings {
	settings := TopologySettings{
		Mode: mode, Workers: services.Workers(),
		DescriptionFormat: "To_%{neighbor}_%{port}", AbbreviatePorts: true,
	}
	settings.Directory, _ = policy.Value("topology.directory").(string)
	if n, ok := policy.Value("topology.parallel").(int); ok {
		settings.Workers = n
	}
	if n, ok := policy.Value("topology.limit").(int); ok {
		settings.Limit = n
	}
	if v, ok := policy.Value("topology.description_format").(string); ok && v != "" {
		settings.DescriptionFormat = v
	}
	if v, ok := policy.Value("topology.abbreviate_ports").(bool); ok {
		settings.AbbreviatePorts = v
	}
	settings.LowercasePorts, _ = policy.Value("topology.lowercase_ports").(bool)
	return settings
}
