package cli

import (
	"context"
	"io"

	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/fleet"
	"github.com/gatework/go-net-connector/internal/bootstrap"
	"github.com/gatework/go-net-connector/internal/workflow"
)

func workflowRuntime(services *bootstrap.Services) workflow.Runtime {
	return workflow.Runtime{
		Rules:        services.Rules(),
		Registry:     services.Registry(),
		Workers:      services.Workers(),
		Directory:    services.Directory(),
		LogDirectory: services.LogDirectory,
		OpenDevice: func(ctx context.Context, device fleet.Record, logger io.Writer, observer workflow.SessionObserver) (*connector.Device, error) {
			return services.WithSessionObserver(observer).DeviceWithLogger(ctx, device, logger)
		},
	}
}

func tftpSettings(services *bootstrap.Services) (workflow.TFTPSettings, error) {
	policy, err := services.TFTPPolicy()
	if err != nil {
		return workflow.TFTPSettings{}, err
	}
	workerPath, _ := services.Policy().Value("tftp.ssh_worker_path").(string)
	return workflow.TFTPSettings{
		ArchiveTimeouts:   services.ArchiveTimeouts(),
		Policy:            policy,
		LimitPerVendor:    services.Limit(),
		ArchiveWorkerPath: workerPath,
		ArchivePassword:   services.ArchivePassword,
	}, nil
}

func topologySettings(policy config.Snapshot, services *bootstrap.Services) (workflow.TopologySettings, error) {
	settings := workflow.TopologySettings{
		Mode:                 snapshotString(policy, "topology.mode"),
		Workers:              services.Workers(),
		Directory:            snapshotString(policy, "topology.directory"),
		DescriptionFormat:    snapshotString(policy, "topology.description_format"),
		AbbreviatePorts:      true,
		NeighborNamePrefixes: []string{},
	}
	if value, ok := policy.Value("topology.parallel").(int); ok {
		settings.Workers = value
	}
	if value, ok := policy.Value("topology.limit").(int); ok {
		settings.Limit = value
	}
	if value, ok := policy.Value("topology.abbreviate_ports").(bool); ok {
		settings.AbbreviatePorts = value
	}
	settings.LowercasePorts, _ = policy.Value("topology.lowercase_ports").(bool)
	if values, ok := policy.Value("topology.neighbor_name_prefixes").([]any); ok {
		for _, value := range values {
			prefix, ok := value.(string)
			if !ok {
				return workflow.TopologySettings{}, config.ErrInvalid
			}
			settings.NeighborNamePrefixes = append(settings.NeighborNamePrefixes, prefix)
		}
	}
	return settings, nil
}

func snapshotString(snapshot config.Snapshot, path string) string {
	value, _ := snapshot.Value(path).(string)
	return value
}
