package cli

import (
	"github.com/spf13/cobra"
)

func addExecCommands(root *cobra.Command, leaf commandBuilder) {
	exec := commandGroup("exec", "只读命令批量采集；仅允许平台声明的只读命令")
	bind := func(c *cobra.Command, o *options, progress bool) {
		bindOperation(c, o, progress)
		f := c.Flags()
		f.StringArrayVar(&o.execCommands, "command", nil, "只读`命令`，如 show version；可重复")
		f.StringVar(&o.execFile, "commands-file", "", "只读命令`文件`；每行一条，# 开头为注释")
		f.StringVar(&o.directory, "directory", "", "采集输出与报告`目录`（默认：./outputs）")
	}
	leaf(exec, "plan", "校验命令并预览采集目标，不连接设备", "exec", "plan", func(c *cobra.Command, o *options) {
		bind(c, o, false)
		c.Example = "  netman exec plan --command 'show version' --platform cisco_ios --json"
	})
	leaf(exec, "run", "逐台执行只读命令并保存输出为设备名称-IP.txt", "exec", "run", func(c *cobra.Command, o *options) {
		bind(c, o, true)
		c.Example = "  netman exec run --command 'display interface brief' --hosts 192.0.2.0/24"
	})
	root.AddCommand(exec)
}
