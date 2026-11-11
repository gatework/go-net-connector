package cli

import (
	"net/netip"
	"strings"

	"github.com/gatework/go-net-connector/internal/batchreport"
	"github.com/gatework/go-net-connector/netdisco"

	"github.com/spf13/cobra"
)

func prepareCommandOptions(c *cobra.Command, o *options) error {
	f := c.Flags()
	for _, name := range []string{"config", "env-file", "directory", "batch", "output", "layout", "for", "method"} {
		if f.Changed(name) {
			value, _ := f.GetString(name)
			if strings.TrimSpace(value) == "" {
				return newUsageError("--" + name + " 不能为空")
			}
		}
	}
	if f.Lookup("for") != nil {
		scope, _ := f.GetString("for")
		if scope != "backup" && scope != "topology" && (c.Parent().Name() != "config" || scope != "tftp") {
			return newUsageError("用途无效，请查看 --for 选项")
		}
		o.mode = scope
	}
	if f.Lookup("method") != nil {
		method, _ := f.GetString("method")
		if method != "cli" && method != "tftp" && method != "remote" {
			return newUsageError("备份方式必须为 cli、tftp 或 remote")
		}
		if method != "cli" {
			o.mode = method
		}
	}
	if o.action == "export" && o.batch == "" {
		return newUsageError("导出必须通过 --batch 明确指定批次目录")
	}
	if o.action == "export" && o.json && o.output == "" {
		return newUsageError("--json 需与 --output 同时使用；直接导出正文时不附加元数据")
	}
	if o.nonInteractive && o.interactive || o.all && o.sample != 0 || o.sample < 0 || o.sample > 5 || f.Changed("sample") && o.sample == 0 || o.parallel < 0 || o.parallel > 500 || f.Changed("parallel") && o.parallel == 0 || o.limit < 0 || f.Changed("limit") && o.limit == 0 {
		return newUsageError("抽样、并发、数量上限或交互选项无效")
	}
	if !batchreport.Layout(o.layout).Valid() {
		return newUsageError("目录布局必须为 batch 或 flat")
	}
	if o.policy != "" && !batchreport.SuccessPolicy(o.policy).AllowedFor(o.mode == "tftp") {
		return newUsageError("成功策略无效；verified 仅适用于 TFTP")
	}
	if f.Changed("host") && f.Changed("hosts") {
		return newUsageError("--host 与 --hosts 不能同时指定")
	}
	if f.Changed("host") {
		hosts, _ := f.GetStringArray("host")
		for _, host := range hosts {
			a, e := netip.ParseAddr(host)
			if e != nil || a.Zone() != "" {
				return newUsageError("--host 必须为精确 IP 地址；范围筛选请使用 --hosts")
			}
		}
	}
	askDevice, _ := f.GetBool("ask-password")
	askInventory, _ := f.GetBool("ask-netdisco-password")
	if o.stdin && (askDevice || askInventory) || askDevice && f.Changed("password") || askInventory && f.Changed("netdisco-password") || o.nonInteractive && (askDevice || askInventory) {
		return newUsageError("凭据输入方式互斥或当前模式禁止交互")
	}
	if o.refresh && f.Changed("cache-only") {
		return newUsageError("刷新清单与仅使用缓存不能同时指定")
	}
	if o.quiet && o.verbose {
		return newUsageError("--quiet 与 --verbose 不能同时指定")
	}
	if f.Changed("cache-mode") {
		cacheMode, _ := f.GetString("cache-mode")
		only, _ := f.GetBool("cache-only")
		if o.refresh && netdisco.CacheMode(cacheMode) != netdisco.CacheRefresh || only && netdisco.CacheMode(cacheMode) != netdisco.CacheOnly {
			return newUsageError("清单缓存选项互相冲突")
		}
	}
	public := map[string]string{"netdisco-url": "ND_URL", "source": "ND_SOURCE", "query": "ND_QUERY", "query-params": "ND_QUERY_PARAMS", "cache-mode": "ND_CACHE_MODE", "known-hosts": "NC_KNOWN_HOSTS", "jump-host": "NC_SSH_JUMP_HOST", "connect-retries": "NC_SSH_CONNECT_RETRIES", "host-key-policy": "NC_HOST_KEY_POLICY", "protocol": "NC_PROTOCOL", "max-script-output-bytes": "NC_MAX_SCRIPT_OUTPUT_BYTES", "tftp-root": "NC_TFTP_ROOT", "tftp-server": "NC_TFTP_HOST", "name-match": "NC_NAME_MATCH", "model-match": "NC_MODEL_MATCH", "layout": "NC_BACKUP_LAYOUT"}
	secrets := map[string]string{ //nolint:gosec // maps flags to environment variable names, not secrets
		"username": "NC_DEVICE_USERNAME", "password": "NC_DEVICE_PASSWORD", "enable-password": "NC_ENABLE_PASSWORD", "netdisco-username": "ND_USERNAME", "netdisco-password": "ND_PASSWORD"}
	filters := map[string]string{"hosts": "NC_INCLUDE_HOSTS", "host": "NC_INCLUDE_HOSTS", "exclude-hosts": "NC_EXCLUDE_HOSTS", "name": "NC_INCLUDE_NAMES", "exclude-name": "NC_EXCLUDE_NAMES", "vendor": "NC_INCLUDE_VENDORS", "exclude-vendor": "NC_EXCLUDE_VENDORS", "model": "NC_INCLUDE_MODELS", "exclude-model": "NC_EXCLUDE_MODELS"}
	for name, key := range public {
		if f.Changed(name) {
			o.public[key], _ = f.GetString(name)
		}
	}
	for name, key := range secrets {
		if f.Changed(name) {
			o.secrets[key], _ = f.GetString(name)
		}
	}
	for name, key := range filters {
		if f.Changed(name) {
			values, _ := f.GetStringArray(name)
			o.public[key] = strings.Join(values, ",")
		}
	}
	if o.stdin && len(o.secrets) > 0 {
		return newUsageError("stdin 凭据不能与显式凭据同时使用")
	}
	if only, _ := f.GetBool("cache-only"); only {
		o.public["ND_CACHE_MODE"] = string(netdisco.CacheOnly)
	}
	if o.directory != "" && o.mode != "deploy" && o.mode != "exec" {
		key := "NC_BACKUP_DIRECTORY"
		if o.mode == "topology" {
			key = "NC_TOPOLOGY_DIRECTORY"
		}
		o.public[key] = o.directory
	}
	if o.parallel > 0 {
		key := "NC_PARALLEL"
		if o.mode == "topology" {
			key = "NC_TOPOLOGY_PARALLEL"
		}
		o.public[key] = integerString(o.parallel)
	}
	if o.sample > 0 {
		o.public["NC_SAMPLE_PER_VENDOR"] = integerString(o.sample)
	}
	if o.all {
		o.public["NC_SAMPLE_PER_VENDOR"] = "0"
	}
	if f.Changed("success-policy") {
		o.public["NC_BACKUP_SUCCESS_POLICY"] = o.policy
	}
	if o.description != "" {
		o.public["NC_TOPOLOGY_DESCRIPTION_FORMAT"] = o.description
	}
	if f.Changed("full-port-names") {
		o.public["NC_TOPOLOGY_ABBREVIATE"] = booleanString(!o.fullPorts)
	}
	if f.Changed("lowercase-ports") {
		o.public["NC_TOPOLOGY_LOWERCASE"] = booleanString(o.lowercase)
	}
	return nil
}

func booleanString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
