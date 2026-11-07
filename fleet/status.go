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

type outcomeStatus string

const (
	outcomeStatusInvalidAddress          outcomeStatus = "invalid_address"
	outcomeStatusFiltered                outcomeStatus = "filtered"
	outcomeStatusUnsupportedVendor       outcomeStatus = "unsupported_vendor"
	outcomeStatusDuplicateHost           outcomeStatus = "duplicate_host"
	outcomeStatusUnsupportedOperation    outcomeStatus = "unsupported_operation"
	outcomeStatusSampleLimit             outcomeStatus = "sample_limit"
	outcomeStatusRemoteFilenameCollision outcomeStatus = "remote_filename_collision"
	outcomeStatusNotStarted              outcomeStatus = "not_started"
	outcomeStatusBackedUp                outcomeStatus = "backed_up"
	outcomeStatusReportedUploaded        outcomeStatus = "reported_uploaded"
	outcomeStatusSavedWithError          outcomeStatus = "saved_with_error"
	outcomeStatusReportedWithError       outcomeStatus = "reported_with_error"
	outcomeStatusMissingCredentials      outcomeStatus = "missing_credentials"
	outcomeStatusFailed                  outcomeStatus = "failed"
	outcomeStatusInterrupted             outcomeStatus = "interrupted"
)

func (s outcomeStatus) succeeded() bool {
	return s == outcomeStatusBackedUp || s == outcomeStatusReportedUploaded
}

func (s outcomeStatus) partiallySucceeded() bool {
	return s == outcomeStatusSavedWithError || s == outcomeStatusReportedWithError
}

func (s outcomeStatus) attempted() bool {
	return s.succeeded() || s.partiallySucceeded() || s == outcomeStatusFailed || s == outcomeStatusInterrupted
}

type batchStatus string

const (
	batchStatusInterrupted batchStatus = "interrupted"
	batchStatusNoDevices   batchStatus = "no_devices"
	batchStatusSucceeded   batchStatus = "succeeded"
	batchStatusIncomplete  batchStatus = "incomplete"
)
