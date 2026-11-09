package h3c

import (
	"regexp"

	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
)

var (
	wirelessOSPattern    = regexp.MustCompile(`wireless|wlan`)
	wirelessModelPattern = regexp.MustCompile(`^(h3c\s+)?(wx|ac)\s*\d`)
	modelPattern         = regexp.MustCompile(`^h3c\s+\S`)
	labelPattern         = regexp.MustCompile(`\bh3c\b`)
)

// MatchInventory reports whether an inventory identity belongs to H3C Comware.
func (Dialect) MatchInventory(identity dialectapi.InventoryIdentity) bool {
	return labelPattern.MatchString(identity.Vendor) ||
		identity.OS == "comware" && modelPattern.MatchString(identity.Model)
}

// MatchInventory reports whether an H3C identity is a wireless controller.
func (w WirelessDialect) MatchInventory(identity dialectapi.InventoryIdentity) bool {
	return w.Dialect.MatchInventory(identity) && (wirelessOSPattern.MatchString(identity.OS) || wirelessModelPattern.MatchString(identity.Model))
}
