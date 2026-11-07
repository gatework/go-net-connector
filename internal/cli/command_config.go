package cli

import (
	"github.com/spf13/cobra"
)

func addConfigCommands(root *cobra.Command, leaf commandBuilder) {
	configuration := commandGroup("config", "离线初始化、查看和检查本地配置")
	for _, action := range []string{"init", "show", "check"} {
		descriptions := map[string]string{"init": "创建缺失的私有配置文件后退出，不访问清单或设备", "show": "显示有效的非敏感配置及其来源", "check": "仅检查本地配置，不验证远程访问或设备凭据"}
		leaf(configuration, action, descriptions[action], "backup", action, func(c *cobra.Command, o *options) {
			c.Flags().String("for", "backup", "配置`用途`：backup、tftp 或 topology")
			c.Flags().BoolVar(&o.json, "json", false, "以 JSON 输出结果")
			if action == "init" {
				c.Flags().BoolVar(&o.interactive, "interactive", false, "在终端使用中文配置向导")
				c.Flags().BoolVar(&o.nonInteractive, "non-interactive", false, "禁止读取交互输入")
			}
			c.Example = "  netman config " + action + " --for backup"
		})
	}
	root.AddCommand(configuration)
}
