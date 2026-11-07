package fortinet

import (
	"regexp"

	"github.com/gatework/go-net-connector/connector"
	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

var completionPattern = regexp.MustCompile(`(?i)^Send config file to tftp server OK\.?$`)

type tftpPolicy struct{ driver.TFTPBase }

func newTFTPPolicy() tftpPolicy {
	return tftpPolicy{driver.NewTFTPBase(driver.TFTPOptions{Extension: "cfg", Kind: "running", Format: "cfg"})}
}
func (f tftpPolicy) Command(_ string, o dialectapi.TFTPOptions, _ bool) connector.Command {
	return driver.CommandWithTimeout("execute backup full-config tftp "+o.Path+" "+o.Server, o.Timeout)
}
func (tftpPolicy) Completed(lines []string, _ dialectapi.TFTPOptions) (string, bool, error) {
	return "", driver.AnyMatch(lines, completionPattern), nil
}
