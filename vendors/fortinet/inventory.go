package fortinet

import (
	"regexp"

	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
)

var (
	labelPattern   = regexp.MustCompile(`\bfortinet\b|\bfortigate\b`)
	fortigateLabel = regexp.MustCompile(`\bfortigate\b`)
	fortigateModel = regexp.MustCompile(`^(fortigate|fgt)\s*\d`)
)

// MatchInventory reports whether an inventory identity belongs to this platform.
func (Dialect) MatchInventory(identity dialectapi.InventoryIdentity) bool {
	return labelPattern.MatchString(identity.Vendor) &&
		(identity.OS == "fortios" || fortigateLabel.MatchString(identity.Vendor) || fortigateModel.MatchString(identity.Model))
}
