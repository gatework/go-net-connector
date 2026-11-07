package cli

import "github.com/spf13/cobra"

func bindBackup(c *cobra.Command, o *options) {
	f := c.Flags()
	f.StringVar(&o.directory, "directory", "", "备份输出`目录`")
	f.StringVar(&o.layout, "layout", "batch", "输出目录`布局`：batch（批次）或 flat（平铺）")
	f.BoolVar(&o.all, "all", false, "选择全部就绪设备并取消配置中的抽样限制")
	f.IntVar(&o.sample, "sample", 0, "每厂商抽样`数量`，范围 1 至 5；未指定时使用配置")
	f.StringVar(&o.policy, "success-policy", "", "成功`策略`：strict、selected 或 verified；默认 CLI 为 selected，TFTP 为 verified")
}
func bindTFTPFlags(c *cobra.Command) {
	c.Flags().String("tftp-server", "", "设备上传目标服务器`地址`")
	c.Flags().String("tftp-root", "", "服务器文件核验和归档的本地或 SSH 根`目录`")
}
func bindOperation(c *cobra.Command, o *options, progress bool) {
	f := c.Flags()
	f.BoolVar(&o.json, "json", false, "以 JSON 输出结果")
	f.BoolVar(&o.nonInteractive, "non-interactive", false, "禁止读取交互输入")
	f.BoolVar(&o.stdin, "stdin-credentials", false, "先读取一行凭据 JSON；备份执行或拓扑应用前再读取 RUN")
	f.BoolVar(&o.refresh, "refresh-inventory", false, "刷新设备清单")
	f.Bool("cache-only", false, "仅使用已校验的清单缓存")
	f.BoolVar(&o.quiet, "quiet", false, "隐藏进度提示")
	if progress {
		f.BoolVar(&o.verbose, "verbose", false, "在原地进度面板中显示连接和采集详情，不显示设备原始回显")
	}
	f.IntVarP(&o.parallel, "parallel", "p", 0, "设备并发`数量`，范围 1 至 500；未指定时使用配置")
	for _, flag := range []struct{ name, description string }{{"netdisco-url", "清单 HTTP 服务`地址`"}, {"source", "清单来源`类型`：http 或 postgres"}, {"query", "PostgreSQL 清单查询`语句`"}, {"query-params", "查询参数 JSON `数组`"}, {"cache-mode", "清单缓存`模式`：prefer、only 或 refresh"}, {"known-hosts", "SSH 主机密钥信任`文件`"}, {"host-key-policy", "主机密钥`策略`：strict、accept_new 或 replace"}, {"protocol", "设备连接`协议`：ssh 或 telnet"}, {"max-script-output-bytes", "脚本响应累计字节`上限`"}, {"name-match", "设备名称匹配`方式`：contains、prefix 或 glob"}, {"model-match", "设备型号匹配`方式`：contains、prefix 或 glob"}} {
		f.String(flag.name, "", flag.description)
	}
	for _, flag := range []struct{ name, description string }{{"username", "显式设备登录`用户名`"}, {"password", "显式设备登录`密码`，优先于环境变量"}, {"enable-password", "设备权限提升`密码`"}, {"netdisco-username", "清单服务登录`用户名`"}, {"netdisco-password", "清单服务登录`密码`"}} {
		f.String(flag.name, "", flag.description)
	}
	f.Bool("ask-password", false, "在终端隐藏输入设备密码")
	f.Bool("ask-netdisco-password", false, "在终端隐藏输入清单服务密码")
	for _, flag := range []struct{ name, description string }{{"host", "精确设备 IP `地址`；可重复"}, {"hosts", "设备 IP、CIDR 或地址`范围`；可重复"}, {"exclude-hosts", "排除的设备地址或`范围`；可重复"}, {"name", "设备名称筛选`条件`；可重复"}, {"exclude-name", "排除设备名称`条件`；可重复"}, {"vendor", "厂商筛选`名称`；可重复"}, {"exclude-vendor", "排除厂商`名称`；可重复"}, {"model", "设备型号筛选`条件`；可重复"}, {"exclude-model", "排除设备型号`条件`；可重复"}} {
		f.StringArray(flag.name, nil, flag.description)
	}
}
