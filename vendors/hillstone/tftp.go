package hillstone

import (
	"regexp"

	"github.com/gatework/go-net-connector/connector"
	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

var exportPattern = regexp.MustCompile(`(?i)^Export\s+ok\s*,\s*target\s+file\s+name\s+(\S+)$`)

type tftpPolicy struct{ driver.TFTPBase }

func newTFTPPolicy() tftpPolicy {
	return tftpPolicy{driver.NewTFTPBase(driver.TFTPOptions{Extension: "dat", Kind: "startup", Format: "dat", DefaultVRF: "mgt-vr", AcceptVRF: true, FlatPath: true})}
}
func (h tftpPolicy) Command(_ string, o dialectapi.TFTPOptions, explicitPath bool) connector.Command {
	vrf := o.VRF
	if vrf == "" {
		vrf = h.DefaultVRF()
	}
	command := "export configuration startup to tftp server " + o.Server + " vrouter " + vrf
	if explicitPath {
		command += " " + o.Path
	}
	return driver.CommandWithTimeout(command, o.Timeout)
}
func (tftpPolicy) Completed(lines []string, _ dialectapi.TFTPOptions) (string, bool, error) {
	for _, line := range lines {
		if match := exportPattern.FindStringSubmatch(line); match != nil {
			return match[1], true, nil
		}
	}
	return "", false, nil
}
func (tftpPolicy) Receipt(o dialectapi.TFTPOptions, explicitSource, explicitPath bool) dialectapi.TFTPReceipt {
	receipt := driver.NewTFTPBase(driver.TFTPOptions{Kind: "startup", Format: "dat", Extension: "dat"}).Receipt(o, explicitSource, explicitPath)
	receipt.IncludeRequestedPath = explicitPath
	return receipt
}
