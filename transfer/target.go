package transfer

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// TFTPOptions configures one device upload.
type TFTPOptions = dialect.TFTPOptions

var filenameLabel = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
var serverPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
var pathPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)
var sourcePattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)
var vrfPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// ValidateServer accepts a TFTP server address or host name.
func ValidateServer(host string) error {
	if len(host) > 253 || !serverPattern.MatchString(host) || strings.Contains(host, "..") {
		return ErrInvalid
	}
	return nil
}

// ValidatePath accepts a relative remote path of safe segments.
func ValidatePath(path string) error {
	if len(path) > 220 || !pathPattern.MatchString(path) {
		return ErrInvalid
	}
	for part := range strings.SplitSeq(path, "/") {
		if part == "." || part == ".." {
			return ErrInvalid
		}
	}
	return nil
}

// ValidateSource accepts a device file path of safe characters.
func ValidateSource(source string) error {
	if !sourcePattern.MatchString(source) {
		return ErrInvalid
	}
	if slices.Contains(strings.Split(source, "/"), "..") {
		return ErrInvalid
	}
	return nil
}

// Validate checks o for vendor's TFTP platform and device host.
func Validate(registry dialect.Registry, vendor, deviceHost string, o TFTPOptions) error {
	if o.Timeout < 0 {
		return ErrInvalid
	}
	if registry == nil {
		return ErrUnsupported
	}
	d, e := registry.Lookup(vendor)
	if e != nil {
		return ErrUnsupported
	}
	tftp, ok := d.(TFTPPolicy)
	if !ok {
		return ErrUnsupported
	}
	if ValidateServer(o.Server) != nil || o.Path != "" && ValidatePath(o.Path) != nil || o.SourceFile != "" && ValidateSource(o.SourceFile) != nil || o.VRF != "" && !vrfPattern.MatchString(o.VRF) {
		return ErrInvalid
	}
	if tftp.RequiresSourceFile() && o.SourceFile == "" {
		return errors.Join(ErrInvalid, ErrSourceFileRequired)
	}
	if err := tftp.ValidateOptions(deviceHost, o); err != nil {
		if errors.Is(err, connector.ErrUnsupported) {
			return ErrUnsupported
		}
		return ErrInvalid
	}
	return nil
}

// Filename returns the platform's remote file name for host and label.
func Filename(registry dialect.Registry, vendor, host, label string) (string, error) {
	if registry == nil {
		return "", ErrUnsupported
	}
	d, e := registry.Lookup(vendor)
	if e != nil {
		return "", ErrUnsupported
	}
	tftp, ok := d.(TFTPPolicy)
	if !ok {
		return "", ErrUnsupported
	}
	name, err := tftp.Filename(host, label)
	if err != nil || ValidatePath(name) != nil {
		return "", ErrInvalid
	}
	return name, nil
}

// InventoryFilename returns the remote file name for host labeled by its
// inventory name, or by vendor when the name is unusable.
func InventoryFilename(registry dialect.Registry, vendor, host, name string) (string, error) {
	label := filenameLabel.ReplaceAllString(name, "-")
	if len(label) > 180 {
		label = label[:180]
	}
	label = strings.Trim(label, "._-")
	if label == "" {
		label = vendor
		if label == "" {
			label = "device"
		}
	}
	return Filename(registry, vendor, host, label)
}
