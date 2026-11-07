package dialect

import (
	"regexp"
	"strings"
)

var unknownNeighbor = regexp.MustCompile(`(?i)^(?:-+|n/?a|unknown|none|null|not\s+(?:available|advertised))?$`)

func (n Neighbor) IdentityIssue() string {
	if unknownNeighbor.MatchString(strings.TrimSpace(n.NeighborName)) {
		return "unknown_neighbor_name"
	}
	if unknownNeighbor.MatchString(strings.TrimSpace(n.NeighborInterface)) {
		return "unknown_neighbor_interface"
	}
	return ""
}
