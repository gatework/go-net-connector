// Package netdisco provides bounded inventory sources and their cache.
package netdisco

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gatework/go-net-connector/inventory"
)

type Source interface {
	Devices(context.Context) ([]inventory.Device, error)
}
type SourceFunc func(context.Context) ([]inventory.Device, error)

func (f SourceFunc) Devices(ctx context.Context) ([]inventory.Device, error) { return f(ctx) }

type InventoryError struct {
	Code  string
	cause error
}

func (e *InventoryError) Error() string    { return "netdisco: " + e.Code }
func (e *InventoryError) GoString() string { return e.Error() }
func (e *InventoryError) Unwrap() error    { return e.cause }
func invError(code string, e error) error  { return &InventoryError{code, e} }

type Limits struct {
	PageSize, MaxPages, MaxDevices      int
	MaxResponseBytes, MaxInventoryBytes int64
	Timeout                             time.Duration
}

func (l Limits) normalize() (Limits, error) {
	if l.PageSize < 0 || l.MaxPages < 0 || l.MaxDevices < 0 || l.MaxResponseBytes < 0 || l.MaxInventoryBytes < 0 || l.Timeout < 0 {
		return l, invError("invalid_limits", nil)
	}
	if l.PageSize == 0 {
		l.PageSize = 500
	}
	if l.MaxPages == 0 {
		l.MaxPages = 10000
	}
	if l.MaxDevices == 0 {
		l.MaxDevices = 100000
	}
	if l.MaxResponseBytes == 0 {
		l.MaxResponseBytes = 16 << 20
	}
	if l.MaxInventoryBytes == 0 {
		l.MaxInventoryBytes = 128 << 20
	}
	if l.Timeout == 0 {
		l.Timeout = 300 * time.Second
	}
	return l, nil
}

type HTTPOptions struct {
	URL, Username, Password, APIKey string
	RequireHTTPS                    bool
	Client                          *http.Client
	Limits                          Limits
}
type HTTP struct {
	base                    *url.URL
	username, password, key string
	client                  *http.Client
	limits                  Limits
}

func NewHTTP(o HTTPOptions) (*HTTP, error) {
	u, e := url.Parse(o.URL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (o.RequireHTTPS && u.Scheme != "https") {
		return nil, invError("invalid_url", nil)
	}
	if strings.ContainsAny(o.APIKey, "\r\n\x00") || strings.ContainsAny(o.Username, "\r\n\x00:") {
		return nil, invError("invalid_credentials", nil)
	}
	if o.APIKey == "" && (o.Username == "" || o.Password == "") {
		return nil, invError("missing_credentials", nil)
	}
	limits, e := o.Limits.normalize()
	if e != nil {
		return nil, e
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	}
	ownedClient := *client
	ownedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTP{u, o.Username, o.Password, o.APIKey, &ownedClient, limits}, nil
}
func (h *HTTP) String() string   { return "netdisco.HTTP" }
func (h *HTTP) GoString() string { return h.String() }
func (h *HTTP) Devices(parent context.Context) ([]inventory.Device, error) {
	ctx, cancel := context.WithTimeout(parent, h.limits.Timeout)
	defer cancel()
	var total int64
	count := 0
	request := func(endpoint, method string, query url.Values, key string) ([]byte, int, error) {
		u := *h.base
		u.Path = strings.TrimRight(u.Path, "/") + "/" + endpoint
		u.RawQuery = query.Encode()
		req, e := http.NewRequestWithContext(ctx, method, u.String(), nil)
		if e != nil {
			return nil, 0, invError("request_failed", e)
		}
		req.Header.Set("Accept", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Apikey "+key)
		} else {
			req.SetBasicAuth(h.username, h.password)
		}
		resp, e := h.client.Do(req)
		if e != nil {
			return nil, 0, invError("connection_failed", e)
		}
		defer resp.Body.Close()
		limit := min(h.limits.MaxResponseBytes, h.limits.MaxInventoryBytes-total)
		b, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if e != nil {
			return nil, resp.StatusCode, invError("connection_failed", e)
		}
		total += int64(len(b))
		if total > h.limits.MaxInventoryBytes {
			return nil, resp.StatusCode, invError("max_inventory_bytes", nil)
		}
		if int64(len(b)) > h.limits.MaxResponseBytes {
			return nil, resp.StatusCode, invError("max_response_bytes", nil)
		}
		if e = ctx.Err(); e != nil {
			return nil, 0, invError("inventory_timeout", e)
		}
		return b, resp.StatusCode, nil
	}
	key := h.key
	if key == "" {
		b, status, e := request("login", http.MethodPost, nil, "")
		if e != nil {
			return nil, e
		}
		if status < 200 || status >= 300 {
			return nil, invError("request_failed", nil)
		}
		var data struct {
			APIKey string `json:"api_key"`
		}
		if e = json.Unmarshal(b, &data); e != nil || data.APIKey == "" || strings.ContainsAny(data.APIKey, "\r\n\x00") {
			return nil, invError("authentication_failed", nil)
		}
		key = data.APIKey
	}

	seen := make(map[string]bool)
	unique := make(map[inventory.Device]bool)
	rows := make([]inventory.Device, 0)
	for page := 0; page < h.limits.MaxPages; page++ {
		query := url.Values{"fields": {"ip,name,dns,vendor,os,model,os_ver,serial"}, "limit": {strconv.Itoa(h.limits.PageSize)}, "offset": {strconv.Itoa(page * h.limits.PageSize)}}
		body, status, e := request("api/v1/search/device", http.MethodGet, query, key)
		if e != nil {
			return nil, e
		}
		if status == 400 {
			var message struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(body, &message) == nil && message.Error == "Missing query" {
				body, status, e = request("api/v1/search/device", http.MethodGet, url.Values{"q": {"%"}, "seeallcolumns": {"true"}}, key)
				if e != nil {
					return nil, e
				}
				if status < 200 || status >= 300 {
					return nil, invError("request_failed", nil)
				}
				data, e := decodeHTTPInventory(ctx, body, h.limits.MaxDevices, &count)
				if e != nil {
					return nil, e
				}
				return dedupContext(ctx, data)
			}
		}
		if status < 200 || status >= 300 {
			return nil, invError("request_failed", fmt.Errorf("HTTP %d", status))
		}
		data, e := decodeHTTPInventory(ctx, body, h.limits.MaxDevices, &count)
		if e != nil {
			return nil, e
		}
		if len(data) == 0 {
			if e := ctx.Err(); e != nil {
				return nil, invError("inventory_timeout", e)
			}
			return rows, nil
		}
		advances := false
		for _, row := range data {
			if e := ctx.Err(); e != nil {
				return nil, invError("inventory_timeout", e)
			}
			if !seen[row.IP] {
				advances = true
			}
			seen[row.IP] = true
			if !unique[row] {
				unique[row] = true
				rows = append(rows, row)
			}
		}
		if !advances {
			return nil, invError("pagination_stalled", nil)
		}
	}
	return nil, invError("max_pages", nil)
}
func dedupContext(ctx context.Context, rows []inventory.Device) ([]inventory.Device, error) {
	seen := make(map[inventory.Device]bool)
	out := make([]inventory.Device, 0, len(rows))
	for _, r := range rows {
		if e := ctx.Err(); e != nil {
			return nil, invError("inventory_timeout", e)
		}
		if !seen[r] {
			out = append(out, r)
			seen[r] = true
		}
	}
	if e := ctx.Err(); e != nil {
		return nil, invError("inventory_timeout", e)
	}
	return out, nil
}

var inventoryFieldNames = [...]string{"ip", "name", "dns", "vendor", "os", "model", "os_ver", "serial"}

func inventoryFields(d *inventory.Device) [8]*string {
	return [8]*string{&d.IP, &d.Name, &d.DNS, &d.Vendor, &d.OS, &d.Model, &d.OSVersion, &d.Serial}
}

func decodeHTTPInventory(ctx context.Context, b []byte, maxDevices int, count *int) ([]inventory.Device, error) {
	if e := ctx.Err(); e != nil {
		return nil, invError("inventory_timeout", e)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return []inventory.Device{}, nil
	}
	if !utf8.Valid(b) {
		return nil, invError("invalid_json", nil)
	}
	// Raw-message maps keep acceptance case-sensitive and retain JSON's last-key-wins behavior.
	var data []map[string]json.RawMessage
	if e := json.Unmarshal(b, &data); e != nil || data == nil {
		return nil, invError("invalid_inventory", e)
	}
	*count += len(data)
	if *count > maxDevices {
		return nil, invError("max_devices", nil)
	}
	rows := make([]inventory.Device, len(data))
	for i, row := range data {
		if e := ctx.Err(); e != nil {
			return nil, invError("inventory_timeout", e)
		}
		if row == nil {
			return nil, invError("invalid_inventory", nil)
		}
		fields := inventoryFields(&rows[i])
		for j, name := range inventoryFieldNames {
			if e := ctx.Err(); e != nil {
				return nil, invError("inventory_timeout", e)
			}
			value, ok := row[name]
			if !ok {
				continue
			}
			// Unmarshaling null into an empty string preserves optional null-as-empty fields.
			if e := json.Unmarshal(value, fields[j]); e != nil {
				return nil, invError("invalid_inventory", nil)
			}
		}
		if strings.TrimSpace(rows[i].IP) == "" {
			return nil, invError("invalid_inventory", nil)
		}
	}
	if e := ctx.Err(); e != nil {
		return nil, invError("inventory_timeout", e)
	}
	return rows, nil
}

func errorCode(e error) string {
	var ie *InventoryError
	if errors.As(e, &ie) {
		return ie.Code
	}
	if errors.Is(e, context.DeadlineExceeded) {
		return "inventory_timeout"
	}
	return "inventory_error"
}

// MarshalJSON exposes operational metadata only; credentials remain available explicitly on the options.
func (o HTTPOptions) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		URL, Username string
		RequireHTTPS  bool
		Limits        Limits
	}{safeOptionURL(o.URL), o.Username, o.RequireHTTPS, o.Limits})
}
func (o HTTPOptions) String() string   { b, _ := o.MarshalJSON(); return "HTTPOptions" + string(b) }
func (o HTTPOptions) GoString() string { return o.String() }

func safeOptionURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "[invalid URL]"
	}
	return u.Scheme + "://" + u.Host
}
