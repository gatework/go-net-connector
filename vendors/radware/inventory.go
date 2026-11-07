package radware

import (
	"regexp"

	dialectapi "github.com/gatework/go-net-connector/vendors/dialect"
)

var labelPattern = regexp.MustCompile(`\bradware\b`)

func (dialect) MatchInventory(identity dialectapi.InventoryIdentity) bool {
	return labelPattern.MatchString(identity.Vendor)
}
