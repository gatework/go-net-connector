package archive

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// Request describes one archive protocol action and its bounded phase budgets.
type Request struct {
	Version     int            `json:"version"`
	Timeouts    *TimeoutBudget `json:"timeouts,omitempty"`
	Action      string         `json:"action"`
	Root        string         `json:"root"`
	Filename    string         `json:"filename"`
	ArchiveName string         `json:"archive_name"`
	Batch       string         `json:"batch"`
	SHA256      string         `json:"sha256"`
}
type frame struct {
	OK           bool     `json:"ok"`
	Phase        string   `json:"phase,omitempty"`
	Error        string   `json:"error,omitempty"`
	Version      int      `json:"version,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Bytes        int64    `json:"bytes,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
	ArchivePath  string   `json:"archive_path,omitempty"`
}

func readRequest(reader *bufio.Reader) (Request, error) {
	var req Request
	line, e := reader.ReadSlice('\n')
	if e != nil || len(line) > ControlLimit {
		return req, ErrArchive
	}
	if json.Unmarshal(line, &req) != nil || req.Version != Version {
		return req, ErrArchive
	}
	return req, nil
}
func writeFrame(writer io.Writer, message frame) error {
	message.Version = Version
	b, e := json.Marshal(message)
	if e != nil {
		return ErrArchive
	}
	b = append(b, '\n')
	if len(b) > ControlLimit {
		return ErrArchive
	}
	return writeAll(writer, b)
}
func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, e := writer.Write(data)
		if n < 0 || n > len(data) {
			return ErrArchive
		}
		data = data[n:]
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
func isSafeName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\r\n\x00") && len(s) <= 255
}
