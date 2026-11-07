package paloalto

import (
	"regexp"

	"github.com/gatework/go-net-connector/connector"
	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

var completionPattern = regexp.MustCompile(`(?i)^Sent\s+[1-9]\d*\s+bytes(?:\s+in\s+[\d.]+\s+(?:secs?|seconds?))?(?:\s+\[\d+(?:\.\d+)?\s+bit/s\])?[.!]?$`)

type tftpPolicy struct{ driver.TFTPBase }

func newTFTPPolicy() tftpPolicy {
	return tftpPolicy{driver.NewTFTPBase(driver.TFTPOptions{FixedPath: "running-config.xml", Kind: "running", Format: "xml", Extension: "xml", AllowDestinationReuse: true, RequireVerificationRoot: true})}
}
func (p tftpPolicy) Command(_ string, o dialectapi.TFTPOptions, _ bool) connector.Command {
	return driver.CommandWithTimeout("tftp export configuration to "+o.Server+" from running-config.xml", o.Timeout)
}
func (tftpPolicy) Completed(lines []string, _ dialectapi.TFTPOptions) (string, bool, error) {
	return "", driver.AnyMatch(lines, completionPattern), nil
}
func (tftpPolicy) Receipt(_ dialectapi.TFTPOptions, _, _ bool) dialectapi.TFTPReceipt {
	return dialectapi.TFTPReceipt{ConfigurationKind: "running", Format: "xml", SourceFile: "running-config.xml", IncludeRequestedPath: true}
}
