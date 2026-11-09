package topology

import (
	"errors"

	"github.com/gatework/go-net-connector/vendors/dialect"
)

// ParseNeighbors delegates command-output interpretation to the resolved
// platform dialect. A nil policy represents a platform without this behavior.
func ParseNeighbors(policy NeighborDiscovery, output string) ([]Neighbor, error) {
	if policy == nil {
		return nil, ErrUnsupported
	}
	neighbors, err := policy.ParseNeighbors(output)
	if err != nil {
		return nil, structuredParseError(err, "parse")
	}
	return neighbors, nil
}

// ParseDescriptions delegates configuration interpretation to the resolved
// platform dialect while keeping topology orchestration vendor-neutral.
func ParseDescriptions(policy DescriptionReader, config string) (map[string]string, error) {
	if policy == nil {
		return nil, ErrUnsupported
	}
	descriptions, err := policy.ParseDescriptions(config)
	if err != nil {
		return nil, structuredParseError(err, "parse")
	}
	return descriptions, nil
}

func structuredParseError(err error, fallbackPhase string) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*Error](err); ok {
		return err
	}
	if metadata, ok := errors.AsType[dialect.DiagnosticMetadata](err); ok {
		code, phase := metadata.DiagnosticCode(), metadata.DiagnosticPhase()
		if code != "" && phase != "" {
			return newError(code, phase, err)
		}
	}
	if errors.Is(err, dialect.ErrParse) {
		return newError("unrecognized_output", fallbackPhase, err)
	}
	return err
}
