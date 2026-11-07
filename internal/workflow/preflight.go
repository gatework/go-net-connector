package workflow

import "errors"

// ErrTFTPArchivePreflight identifies preparation failures before any device upload.
var ErrTFTPArchivePreflight = errors.New("TFTP archive preflight failed")
