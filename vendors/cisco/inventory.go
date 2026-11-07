package cisco

import (
	"regexp"

	"github.com/gatework/go-net-connector/vendors/dialect"
)

var (
	labelPattern = regexp.MustCompile(`\bcisco\b`)
	nxOS         = regexp.MustCompile(`nx\s*os|nexus`)
	nexusModel   = regexp.MustCompile(`\bnexus\b|\bn\s*[3579]k\b`)
	iosOS        = regexp.MustCompile(`\bios\b|\bios\s*xe\b`)
)

func (nxosDialect) MatchInventory(identity dialect.InventoryIdentity) bool {
	return labelPattern.MatchString(identity.Vendor) &&
		(nxOS.MatchString(identity.OS) || nexusModel.MatchString(identity.Model))
}

func (iosDialect) MatchInventory(identity dialect.InventoryIdentity) bool {
	return labelPattern.MatchString(identity.Vendor) && iosOS.MatchString(identity.OS)
}
