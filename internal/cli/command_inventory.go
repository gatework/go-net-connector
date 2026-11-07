package cli

import (
	"github.com/spf13/cobra"
)

func addInventoryCommands(root *cobra.Command, leaf commandBuilder) {
	inventory := commandGroup("inventory", "查看设备清单，不连接设备")
	leaf(inventory, "list", "预览筛选后的设备清单", "backup", "inventory", func(c *cobra.Command, o *options) {
		bindOperation(c, o, false)
		c.Flags().String("for", "backup", "清单`用途`：backup 或 topology")
		c.Example = "  netman inventory list --for topology --hosts 192.0.2.0/24 --json"
	})
	root.AddCommand(inventory)
}
