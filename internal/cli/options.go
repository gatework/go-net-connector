package cli

import (
	"github.com/gatework/go-net-connector/config"
	"github.com/gatework/go-net-connector/internal/buildinfo"
	"github.com/spf13/pflag"
	"io"
	"net/netip"
	"strings"
)

type options struct {
	help, init, interactive, nonInteractive, show, plan, json, quiet, all, stdin, refresh, tftp, apply, discover, fullPorts, lowercase bool
	sample, parallel, limit                                                                                                            int
	config, envFile, directory, export, output, policy, mode, description                                                              string
	public, secrets                                                                                                                    map[string]string
	flags                                                                                                                              *pflag.FlagSet
}

func parse(args []string, program string, out io.Writer) (options, error) {
	o := options{public: map[string]string{}, secrets: map[string]string{}, policy: "selected", mode: "backup"}
	if program == "net-backup" {
		o.policy = "strict"
	}
	if program == "net-backup-tftp" {
		o.mode = "tftp"
	}
	if program == "net-topology" {
		o.mode = "topology"
	}
	f := pflag.NewFlagSet(program, pflag.ContinueOnError)
	f.SetOutput(io.Discard)
	o.flags = f
	f.Bool("version", false, "Show version and source revision")
	f.BoolP("version-short", "v", false, "Show version and source revision")
	f.BoolVarP(&o.help, "help", "h", false, "Show options without loading files")
	f.BoolVar(&o.help, "help-all", false, "Show all options")
	f.BoolVar(&o.help, "examples", false, "Show usage examples")
	f.BoolVar(&o.init, "init", false, "Create missing private configuration files and exit")
	f.BoolVar(&o.interactive, "interactive", false, "Use interactive setup")
	f.BoolVar(&o.nonInteractive, "non-interactive", false, "Never prompt")
	f.BoolVar(&o.show, "show-config", false, "Show public settings")
	f.StringVar(&o.config, "config", "", "Configuration loaded last")
	f.StringVar(&o.envFile, "env-file", "", "Environment file loaded last")
	f.StringVar(&o.directory, "directory", "", "Output directory")
	f.StringVar(&o.export, "export", "", "Export saved configuration for IP")
	f.StringVar(&o.output, "output", "", "Export destination")
	f.BoolVar(&o.plan, "inventory-only", false, "Read inventory without device connections")
	if o.mode != "topology" {
		f.BoolVar(&o.plan, "plan", false, "Read inventory without device connections")
	} else {
		f.BoolVar(&o.apply, "apply", false, "Explicitly apply a reviewed topology plan")
		f.BoolVar(&o.discover, "discover", false, "Discover and audit topology")
		f.Bool("plan", false, "Plan topology changes")
		f.IntVar(&o.limit, "limit", 0, "Maximum topology devices")
		f.StringVar(&o.description, "description-format", "", "Description format")
		f.BoolVar(&o.fullPorts, "full-port-names", false, "Keep full port names")
		f.BoolVar(&o.lowercase, "lowercase-ports", false, "Lowercase port names")
	}
	f.BoolVar(&o.json, "json", false, "Write JSON results")
	f.BoolVar(&o.quiet, "no-progress", false, "Suppress progress")
	f.BoolVar(&o.quiet, "quiet", false, "Suppress progress")
	f.Bool("verbose", false, "Show progress details")
	f.BoolVar(&o.all, "all", false, "Select all ready devices")
	f.IntVar(&o.sample, "sample", 0, "Sample 1 to 5 per vendor")
	f.IntVar(&o.sample, "limit-per-vendor", 0, "Sample 1 to 5 per vendor")
	f.IntVarP(&o.parallel, "parallel", "p", 0, "Concurrent devices 1 to 500")
	f.StringVar(&o.policy, "success-policy", o.policy, "strict, selected, or verified")
	f.BoolVar(&o.stdin, "stdin-credentials", false, "Read credentials JSON, then require RUN")
	f.BoolVar(&o.refresh, "refresh-inventory", false, "Refresh inventory")
	f.Bool("cache-only", false, "Use validated cache only")
	f.BoolVar(&o.tftp, "tftp", false, "Use device TFTP upload")
	f.Bool("ask-password", false, "Prompt for device password")
	f.Bool("ask-netdisco-password", false, "Prompt for inventory password")
	public := map[string]string{"netdisco-url": "ND_URL", "source": "ND_SOURCE", "query": "ND_QUERY", "query-params": "ND_QUERY_PARAMS", "cache-mode": "ND_CACHE_MODE", "known-hosts": "NC_KNOWN_HOSTS", "host-key-policy": "NC_HOST_KEY_POLICY", "protocol": "NC_PROTOCOL", "max-script-output-bytes": "NC_MAX_SCRIPT_OUTPUT_BYTES", "tftp-root": "NC_TFTP_ROOT", "tftp-server": "NC_TFTP_HOST", "name-match": "NC_NAME_MATCH", "model-match": "NC_MODEL_MATCH"}
	secrets := map[string]string{"username": "NC_DEVICE_USERNAME", "password": "NC_DEVICE_PASSWORD", "enable-password": "NC_ENABLE_PASSWORD", "netdisco-username": "ND_USERNAME", "netdisco-password": "ND_PASSWORD"}
	filters := map[string]string{"hosts": "NC_INCLUDE_HOSTS", "exclude-hosts": "NC_EXCLUDE_HOSTS", "names": "NC_INCLUDE_NAMES", "exclude-names": "NC_EXCLUDE_NAMES", "vendors": "NC_INCLUDE_VENDORS", "exclude-vendors": "NC_EXCLUDE_VENDORS", "models": "NC_INCLUDE_MODELS", "exclude-models": "NC_EXCLUDE_MODELS", "host": "NC_INCLUDE_HOSTS", "name": "NC_INCLUDE_NAMES", "exclude-name": "NC_EXCLUDE_NAMES", "vendor": "NC_INCLUDE_VENDORS", "exclude-vendor": "NC_EXCLUDE_VENDORS", "model": "NC_INCLUDE_MODELS", "exclude-model": "NC_EXCLUDE_MODELS"}
	for name := range public {
		f.String(name, "", "Public setting")
	}
	for name := range secrets {
		f.String(name, "", "Explicit credential")
	}
	for name := range filters {
		f.StringArray(name, nil, "Inventory filter; repeat to combine")
	}
	// Help is a pure path even if other arguments refer to inaccessible files.
	for _, arg := range args {
		if arg == "--version" || arg == "-v" {
			o.help = true
			_, e := io.WriteString(out, buildinfo.Identity+"\n")
			return o, e
		}
		if arg == "--help" || arg == "-h" || arg == "--help-all" {
			o.help = true
			usage := f.FlagUsages()
			if arg != "--help-all" {
				usage = commonUsages(program)
			}
			_, e := io.WriteString(out, "Usage: "+program+" [options]\n"+usage)
			return o, e
		}
		if arg == "--examples" {
			o.help = true
			_, e := io.WriteString(out, examples(program))
			return o, e
		}
	}
	if f.Parse(args) != nil || f.NArg() != 0 {
		return o, config.ErrInvalid
	}
	if o.nonInteractive && o.interactive || o.all && o.sample != 0 || o.sample < 0 || o.sample > 5 || o.parallel < 0 || o.parallel > 500 || o.limit < 0 || o.apply && o.discover || f.Changed("host") && f.Changed("hosts") || o.mode == "topology" && o.tftp || o.refresh && f.Changed("cache-only") {
		return o, config.ErrInvalid
	}
	if (f.Changed("sample") || f.Changed("limit-per-vendor")) && o.sample < 1 || f.Changed("parallel") && o.parallel < 1 || f.Changed("limit") && o.limit < 1 || o.mode == "topology" && f.Changed("plan") && (o.apply || o.discover) {
		return o, config.ErrInvalid
	}
	if f.Changed("host") {
		values, _ := f.GetStringArray("host")
		for _, v := range values {
			a, e := netip.ParseAddr(v)
			if e != nil || a.Zone() != "" {
				return o, config.ErrInvalid
			}
		}
	}
	askDevice, _ := f.GetBool("ask-password")
	askInventory, _ := f.GetBool("ask-netdisco-password")
	if o.stdin && (askDevice || askInventory) || askDevice && f.Changed("password") || askInventory && f.Changed("netdisco-password") || o.nonInteractive && (askDevice || askInventory) {
		return o, config.ErrInvalid
	}
	modes := 0
	for _, active := range []bool{o.show, o.plan, o.export != ""} {
		if active {
			modes++
		}
	}
	if modes > 1 || o.output != "" && o.export == "" || f.Changed("tftp-root") && o.mode != "tftp" && !o.tftp {
		return o, config.ErrInvalid
	}
	if o.export != "" {
		for _, flag := range []string{"source", "query", "query-params", "tftp", "all", "sample", "limit-per-vendor", "parallel", "host", "success-policy", "tftp-root", "max-script-output-bytes", "refresh-inventory"} {
			if f.Changed(flag) {
				return o, config.ErrInvalid
			}
		}
	}
	if o.policy != "strict" && o.policy != "selected" && o.policy != "verified" {
		return o, config.ErrInvalid
	}
	if o.tftp {
		o.mode = "tftp"
	}
	if o.policy == "verified" && o.mode != "tftp" {
		return o, config.ErrInvalid
	}
	for name, key := range public {
		if f.Changed(name) {
			v, _ := f.GetString(name)
			o.public[key] = v
		}
	}
	for name, key := range secrets {
		if f.Changed(name) {
			v, _ := f.GetString(name)
			o.secrets[key] = v
		}
	}
	for name, key := range filters {
		if f.Changed(name) {
			v, _ := f.GetStringArray(name)
			if prior := o.public[key]; prior != "" {
				v = append(strings.Split(prior, ","), v...)
			}
			o.public[key] = strings.Join(v, ",")
		}
	}
	if v, _ := f.GetBool("cache-only"); v {
		o.public["ND_CACHE_MODE"] = "only"
	}
	if o.directory != "" {
		key := "NC_BACKUP_DIRECTORY"
		if o.mode == "topology" {
			key = "NC_TOPOLOGY_DIRECTORY"
		}
		o.public[key] = o.directory
	}
	if o.parallel > 0 {
		o.public["NC_PARALLEL"] = integerString(o.parallel)
		if o.mode == "topology" {
			o.public["NC_TOPOLOGY_PARALLEL"] = integerString(o.parallel)
		}
	}
	if o.sample > 0 {
		o.public["NC_SAMPLE_PER_VENDOR"] = integerString(o.sample)
	}
	if o.description != "" {
		o.public["NC_TOPOLOGY_DESCRIPTION_FORMAT"] = o.description
	}
	if o.fullPorts {
		o.public["NC_TOPOLOGY_ABBREVIATE"] = "false"
	}
	if o.lowercase {
		o.public["NC_TOPOLOGY_LOWERCASE"] = "true"
	}
	if o.stdin && len(o.secrets) > 0 {
		return o, config.ErrInvalid
	}
	return o, nil
}

func commonUsages(program string) string {
	usage := "  --help                 Show common options\n  --help-all             Show every option\n  --examples             Show runnable examples\n  --init                 Create missing configuration and exit\n  --interactive          Ask before creating missing files\n  --config FILE          Load a configuration file\n  --inventory-only       Preview inventory without devices\n  --json                 Write JSON results\n"
	if program == "net-topology" {
		usage += "  --discover             Discover without planning changes\n  --plan                 Plan topology changes\n  --apply                Apply after review and revalidation\n"
	} else {
		usage += "  --directory DIR        Set output directory\n  --all                  Select all ready devices\n"
	}
	return usage
}

func examples(program string) string {
	switch program {
	case "net-topology":
		return "Examples:\n  net-topology --init --non-interactive\n  net-topology --inventory-only\n  net-topology --plan --json\n  net-topology --apply --json\n"
	case "net-backup-tftp":
		return "Examples:\n  net-backup-tftp --init --non-interactive\n  net-backup-tftp --inventory-only --tftp-server 192.0.2.10\n  net-backup-tftp --tftp-server 192.0.2.10 --tftp-root /srv/tftp --json\n"
	case "net-backup-batch":
		return "Examples:\n  net-backup-batch --init --non-interactive\n  net-backup-batch --inventory-only --json\n  net-backup-batch --all --directory ./backups\n"
	default:
		return "Examples:\n  net-backup --init --non-interactive\n  net-backup --inventory-only --json\n  net-backup --all --directory ./backups\n  net-backup --tftp --tftp-server 192.0.2.10 --tftp-root /srv/tftp\n"
	}
}
