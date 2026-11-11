package batchreport

// SuccessPolicy decides which device outcomes make a batch successful. Its
// string values are part of the configuration, CLI and report contracts.
type SuccessPolicy string

// Success policies, from strict inventory coverage to verified selected devices.
const (
	PolicyStrict   SuccessPolicy = "strict"
	PolicySelected SuccessPolicy = "selected"
	PolicyVerified SuccessPolicy = "verified"
)

// SuccessPolicies returns every accepted policy in documentation order.
func SuccessPolicies() []string {
	return []string{string(PolicyStrict), string(PolicySelected), string(PolicyVerified)}
}

// Valid reports whether p is a known policy.
func (p SuccessPolicy) Valid() bool {
	return p == PolicyStrict || p == PolicySelected || p == PolicyVerified
}

// AllowedFor reports whether p is known and applicable to the batch method.
// Only TFTP batches have server-side verification evidence.
func (p SuccessPolicy) AllowedFor(tftp bool) bool {
	return p.Valid() && (p != PolicyVerified || tftp)
}

// Layout selects how a batch arranges its output directory.
type Layout string

// Supported batch output layouts.
const (
	LayoutBatch Layout = "batch"
	LayoutFlat  Layout = "flat"
)

// Layouts returns every accepted output layout.
func Layouts() []string { return []string{string(LayoutBatch), string(LayoutFlat)} }

// Valid reports whether l is a known layout.
func (l Layout) Valid() bool { return l == LayoutBatch || l == LayoutFlat }
