package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// renderChineseHelp writes the help of c: description, usage, subcommands,
// flags by group, inherited flags and examples.
func renderChineseHelp(c *cobra.Command, w io.Writer) error {
	var b strings.Builder
	description := c.Long
	if description == "" {
		description = c.Short
	}
	fmt.Fprintln(&b, description)
	fmt.Fprintf(&b, "\n用法：\n  %s\n", helpUsage(c))
	shown := false
	for _, child := range c.Commands() {
		if child.Hidden {
			continue
		}
		if !shown {
			b.WriteString("\n子命令：\n")
			shown = true
		}
		fmt.Fprintf(&b, "  %-18s %s\n", child.Name(), child.Short)
	}
	for _, group := range []string{"常用选项", "设备筛选", "连接与认证", "高级选项"} {
		selected := pflag.NewFlagSet(group, pflag.ContinueOnError)
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if flagHelpGroup(f.Name) == group {
				selected.AddFlag(f)
			}
		})
		writeFlagHelp(&b, group, selected)
	}
	writeFlagHelp(&b, "全局选项", c.InheritedFlags())
	if c.Example != "" {
		fmt.Fprintf(&b, "\n示例：\n%s\n", c.Example)
	}
	b.WriteString("\n使用 netman help <命令路径> 查看详细帮助。\n")
	text := b.String()
	n, err := io.WriteString(w, text)
	if err == nil && n != len(text) {
		return io.ErrShortWrite
	}
	return err
}

// helpUsage is the usage line of c.
func helpUsage(c *cobra.Command) string {
	usage := c.CommandPath()
	if fields := strings.SplitN(c.Use, " ", 2); len(fields) == 2 {
		usage += " " + fields[1]
	}
	if c.HasAvailableSubCommands() {
		usage += " [子命令]"
	}
	if c.HasAvailableFlags() {
		usage += " [选项]"
	}
	return usage
}

// writeFlagHelp writes one titled group of visible flags, if any.
func writeFlagHelp(b *strings.Builder, title string, flags *pflag.FlagSet) {
	shown := false
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		if !shown {
			fmt.Fprintf(b, "\n%s：\n", title)
			shown = true
		}
		placeholder, usage := pflag.UnquoteUsage(f)
		if f.Value.Type() != "bool" && !strings.Contains(f.Usage, "`") {
			placeholder = "值"
		}
		label := "--" + f.Name
		if f.Shorthand != "" {
			label = "-" + f.Shorthand + ", " + label
		}
		if placeholder != "" {
			label += " <" + placeholder + ">"
		}
		fmt.Fprintf(b, "  %-26s %s", label, usage)
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "[]" {
			fmt.Fprintf(b, "（默认：%s）", f.DefValue)
		}
		b.WriteByte('\n')
	})
}

// Flag categories are presentation metadata; every flag remains available.
var flagHelpGroups = map[string][]string{
	"常用选项":  {"help", "version", "config", "env-file", "for", "method", "directory", "output", "batch", "json", "quiet", "parallel", "interactive", "non-interactive", "tftp-server", "tftp-root", "script", "script-for", "save", "backup", "stop-after-failures", "command", "commands-file", "remote-server", "remote-path", "remote-username", "remote-protocol", "from", "to"},
	"设备筛选":  {"host", "hosts", "exclude-hosts", "name", "exclude-name", "vendor", "exclude-vendor", "model", "exclude-model", "name-match", "model-match", "all", "sample", "limit"},
	"连接与认证": {"protocol", "username", "password", "enable-password", "ask-password", "netdisco-username", "netdisco-password", "ask-netdisco-password", "known-hosts", "host-key-policy", "stdin-credentials", "jump-host", "connect-retries"},
}

func flagHelpGroup(name string) string {
	for group, names := range flagHelpGroups {
		if slices.Contains(names, name) {
			return group
		}
	}
	return "高级选项"
}
