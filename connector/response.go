package connector

import (
	"encoding/json"
	"fmt"

	"github.com/gatework/go-net-connector/internal/terminal"
	"github.com/gatework/go-net-connector/internal/textcodec"
)

// Response is an immutable snapshot of received bytes and verified text.
// A completed prompt does not imply acceptance, valid encoding, or persistence.
// Text accessors never decode again; unavailable text returns ErrIncomplete.
// The zero value contains no received evidence. Formatting exposes only metadata.
type Response struct{ data *responseData }

// Only the receiving operation can populate data, before exposing a snapshot.
// Steps, attempts, and callback receipts subsequently share it without mutation.
type responseData struct {
	raw, rawPrompt, output, rendered, prompt, encoding string
	outputErr, renderErr, promptErr                    error
}

func (r Response) Raw() string {
	if r.data == nil {
		return ""
	}
	return r.data.raw
}
func (r Response) RawPrompt() string {
	if r.data == nil {
		return ""
	}
	return r.data.rawPrompt
}
func (r Response) Encoding() string {
	if r.data == nil {
		return ""
	}
	return r.data.encoding
}
func (r Response) Output() (string, error) {
	if r.data == nil {
		return "", ErrIncomplete
	}
	if r.data.outputErr != nil {
		return "", r.data.outputErr
	}
	return r.data.output, nil
}
func (r Response) Rendered() (string, error) {
	if r.data == nil {
		return "", ErrIncomplete
	}
	return r.data.rendered, r.data.renderErr
}
func (r Response) Prompt() (string, error) {
	if r.data == nil {
		return "", ErrIncomplete
	}
	return r.data.prompt, r.data.promptErr
}
func (r Response) String() string   { return fmt.Sprintf("connector.Response{bytes:%d}", len(r.Raw())) }
func (r Response) GoString() string { return r.String() }
func (r Response) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Bytes int `json:"bytes"`
	}{len(r.Raw())})
}

// convert verifies the complete response once, before command acceptance checks.
// diagnostic preserves decoded text even where terminal edits erase a device error.
func (r *responseData) convert() (diagnostic string, err error) {
	wire := r.output
	output, encoding, err := textcodec.Decode(wire, "auto")
	if err != nil {
		r.outputErr, r.renderErr, r.promptErr = err, err, err
		return "", err
	}
	r.encoding = encoding
	diagnostic = output
	if encoding != "utf-8" {
		output, err = terminal.RenderEncoded([]byte(wire), true, encoding)
		if err != nil {
			r.outputErr, r.renderErr, r.promptErr = err, err, err
			return "", err
		}
	}
	r.output, r.outputErr = output, nil
	r.rendered, r.renderErr = terminal.RenderString(output, true)
	if encoding == "utf-8" {
		r.prompt, r.promptErr = terminal.RenderString(r.rawPrompt, true)
	} else {
		r.prompt, r.promptErr = terminal.RenderEncoded([]byte(r.rawPrompt), true, encoding)
	}
	if r.renderErr != nil {
		return diagnostic, r.renderErr
	}
	return diagnostic, r.promptErr
}
