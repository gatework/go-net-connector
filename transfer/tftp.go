package transfer

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/terminal"
)

var ErrInvalid = errors.New("transfer: invalid options")
var ErrUnsupported = errors.New("transfer: unsupported operation")
var ErrUnconfirmed = errors.New("transfer: completion unconfirmed")

type Error struct {
	Code, Phase string
	cause       error
}

func (e *Error) Error() string    { return "transfer: " + e.Code + " (" + e.Phase + ")" }
func (e *Error) Unwrap() error    { return e.cause }
func (e *Error) GoString() string { return e.Error() }
func failure(code string, cause error) error {
	return &Error{Code: code, Phase: "tftp_backup", cause: cause}
}

type Result struct {
	Receipt Receipt
	Steps   []connector.StepResult
}

func (r Result) String() string {
	return fmt.Sprintf("transfer.Result{steps:%d,reported:%t}", len(r.Steps), r.Receipt.Reported())
}
func (r Result) GoString() string { return r.String() }
func reply(pattern, response string) connector.Interaction {
	return connector.Interaction{Pattern: regexp.MustCompile(pattern), Reply: response, Unlimited: true}
}
func TFTP(ctx context.Context, d *connector.Device, o TFTPOptions) (result Result, err error) {
	if d == nil {
		return result, ErrInvalid
	}
	vendor := d.ProfileName()
	if e := Validate(vendor, d.Host(), o); e != nil {
		return result, e
	}
	explicit := o.Path != ""
	explicitSource := o.SourceFile != ""
	err = d.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
		if (vendor == "h3c" || vendor == "h3c_wireless") && o.SourceFile == "" {
			r, e := op.ExecuteCommand(ctx, connector.Command{Text: "display startup"}, connector.ExecuteOptions{})
			result.Steps = append(result.Steps, r.Steps()...)
			if e != nil {
				return e
			}
			output, e := terminal.Render([]byte(r.Output()), true)
			if e != nil {
				return e
			}
			m := regexp.MustCompile(`(?mi)^\s*Next main startup saved-configuration file:\s*(\S+)`).FindStringSubmatch(output)
			if m == nil {
				m = regexp.MustCompile(`(?mi)^\s*Current startup saved-configuration file:\s*(\S+)`).FindStringSubmatch(output)
			}
			if m == nil || strings.EqualFold(m[1], "NULL") {
				return failure("startup_config_missing", ErrUnconfirmed)
			}
			o.SourceFile = strings.TrimSuffix(m[1], "(*)")
			if ValidateSource(o.SourceFile) != nil {
				return ErrInvalid
			}
		}
		if !explicit {
			if vendor == "h3c" || vendor == "h3c_wireless" || vendor == "huawei" {
				o.Path = path.Base(o.SourceFile)
			} else {
				var e error
				o.Path, e = Filename(vendor, d.Host(), "")
				if e != nil {
					return e
				}
			}
			if ValidatePath(o.Path) != nil {
				return ErrInvalid
			}
		}
		command := transferCommand(vendor, d.Host(), o, explicit)
		r, e := op.ExecuteCommand(ctx, command, connector.ExecuteOptions{SkipPrivilege: true})
		result.Steps = append(result.Steps, r.Steps()...)
		lines, outputs, parseErr := evidence(r.Steps())
		if parseErr != nil {
			return parseErr
		}
		for _, output := range outputs {
			if failed(output) {
				return failure("transfer_failed", connector.ErrDevice)
			}
		}
		if !completed(vendor, lines) {
			if e != nil {
				return e
			}
			return failure("transfer_unconfirmed", ErrUnconfirmed)
		}
		result.Receipt = Receipt{ConfigurationKind: "unknown", Format: "unknown", Server: o.Server, Verification: "device_reported", SourceFile: o.SourceFile, CompletedAt: time.Now().UTC()}
		if explicit || vendor != "hillstone" {
			v := o.Path
			result.Receipt.RequestedPath = &v
		}
		actual := o.Path
		if vendor == "hillstone" {
			actual = ""
			for _, line := range lines {
				if m := regexp.MustCompile(`(?i)^Export\s+ok\s*,\s*target\s+file\s+name\s+(\S+)$`).FindStringSubmatch(line); m != nil {
					actual = m[1]
					break
				}
			}
		}
		if vendor == "radware" {
			found := map[string]bool{}
			for _, line := range lines {
				if m := regexp.MustCompile(`(?i)^Current\s+config\s+successfully\s+transferred\s+to\s+([^\s:]+):(\S+)$`).FindStringSubmatch(line); m != nil {
					found[strings.ToLower(m[1])+":"+m[2]] = true
					if !strings.EqualFold(m[1], o.Server) {
						return failure("transfer_path_unconfirmed", ErrUnconfirmed)
					}
					actual = m[2]
				}
			}
			if len(found) > 1 {
				return failure("transfer_path_unconfirmed", ErrUnconfirmed)
			}
		}
		if ValidatePath(actual) != nil {
			return failure("transfer_path_unconfirmed", ErrInvalid)
		}
		result.Receipt.Path = actual
		result.Receipt.ConfigurationKind = "running"
		result.Receipt.Format = "cfg"
		switch vendor {
		case "h3c", "h3c_wireless":
			result.Receipt.ConfigurationKind = "startup"
			if explicitSource {
				result.Receipt.ConfigurationKind = "saved_file"
			}
			result.Receipt.Format = "unknown"
		case "huawei":
			result.Receipt.ConfigurationKind = "saved_file"
			result.Receipt.Format = "unknown"
		case "hillstone":
			result.Receipt.ConfigurationKind = "startup"
			result.Receipt.Format = "dat"
		case "radware":
			result.Receipt.ConfigurationKind = "native_archive"
			result.Receipt.Format = "tgz"
		case "palo_alto":
			result.Receipt.Format = "xml"
			result.Receipt.SourceFile = "running-config.xml"
		}
		if explicit && actual != o.Path {
			return failure("transfer_path_mismatch", ErrUnconfirmed)
		}
		if e != nil {
			return failure("transfer_finalize_failed", e)
		}
		return nil
	})
	return result, err
}
func transferCommand(vendor, deviceHost string, o TFTPOptions, explicit bool) connector.Command {
	timeout := 180 * time.Second
	c := connector.Command{Timeout: &timeout}
	switch vendor {
	case "h3c", "h3c_wireless", "huawei":
		c.Text = "tftp " + o.Server + " put " + o.SourceFile
		if explicit {
			c.Text += " " + o.Path
		}
		if o.SourceInterface == "management" {
			c.Text += " source ip " + deviceHost
		} else if o.SourceInterface != "" {
			m := interfacePattern.FindStringSubmatch(o.SourceInterface)
			c.Text += " source interface " + m[1] + " " + m[2]
		}
	case "cisco_ios":
		c.Text = "copy running-config tftp:"
		c.Interactions = []connector.Interaction{reply(`(?i)Address\s+or\s+name\s+of\s+remote\s+host\s*\[[^\]]*\]\?\s*$`, o.Server+"\n"), reply(`(?i)Destination\s+filename\s*\[[^\]]*\]\?\s*$`, o.Path+"\n")}
	case "cisco_nxos":
		vrf := o.VRF
		if vrf == "" {
			vrf = "management"
		}
		c.Text = "copy running-config tftp://" + o.Server + "/" + o.Path + " vrf " + vrf
		c.Interactions = []connector.Interaction{reply(`(?i)Enter\s+vrf\s*\([^\r\n]*\):\s*$`, vrf+"\n")}
	case "fortinet":
		c.Text = "execute backup full-config tftp " + o.Path + " " + o.Server
	case "centec":
		c.Text = "copy running-config"
		if o.SourceInterface == "management" {
			c.Text += " mgmt-if"
		}
		c.Text += " tftp://" + o.Server + "/" + o.Path
	case "hillstone":
		vrf := o.VRF
		if vrf == "" {
			vrf = "mgt-vr"
		}
		c.Text = "export configuration startup to tftp server " + o.Server + " vrouter " + vrf
		if explicit {
			c.Text += " " + o.Path
		}
	case "palo_alto":
		c.Text = "tftp export configuration to " + o.Server + " from running-config.xml"
	case "radware":
		c.Text = "/cfg/ptcfg " + o.Server + " -tftp"
		if o.SourceInterface == "management" {
			c.Text += " -mgmt"
		} else if o.SourceInterface == "data" {
			c.Text += " -data"
		}
		c.Interactions = []connector.Interaction{reply(`(?i)Enter\s+(?:hostname|IP address)[^\r\n]*:\s*$`, o.Server+"\n"), reply(`(?i)Enter\s+name\s+of\s+(?:\.tgz\s+)?file[^\r\n]*:\s*$`, o.Path+"\n"), reply(`(?i)Enter\s+username[^\r\n]*:\s*$`, "\n"), reply(`(?i)Include\s+private\s+keys\?\s*\[y/n\]:\s*$`, "n\n"), reply(`(?i)Enter\s+"?mansync"?\s+to\s+get\s+real/group/virt\s+internal\s+index\s+config:\s*$`, "mansync\n")}
	}
	return c
}
