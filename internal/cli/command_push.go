package cli

import (
	"github.com/spf13/cobra"
)

func addPushCommands(root *cobra.Command, leaf commandBuilder) {
	push := commandGroup("push", "批量下发命令脚本，可选保存配置")
	bind := func(c *cobra.Command, o *options, progress bool) {
		bindOperation(c, o, progress)
		f := c.Flags()
		f.StringVar(&o.pushScript, "script", "", "适用于所有平台的命令脚本`文件`；每行一条命令，# 开头为注释")
		f.StringArrayVar(&o.pushScriptFor, "script-for", nil, "指定平台的脚本，格式 `平台=文件`，优先于 --script；可重复")
		f.StringVar(&o.directory, "directory", "", "下发计划与报告输出`目录`（默认：./pushes）")
		f.BoolVar(&o.pushSave, "save", false, "全部命令被接受后保存配置；仅以设备的保存确认为成功")
		f.IntVar(&o.pushStopAfter, "stop-after-failures", 0, "失败设备达到该`数量`后不再启动新设备；0 表示不限制")
	}
	leaf(push, "plan", "校验脚本并预览下发目标，不连接设备", "push", "plan", func(c *cobra.Command, o *options) {
		bind(c, o, false)
		c.Example = "  netman push plan --script-for h3c=vlan.txt --hosts 192.0.2.0/28 --json"
	})
	leaf(push, "run", "逐台下发脚本；失败设备不重试、不重放", "push", "run", func(c *cobra.Command, o *options) {
		bind(c, o, true)
		c.Example = "  netman push run --script ntp.txt --vendor cisco --save --stop-after-failures 1"
	})
	root.AddCommand(push)
}
