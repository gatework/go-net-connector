package cli

import (
	"github.com/spf13/pflag"
)

type options struct {
	action, layout, batch, topologyMode string
	inventoryOnly                       bool
	verbose                             bool

	init, interactive, nonInteractive, show, plan, json, quiet, all, stdin, refresh, apply, discover, fullPorts, lowercase bool
	sample, parallel, limit                                                                                                int
	config, envFile, directory, export, output, policy, mode, description                                                  string
	public, secrets                                                                                                        map[string]string
	flags                                                                                                                  *pflag.FlagSet
}
