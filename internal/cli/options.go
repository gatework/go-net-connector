package cli

import (
	"github.com/spf13/pflag"
)

type options struct {
	action, layout, batch string
	verbose               bool

	interactive, nonInteractive, json, quiet, all, stdin, refresh, fullPorts, lowercase bool
	sample, parallel, limit                                                             int
	config, envFile, directory, export, output, policy, mode, description               string
	public, secrets                                                                     map[string]string
	flags                                                                               *pflag.FlagSet

	deployScript    string
	deployScriptFor []string
	save            bool
	deployBackup    bool
	execCommands    []string
	execFile        string
	remoteServer    string
	remotePath      string
	remoteUsername  string
	remoteProtocol  string
	diffFrom        string
	diffTo          string
	deployStopAfter int
}
