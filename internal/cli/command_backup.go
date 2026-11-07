package cli

import (
	"net/netip"

	"github.com/spf13/cobra"
)

func addBackupCommands(root *cobra.Command, leaf commandBuilder) {
	backup := commandGroup("backup", "配置备份、目标规划与批次导出")
	leaf(backup, "run", "采集配置并保存为设备名称-IP.txt", "backup", "run", func(c *cobra.Command, o *options) {
		bindOperation(c, o, true)
		bindBackup(c, o)
		c.Example = "  netman backup run --all --json\n  netman backup run --sample 2 --success-policy strict"
	})
	leaf(backup, "tftp", "原生 TFTP 上传、文件核验和归档", "tftp", "run", func(c *cobra.Command, o *options) {
		bindOperation(c, o, true)
		bindBackup(c, o)
		bindTFTPFlags(c)
		c.Example = "  netman backup tftp --tftp-server 192.0.2.9 --tftp-root /srv/tftp --success-policy verified --json"
	})
	leaf(backup, "plan", "预览备份目标，不连接设备或归档服务", "backup", "plan", func(c *cobra.Command, o *options) {
		bindOperation(c, o, false)
		bindBackup(c, o)
		bindTFTPFlags(c)
		c.Flags().String("method", "cli", "备份`方式`：cli 或 tftp")
		c.Example = "  netman backup plan --method cli --json\n  netman backup plan --method tftp --tftp-server 192.0.2.9"
	})
	exp := leaf(backup, "export <IP>", "从明确指定的批次导出配置，不访问网络", "backup", "export", func(c *cobra.Command, o *options) {
		c.Flags().StringVar(&o.batch, "batch", "", "明确的批次`目录`，不自动查找最新批次")
		c.Flags().StringVar(&o.output, "output", "", "导出目标`文件`；未指定时输出原文件正文")
		c.Flags().BoolVar(&o.json, "json", false, "以 JSON 输出导出文件的元数据；需指定 --output")
	})
	exp.Args = func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return usageError("必须指定一个设备 IP")
		}
		if a, e := netip.ParseAddr(args[0]); e != nil || a.Zone() != "" {
			return usageError("设备 IP 无效")
		}
		return nil
	}
	exp.Example = "  netman backup export 192.0.2.1 --batch ./backups/2026-11-03_10-00-00 --output ./saved.txt"
	root.AddCommand(backup)
}
