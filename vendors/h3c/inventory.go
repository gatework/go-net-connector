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

func (dialect) MatchInventory(identity dialectapi.InventoryIdentity) bool {
	return labelPattern.MatchString(identity.Vendor) ||
		identity.OS == "comware" && modelPattern.MatchString(identity.Model)
}

func (wirelessDialect) MatchInventory(identity dialectapi.InventoryIdentity) bool {
	isH3C := labelPattern.MatchString(identity.Vendor) ||
		identity.OS == "comware" && modelPattern.MatchString(identity.Model)
	return isH3C && (wirelessOSPattern.MatchString(identity.OS) || wirelessModelPattern.MatchString(identity.Model))
}
