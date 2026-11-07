package cli

import (
	"github.com/spf13/cobra"
)

func addTopologyCommands(root *cobra.Command, leaf commandBuilder) {
	topology := commandGroup("topology", "发现、规划和重新核验后应用拓扑变更")
	for _, action := range []string{"discover", "plan", "apply"} {
		descriptions := map[string]string{"discover": "只读发现并审计拓扑", "plan": "读取设备邻居和描述，生成变更计划，不下发修改", "apply": "生成计划、重新核验并显式应用变更"}
		leaf(topology, action, descriptions[action], "topology", action, func(c *cobra.Command, o *options) {
			bindOperation(c, o, true)
			c.Flags().IntVar(&o.limit, "limit", 0, "拓扑设备数量`上限`，未指定时使用配置")
			c.Flags().StringVar(&o.directory, "directory", "", "拓扑报告输出`目录`")
			c.Flags().StringVar(&o.description, "description-format", "", "接口描述`格式`，支持 %{neighbor} 与 %{port}")
			c.Flags().BoolVar(&o.fullPorts, "full-port-names", false, "保留完整端口名称")
			c.Flags().BoolVar(&o.lowercase, "lowercase-ports", false, "端口名称转为小写")
			c.Example = "  netman topology " + action + " --hosts 192.0.2.1 --json"
		})
	}
	root.AddCommand(topology)
}
