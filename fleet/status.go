package fleet

type mode string

const (
	modeBackup mode = "backup"
	modeTFTP   mode = "tftp"
)

// Private status types keep workflow decisions closed and typo-resistant while
// exported reports retain their established string values.
type planStatus string

const (
	planStatusInvalidAddress          planStatus = "invalid_address"
	planStatusFiltered                planStatus = "filtered"
	planStatusUnsupportedVendor       planStatus = "unsupported_vendor"
	planStatusDuplicateHost           planStatus = "duplicate_host"
	planStatusUnsupportedOperation    planStatus = "unsupported_operation"
	planStatusSampleLimit             planStatus = "sample_limit"
	planStatusRemoteFilenameCollision planStatus = "remote_filename_collision"
	planStatusReady                   planStatus = "ready"
)
