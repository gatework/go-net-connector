package cli

import (
	"github.com/spf13/cobra"
)

func addFactsCommands(root *cobra.Command, leaf commandBuilder) {
	facts := commandGroup("facts", "采集设备自报的型号、序列号与版本，并与清单比对")
	bind := func(c *cobra.Command, o *options, progress bool) {
		bindOperation(c, o, progress)
		c.Flags().StringVar(&o.directory, "directory", "", "报告`目录`（默认：./facts）")
	}
	leaf(facts, "plan", "预览支持事实采集的设备，不连接设备", "facts", "plan", func(c *cobra.Command, o *options) {
		bind(c, o, false)
		c.Example = "  netman facts plan --platform cisco_ios --json"
	})
	leaf(facts, "run", "逐台执行平台声明的只读命令并报告与清单不一致的字段", "facts", "run", func(c *cobra.Command, o *options) {
		bind(c, o, true)
		c.Example = "  netman facts run --hosts 192.0.2.0/24 --json"
	})
	root.AddCommand(facts)
}
