package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gatework/go-net-connector/internal/buildinfo"
	"github.com/spf13/cobra"
)

type commandExit int

func (e commandExit) Error() string { return "命令未完成" }

type commandUsage struct{ message string }

func (e *commandUsage) Error() string { return e.message }
func usageError(message string) error { return &commandUsage{message} }

func (r Runtime) runCommands(ctx context.Context) int {
	var outputErr error
	root := r.commandTree(ctx, &outputErr)
	root.SetArgs(append([]string{}, r.Args...))
	root.SetIn(r.Input)
	root.SetOut(&commandOutput{writer: r.Output, first: &outputErr})
	root.SetErr(r.Error)
	err := root.ExecuteContext(ctx)
	if outputErr != nil {
		err = outputErr
	}
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return 130
	}
	if err == nil {
		return 0
	}
	var exit commandExit
	if errors.As(err, &exit) {
		return int(exit)
	}
	message := "命令或参数无效，请使用 netman help 查看用法"
	var bad *commandUsage
	if errors.As(err, &bad) {
		message = bad.message
	}
	fmt.Fprintln(r.Error, "netman："+message)
	return 2
}

func (r Runtime) commandTree(ctx context.Context, outputErr *error) *cobra.Command {
	var configFile, envFile string
	root := &cobra.Command{Use: "netman", Short: "网络设备配置备份、清单与拓扑管理", Long: "统一管理配置采集、原生上传、设备清单和拓扑；帮助、版本和补全不读取配置或凭据。", SilenceErrors: true, SilenceUsage: true, Version: buildinfo.Identity}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVar(&configFile, "config", "", "最后加载的配置`文件`；相对路径从启动目录解析")
	root.PersistentFlags().StringVar(&envFile, "env-file", "", "最后加载的环境`文件`；不修改进程环境")
	root.Flags().BoolP("version", "v", false, "显示版本和源码修订号")
	root.SetVersionTemplate("netman {{.Version}}\n")
	root.SetFlagErrorFunc(func(c *cobra.Command, _ error) error {
		return usageError("选项无效或缺少参数，请使用 " + c.CommandPath() + " --help 查看用法")
	})
	root.SetHelpFunc(func(c *cobra.Command, _ []string) {
		if err := renderChineseHelp(c, c.OutOrStdout()); err != nil && *outputErr == nil {
			*outputErr = err
		}
	})
	root.SetUsageFunc(func(c *cobra.Command) error { return renderChineseHelp(c, c.OutOrStdout()) })
	root.RunE = func(c *cobra.Command, _ []string) error { return renderChineseHelp(c, c.OutOrStdout()) }
	root.Args = noArguments
	help := &cobra.Command{Use: "help [命令路径]", Short: "显示指定命令的中文帮助", Long: "按命令层级查询帮助，不读取配置文件或凭据。", Example: "  netman help backup run", RunE: func(c *cobra.Command, args []string) error {
		target, remaining, err := root.Find(args)
		if err != nil || len(remaining) > 0 {
			return usageError("未找到指定命令，请使用 netman --help 查看子命令")
		}
		return renderChineseHelp(target, c.OutOrStdout())
	}}
	root.SetHelpCommand(help)
	leaf := func(parent *cobra.Command, name, short, mode, action string, bind func(*cobra.Command, *options)) *cobra.Command {
		o := options{mode: mode, action: action, policy: "selected", layout: "batch", public: map[string]string{}, secrets: map[string]string{}}
		c := &cobra.Command{Use: name, Short: short, Long: short, Args: noArguments, Example: "  netman " + parent.Name() + " " + strings.Fields(name)[0] + " --help"}
		bind(c, &o)
		c.PreRunE = func(c *cobra.Command, args []string) error {
			o.config = configFile
			o.envFile = envFile
			o.flags = c.Flags()
			if action == "export" {
				o.export = args[0]
			}
			if err := prepareCommandOptions(c, &o); err != nil {
				return err
			}
			return nil
		}
		c.RunE = func(c *cobra.Command, _ []string) error {
			if code := r.executeSelected(c.Context(), o); code != 0 {
				return commandExit(code)
			}
			return nil
		}
		parent.AddCommand(c)
		return c
	}
	addBackupCommands(root, leaf)
	addInventoryCommands(root, leaf)
	addTopologyCommands(root, leaf)
	addConfigCommands(root, leaf)
	addCompletionCommands(root)
	root.AddCommand(&cobra.Command{Use: "version", Short: "显示版本与源码修订号", Long: "显示构建身份，不读取配置或凭据。", Example: "  netman version", Args: noArguments, RunE: func(c *cobra.Command, _ []string) error {
		_, err := fmt.Fprintln(c.OutOrStdout(), "netman", buildinfo.Identity)
		return err
	}})
	var customize func(*cobra.Command)
	customize = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = "显示当前命令的中文帮助"
		}
		for name, values := range map[string][]string{"layout": {"batch", "flat"}, "method": {"cli", "tftp"}, "success-policy": {"strict", "selected", "verified"}, "protocol": {"ssh", "telnet"}, "host-key-policy": {"strict", "accept_new", "replace"}, "cache-mode": {"prefer", "only", "refresh"}, "source": {"http", "postgres"}, "for": {"backup", "tftp", "topology"}, "name-match": {"contains", "prefix", "glob"}, "model-match": {"contains", "prefix", "glob"}} {
			if c.Flags().Lookup(name) != nil {
				_ = c.RegisterFlagCompletionFunc(name, func(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
					var matches []string
					for _, value := range values {
						if strings.HasPrefix(value, prefix) {
							matches = append(matches, value)
						}
					}
					return matches, cobra.ShellCompDirectiveNoFileComp
				})
			}
		}
		for _, child := range c.Commands() {
			customize(child)
		}
	}
	customize(root)
	return root
}
func noArguments(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError("存在未知子命令或多余的位置参数")
	}
	return nil
}

type commandBuilder func(*cobra.Command, string, string, string, string, func(*cobra.Command, *options)) *cobra.Command

func commandGroup(name, description string) *cobra.Command {
	return &cobra.Command{Use: name, Short: description, Long: description, Args: noArguments, RunE: func(c *cobra.Command, _ []string) error { return renderChineseHelp(c, c.OutOrStdout()) }}
}

type commandOutput struct {
	writer io.Writer
	first  *error
}

func (w *commandOutput) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil && *w.first == nil {
		*w.first = err
	}
	return n, err
}
