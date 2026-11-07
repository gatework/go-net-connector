package centec

import (
	"regexp"

	"github.com/gatework/go-net-connector/connector"
	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

var completionPattern = regexp.MustCompile(`(?i)^Sent\s+[1-9]\d*\s+bytes\s+in\s+[\d.]+\s+seconds?[.!]?$`)

type tftpPolicy struct{ driver.TFTPBase }

func newTFTPPolicy() tftpPolicy {
	return tftpPolicy{driver.NewTFTPBase(driver.TFTPOptions{Extension: "cfg", Kind: "running", Format: "cfg", ValidateSourceInterface: driver.ValidateManagementOrDataSource})}
}
func (c tftpPolicy) Command(_ string, o dialectapi.TFTPOptions, _ bool) connector.Command {
	command := "copy running-config"
	if o.SourceInterface == "management" {
		command += " mgmt-if"
	}
	command += " tftp://" + o.Server + "/" + o.Path
	return driver.CommandWithTimeout(command, o.Timeout)
}
func (tftpPolicy) Completed(lines []string, _ dialectapi.TFTPOptions) (string, bool, error) {
	return "", driver.AnyMatch(lines, completionPattern), nil
}
