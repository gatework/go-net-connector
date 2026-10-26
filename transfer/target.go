package transfer

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/gatework/go-net-connector/vendors"
)

type TFTPOptions struct{ Server, Path, SourceFile, VRF, SourceInterface string }

var serverPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
var pathPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)
var sourcePattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)
var interfacePattern = regexp.MustCompile(`^([A-Za-z][A-Za-z-]*) ?([0-9][0-9/.:]*)$`)
var vrfPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func ValidateServer(host string) error {
	if len(host) > 253 || !serverPattern.MatchString(host) || strings.Contains(host, "..") {
		return ErrInvalid
	}
	return nil
}
func ValidatePath(path string) error {
	if len(path) > 220 || !pathPattern.MatchString(path) {
		return ErrInvalid
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return ErrInvalid
		}
	}
	return nil
}
func ValidateSource(source string) error {
	if !sourcePattern.MatchString(source) {
		return ErrInvalid
	}
	for _, part := range strings.Split(source, "/") {
		if part == ".." {
			return ErrInvalid
		}
	}
	return nil
}
func Validate(vendor, deviceHost string, o TFTPOptions) error {
	p, e := vendors.Profile(vendor)
	if e != nil || !p.Capabilities.TFTP {
		return ErrUnsupported
	}
	if ValidateServer(o.Server) != nil || o.Path != "" && ValidatePath(o.Path) != nil || o.SourceFile != "" && ValidateSource(o.SourceFile) != nil || o.VRF != "" && !vrfPattern.MatchString(o.VRF) {
		return ErrInvalid
	}
	if o.SourceFile != "" && vendor != "h3c" && vendor != "h3c_wireless" && vendor != "huawei" {
		return ErrInvalid
	}
	if vendor == "huawei" && o.SourceFile == "" {
		return ErrInvalid
	}
	if o.VRF != "" && vendor != "cisco_nxos" && vendor != "hillstone" {
		return ErrInvalid
	}
	if vendor == "hillstone" && strings.Contains(o.Path, "/") {
		return ErrInvalid
	}
	if vendor == "palo_alto" && o.Path != "" && o.Path != "running-config.xml" {
		return ErrInvalid
	}
	if o.SourceInterface != "" {
		switch vendor {
		case "h3c", "h3c_wireless":
			if o.SourceInterface == "management" {
				a, e := netip.ParseAddr(deviceHost)
				if e != nil || !a.Is4() {
					return ErrInvalid
				}
			} else if !interfacePattern.MatchString(o.SourceInterface) {
				return ErrInvalid
			}
		case "radware", "centec":
			if o.SourceInterface != "management" && o.SourceInterface != "data" {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}
func Filename(vendor, host, label string) (string, error) {
	p, e := vendors.Profile(vendor)
	if e != nil || !p.Capabilities.TFTP {
		return "", ErrUnsupported
	}
	if vendor == "palo_alto" {
		return "running-config.xml", nil
	}
	extension := "cfg"
	if vendor == "hillstone" {
		extension = "dat"
	}
	if vendor == "radware" {
		extension = "tgz"
	}
	address := regexp.MustCompile(`[^A-Za-z0-9._-]`).ReplaceAllString(host, "_")
	suffix := address + "." + extension
	if len(suffix) > 218 {
		suffix = fmt.Sprintf("%x.%s", sha256.Sum256([]byte(host)), extension)
	}
	name := suffix
	if label != "" {
		for _, c := range label {
			if c > 127 {
				return "", ErrInvalid
			}
		}
		if max := 220 - len(suffix) - 1; len(label) > max {
			label = label[:max]
		}
		name = label + "-" + suffix
	}
	return name, ValidatePath(name)
}
func InventoryFilename(vendor, host, name string) (string, error) {
	label := regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(name, "-")
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
	return Filename(vendor, host, label)
}
