package driver

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"time"

	expect "github.com/gatework/go-expect"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/dialect"
)

// TFTPOptions declares a platform's fixed transfer rules.
type TFTPOptions struct {
	Extension, Kind, Format, FixedPath, DefaultVRF string
	AcceptSourceFile, RequireSourceFile            bool
	AcceptVRF, FlatPath                            bool
	AllowDestinationReuse, RequireVerificationRoot bool
	ValidateSourceInterface                        func(deviceHost, sourceInterface string) error
}

// TFTPBase implements transfer validation and common filename/receipt rules.
type TFTPBase struct {
	extension, kind, format, fixedPath  string
	defaultVRF                          string
	acceptSourceFile, requireSourceFile bool
	acceptVRF, flatPath                 bool
	allowDestinationReuse               bool
	requireVerificationRoot             bool
	validateSourceInterface             func(deviceHost, sourceInterface string) error
}

// NewTFTPBase constructs the shared portion of a platform's transfer policy.
func NewTFTPBase(options TFTPOptions) TFTPBase {
	return TFTPBase{
		extension: options.Extension, kind: options.Kind, format: options.Format, fixedPath: options.FixedPath,
		defaultVRF: options.DefaultVRF, acceptSourceFile: options.AcceptSourceFile, requireSourceFile: options.RequireSourceFile,
		acceptVRF: options.AcceptVRF, flatPath: options.FlatPath, allowDestinationReuse: options.AllowDestinationReuse,
		requireVerificationRoot: options.RequireVerificationRoot, validateSourceInterface: options.ValidateSourceInterface,
	}
}

var (
	deviceFilePattern      = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)
	deviceInterfacePattern = regexp.MustCompile(`^([A-Za-z][A-Za-z-]*) ?([0-9][0-9/.:]*)$`)
	deviceVRFPattern       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	deviceFilenameAddress  = regexp.MustCompile(`[^A-Za-z0-9._-]`)
	startupNext            = regexp.MustCompile(`(?mi)^\s*Next main startup saved-configuration file:\s*(\S+)`)
	startupCurrent         = regexp.MustCompile(`(?mi)^\s*Current startup saved-configuration file:\s*(\S+)`)
	fileCompletion         = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(?:File\s+)?(?:transfer(?:red)?|upload(?:ed)?)(?:\s+(?:is|was))?\s+(?:success(?:ful(?:ly)?)?|succeeded|complete(?:d)?(?:\s+successfully)?)[.!]?$`),
		regexp.MustCompile(`(?i)^(?:TFTP:\s*)?[1-9]\d*\s+bytes?\s+sent(?:\s+in\s+[\d.]+\s+(?:secs?|seconds?|second\(s\)))?[.!]?$`),
	}
	fileProgress = regexp.MustCompile(`(?i)^100\s+([1-9]\d*(?:\.\d+)?[kMGT]?)\s+0\s+0\s+100\s+([1-9]\d*(?:\.\d+)?[kMGT]?)(?:\s+[\d.:kMGT-]+)*$`)
)

// ParseDeviceInterface separates a conventional interface type and number.
func ParseDeviceInterface(value string) (kind, number string, ok bool) {
	match := deviceInterfacePattern.FindStringSubmatch(value)
	if len(match) != 3 {
		return "", "", false
	}
	return match[1], match[2], true
}

// ValidateManagementOrDataSource accepts the two named source interfaces used
// by platforms that distinguish management and data planes.
func ValidateManagementOrDataSource(_ string, source string) error {
	if source != "management" && source != "data" {
		return connector.ErrInvalid
	}
	return nil
}

// ValidateOptions checks the source, VRF and destination constraints declared
// by a platform before any device command is sent.
func (b TFTPBase) ValidateOptions(deviceHost string, options dialect.TFTPOptions) error {
	if options.SourceFile != "" && (!b.acceptSourceFile || !deviceFilePattern.MatchString(options.SourceFile) || strings.Contains(options.SourceFile, "..")) {
		return connector.ErrInvalid
	}
	if b.requireSourceFile && options.SourceFile == "" {
		return connector.ErrInvalid
	}
	if options.VRF != "" && (!b.acceptVRF || !deviceVRFPattern.MatchString(options.VRF)) {
		return connector.ErrInvalid
	}
	if b.flatPath && strings.Contains(options.Path, "/") {
		return connector.ErrInvalid
	}
	if b.fixedPath != "" && options.Path != "" && options.Path != b.fixedPath {
		return connector.ErrInvalid
	}
	if options.SourceInterface == "" {
		return nil
	}
	if b.validateSourceInterface == nil {
		return connector.ErrInvalid
	}
	return b.validateSourceInterface(deviceHost, options.SourceInterface)
}

// DefaultVRF returns the platform's preferred transfer routing instance.
func (b TFTPBase) DefaultVRF() string { return b.defaultVRF }

// AllowsConcurrentDestinationReuse reports whether a fixed remote filename is reusable.
func (b TFTPBase) AllowsConcurrentDestinationReuse() bool { return b.allowDestinationReuse }

// RequiresSourceFile reports whether the source file must be resolved explicitly.
func (b TFTPBase) RequiresSourceFile() bool { return b.requireSourceFile }

// RequiresVerificationRoot reports whether server-side archive verification is mandatory.
func (b TFTPBase) RequiresVerificationRoot() bool { return b.requireVerificationRoot }

// Filename returns a safe default remote filename for this platform.
func (b TFTPBase) Filename(host, label string) (string, error) {
	if b.fixedPath != "" {
		return b.fixedPath, nil
	}
	address := deviceFilenameAddress.ReplaceAllString(host, "_")
	suffix := address + "." + b.extension
	if len(suffix) > 218 {
		suffix = fmt.Sprintf("%x.%s", sha256.Sum256([]byte(host)), b.extension)
	}
	name := suffix
	if label != "" {
		for _, character := range label {
			if character > 127 {
				return "", connector.ErrInvalid
			}
		}
		if maximum := 220 - len(suffix) - 1; len(label) > maximum {
			label = label[:maximum]
		}
		name = label + "-" + suffix
	}
	return name, nil
}

// SourceFileCommand is empty for platforms that do not discover a source file.
func (b TFTPBase) SourceFileCommand() string { return "" }

// ResolveSourceFile reports unsupported when no source-file discovery policy is set.
func (b TFTPBase) ResolveSourceFile(string) (string, error) { return "", dialect.ErrUnsupported }

// Receipt returns the common archive metadata for this platform.
func (b TFTPBase) Receipt(options dialect.TFTPOptions, explicitSource, _ bool) dialect.TFTPReceipt {
	receipt := dialect.TFTPReceipt{ConfigurationKind: b.kind, Format: b.format, SourceFile: options.SourceFile, IncludeRequestedPath: true}
	if explicitSource && b.acceptSourceFile {
		receipt.ConfigurationKind = "saved_file"
	}
	return receipt
}

// CommandWithTimeout applies a per-operation timeout when one is configured.
func CommandWithTimeout(text string, timeout time.Duration) connector.Command {
	command := connector.Command{Text: text}
	command.Timeout = TimeoutPointer(timeout)
	return command
}

// TimeoutPointer returns nil for an unspecified timeout.
func TimeoutPointer(timeout time.Duration) *time.Duration {
	if timeout <= 0 {
		return nil
	}
	return &timeout
}

// TransferReply compiles a TFTP prompt and its automatic response.
func TransferReply(expression, response string) connector.Interaction {
	pattern, err := expect.Compile(expression)
	if err != nil {
		panic(err)
	}
	return connector.Interaction{Pattern: pattern, Reply: response, Unlimited: true}
}

// AnyMatch reports whether any response line matches a compiled completion rule.
func AnyMatch(lines []string, pattern *regexp.Regexp) bool {
	for _, line := range lines {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

// FileTransferred recognizes generic positive byte-count and completion output.
func FileTransferred(lines []string) bool {
	for _, line := range lines {
		for _, pattern := range fileCompletion {
			if pattern.MatchString(line) {
				return true
			}
		}
		if match := fileProgress.FindStringSubmatch(line); match != nil && match[1] == match[2] {
			return true
		}
	}
	return false
}
