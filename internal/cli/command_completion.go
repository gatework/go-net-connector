package cli

import (
	"github.com/spf13/cobra"
)

func addCompletionCommands(root *cobra.Command) {
	completion := commandGroup("completion", "生成无需配置或凭据的命令补全脚本")
	for _, shell := range []string{"bash", "zsh", "fish"} {
		completion.AddCommand(&cobra.Command{Use: shell, Short: "生成 " + shell + " 补全脚本", Long: "生成 " + shell + " 补全脚本；补全只使用静态命令、枚举和文件路径。", Example: "  netman completion " + shell, Args: noArguments, RunE: func(c *cobra.Command, _ []string) error {
			switch shell {
			case "bash":
				return root.GenBashCompletionV2(c.OutOrStdout(), true)
			case "zsh":
				return root.GenZshCompletion(c.OutOrStdout())
			default:
				return root.GenFishCompletion(c.OutOrStdout(), true)
			}
		}})
	}
	root.AddCommand(completion)
}
