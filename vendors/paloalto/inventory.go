package paloalto

import (
	"regexp"

	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
)

var labelPattern = regexp.MustCompile(`palo\s*alto|paloalto`)

func (dialect) MatchInventory(identity dialectapi.InventoryIdentity) bool {
	return labelPattern.MatchString(identity.Vendor)
}
