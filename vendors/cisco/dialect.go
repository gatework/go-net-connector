// Package cisco implements Cisco IOS and NX-OS network device dialects.
package cisco

import (
	"time"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/dialect"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

// IOSDialect is the built-in Cisco IOS platform.
type IOSDialect struct {
	driver.ProfiledPlatform
	driver.TopologyReader
	driver.DescriptionWriter
	iosTFTPPolicy
}

// NXOSDialect is the built-in Cisco NX-OS platform.
type NXOSDialect struct {
	driver.ProfiledPlatform
	driver.TopologyReader
	driver.DescriptionWriter
	nxosTFTPPolicy
}

const neighborCommand = "show cdp neighbors detail"

var writeMode = dialect.WriteMode{Enter: "configure terminal", Leave: "end", InterfaceExit: "exit"}

// IOS returns the built-in Cisco IOS dialect.
func IOS() IOSDialect {
	return IOSDialect{
		ProfiledPlatform:  driver.NewProfiledPlatform(newIOSProfile()),
		TopologyReader:    driver.NewTopologyReader(neighborCommand, iosTopologyParser()),
		DescriptionWriter: driver.NewDescriptionWriter(writeMode),
		iosTFTPPolicy:     newIOSTFTPPolicy(),
	}
}

// NXOS returns the built-in Cisco NX-OS dialect.
func NXOS() NXOSDialect {
	return NXOSDialect{
		ProfiledPlatform:  driver.NewProfiledPlatform(newNXOSProfile()),
		TopologyReader:    driver.NewTopologyReader(neighborCommand, nxosTopologyParser()),
		DescriptionWriter: driver.NewDescriptionWriter(writeMode),
		nxosTFTPPolicy:    newNXOSTFTPPolicy(),
	}
}

var saveConfirmationPattern = driver.Regex(`(?i)^(?:\[OK\]|Copy complete\.)$`)

func newIOSProfile() connector.Profile {
	p := driver.BaseProfile("cisco_ios")
	p.ConfigCommands = []string{"terminal length 0", "show running-config"}
	p.SaveCommands = []string{"copy running-config startup-config"}
	p.LoginPrompt = driver.Pattern(`(?:\A|[\r\n])\S+[>#]\s*\z`)
	p.CommandPrompt = p.LoginPrompt
	p.Pager = driver.Pattern(`(?i)(?:\A|[\r\n])[^\S\r\n]*--More--[^\r\n]*(?:\r|\n|\z)`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*%[ \t]+(?:Invalid|Incomplete|Ambiguous)\s+(?:input|command)\b`, `(?m)^[ \t]*\^`)
	p.Interactions = []connector.Interaction{driver.Interaction(`(?i)Destination\s+filename\s+\[[^\]]+\]\?\s*\z`, "\n"), driver.Interaction(`(?i)Do\s+you\s+want\s+to\s+overwrite\?\s*\[yes/no\]\s*\z`, "yes\n"), driver.Interaction(`(?i)Proceed\s+with\s+copy\?\s*\[confirm\]\s*\z`, "\n")}
	return driver.FinishProfile(p, 60*time.Second, configFactory(), saveConfirmationPattern)
}

func newNXOSProfile() connector.Profile {
	p := driver.BaseProfile("cisco_nxos")
	p.ConfigCommands = []string{"terminal length 0", "show running-config"}
	p.SaveCommands = []string{"copy run start"}
	p.LoginPrompt = driver.Pattern(`\S+[>#]\s*\z`)
	p.CommandPrompt = p.LoginPrompt
	p.Pager = driver.Pattern(`(?i)(?:\A|[\r\n])[ \t]*(?:\x1b\[[0-9;]*[A-Za-z])*--More--(?:[ \t\x00\x08]|\x1b\[[0-9;]*[A-Za-z])*(?:\r|\n|\z)`)
	p.CommandErrors = driver.Regexps(`(?im)^[ \t]*%?[ \t]*(?:(?:Invalid|Ambiguous|Incomplete)\s+command(?:\s+\([^)]+\))?|Invalid\s+parameter\s+detected)\s+at\s+'\^'\s+marker\.`, `(?m)^[ \t]*\^`, `(?im)^[ \t]*%[ \t]+(?:Invalid|Ambiguous|Incomplete)\s+command\b`, `(?im)^[ \t]*%?[ \t]*Invalid\s+parameter\s+detected\b`, `(?im)^[ \t]*syntax\s+error\b`)
	p.Interactions = []connector.Interaction{driver.Interaction(`(?i)Destination\s+filename\s+\[[^\]]+\]\?\s*\z`, "\n"), driver.Interaction(`(?i)Do\s+you\s+want\s+to\s+overwrite\?\s*\[yes/no\]\s*\z`, "yes\n")}
	return driver.FinishProfile(p, 60*time.Second, configFactory(), saveConfirmationPattern)
}
