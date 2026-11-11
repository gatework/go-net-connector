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
	Devices(ctx context.Context) ([]inventory.Device, error)
}
type SourceFunc func(context.Context) ([]inventory.Device, error)

func (f SourceFunc) Devices(ctx context.Context) ([]inventory.Device, error) { return f(ctx) }

type InventoryError struct {
	Code  string
	cause error
}

func (e *InventoryError) Error() string            { return "netdisco: " + e.Code }
func (e *InventoryError) GoString() string         { return e.Error() }
func (e *InventoryError) Unwrap() error            { return e.cause }
func newInventoryError(code string, e error) error { return &InventoryError{code, e} }

type Limits struct {
	PageSize, MaxPages, MaxDevices      int
	MaxResponseBytes, MaxInventoryBytes int64
	Timeout                             time.Duration
}

func (l Limits) normalize() (Limits, error) {
	if l.PageSize < 0 || l.MaxPages < 0 || l.MaxDevices < 0 || l.MaxResponseBytes < 0 || l.MaxInventoryBytes < 0 || l.Timeout < 0 {
		return l, newInventoryError("invalid_limits", nil)
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
	ownedTransport          *http.Transport
}

func NewHTTP(o HTTPOptions) (*HTTP, error) {
	u, limits, e := httpSettings(o, true)
	if e != nil {
		return nil, e
	}
	client := o.Client
	var transport *http.Transport
	if client == nil {
		// A custom DefaultTransport is borrowed: only a transport we cloned is ours.
		client = &http.Client{Transport: http.DefaultTransport}
		if standard, ok := http.DefaultTransport.(*http.Transport); ok {
			transport = standard.Clone()
			client.Transport = transport
		}
	}
	ownedClient := *client
	ownedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTP{base: u, username: o.Username, password: o.Password, key: o.APIKey, client: &ownedClient, limits: limits, ownedTransport: transport}, nil
}

func httpSettings(o HTTPOptions, requirePassword bool) (*url.URL, Limits, error) {
	u, e := url.Parse(o.URL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (o.RequireHTTPS && u.Scheme != "https") {
		return nil, Limits{}, newInventoryError("invalid_url", nil)
	}
	if strings.ContainsAny(o.APIKey, "\r\n\x00") || strings.ContainsAny(o.Username, "\r\n\x00:") {
		return nil, Limits{}, newInventoryError("invalid_credentials", nil)
	}
	if o.APIKey == "" && (o.Username == "" || requirePassword && o.Password == "") {
		return nil, Limits{}, newInventoryError("missing_credentials", nil)
	}
	limits, e := o.Limits.normalize()
	if e != nil {
		return nil, Limits{}, e
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), canonicalHost(u.Host)
	u.Path, u.RawPath = strings.TrimRight(u.Path, "/"), ""
	return u, limits, nil
}

func canonicalHost(host string) string {
	// IPv6 zone identifiers name local interfaces and may be case-sensitive.
	if zone := strings.IndexByte(host, '%'); zone >= 0 {
		return strings.ToLower(host[:zone]) + host[zone:]
	}
	return strings.ToLower(host)
}

// CloseIdleConnections releases connections created by this source. Injected
// clients and custom default transports are borrowed and are never closed.
func (h *HTTP) CloseIdleConnections() {
	if h.ownedTransport != nil {
		h.ownedTransport.CloseIdleConnections()
	}
}

const httpInventoryEndpoint = "api/v1/search/device"
const httpInventoryFields = "ip,name,dns,vendor,os,model,os_ver,serial"
const httpFallbackQuery = "%"

func (h *HTTP) String() string   { return "netdisco.HTTP" }
func (h *HTTP) GoString() string { return h.String() }
// Devices logs in when no API key is configured and pages through the
// inventory search, falling back to a single wildcard query on servers that
// require one. The whole call is bounded by the configured timeout and
// byte, page and device limits.
func (h *HTTP) Devices(parent context.Context) ([]inventory.Device, error) {
	ctx, cancel := context.WithTimeout(parent, h.limits.Timeout)
	defer cancel()
	f := &httpFetch{source: h, ctx: ctx, key: h.key}
	if f.key == "" {
		if e := f.login(); e != nil {
			return nil, e
		}
	}
	pages := newPageSet()
	for page := range h.limits.MaxPages {
		query := url.Values{"fields": {httpInventoryFields}, "limit": {strconv.Itoa(h.limits.PageSize)}, "offset": {strconv.Itoa(page * h.limits.PageSize)}}
		body, status, e := f.request(httpInventoryEndpoint, http.MethodGet, query)
		if e != nil {
			return nil, e
		}
		if status == http.StatusBadRequest && missingQuery(body) {
			return f.fallback()
		}
		if status < 200 || status >= 300 {
			return nil, newInventoryError("request_failed", fmt.Errorf("HTTP %d", status))
		}
		data, e := decodeHTTPInventory(ctx, body, h.limits.MaxDevices, &f.count)
		if e != nil {
			return nil, e
		}
		if len(data) == 0 {
			if e := ctx.Err(); e != nil {
				return nil, newInventoryError("inventory_timeout", e)
			}
			return pages.rows, nil
		}
		advances, e := pages.add(ctx, data)
		if e != nil {
			return nil, e
		}
		if !advances {
			return nil, newInventoryError("pagination_stalled", nil)
		}
	}
	return nil, newInventoryError("max_pages", nil)
}

// httpFetch is one bounded inventory download.
type httpFetch struct {
	source *HTTP
	ctx    context.Context //nolint:containedctx // one fetch shares a single deadline across its requests
	key    string
	total  int64
	count  int
}

// request sends one API request and reads its body within the per-response
// and whole-inventory byte limits.
func (f *httpFetch) request(endpoint, method string, query url.Values) ([]byte, int, error) {
	h := f.source
	u := *h.base
	u.Path = strings.TrimRight(u.Path, "/") + "/" + endpoint
	u.RawQuery = query.Encode()
	req, e := http.NewRequestWithContext(f.ctx, method, u.String(), nil)
	if e != nil {
		return nil, 0, newInventoryError("request_failed", e)
	}
	req.Header.Set("Accept", "application/json")
	if f.key != "" {
		req.Header.Set("Authorization", "Apikey "+f.key)
	} else {
		req.SetBasicAuth(h.username, h.password)
	}
	resp, e := h.client.Do(req)
	if e != nil {
		return nil, 0, newInventoryError("connection_failed", e)
	}
	defer resp.Body.Close()
	limit := min(h.limits.MaxResponseBytes, h.limits.MaxInventoryBytes-f.total)
	b, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil {
		return nil, resp.StatusCode, newInventoryError("connection_failed", e)
	}
	f.total += int64(len(b))
	if f.total > h.limits.MaxInventoryBytes {
		return nil, resp.StatusCode, newInventoryError("max_inventory_bytes", nil)
	}
	if int64(len(b)) > h.limits.MaxResponseBytes {
		return nil, resp.StatusCode, newInventoryError("max_response_bytes", nil)
	}
	if e = f.ctx.Err(); e != nil {
		return nil, 0, newInventoryError("inventory_timeout", e)
	}
	return b, resp.StatusCode, nil
}

// login exchanges the basic credentials for an API key.
func (f *httpFetch) login() error {
	b, status, e := f.request("login", http.MethodPost, nil)
	if e != nil {
		return e
	}
	if status < 200 || status >= 300 {
		return newInventoryError("request_failed", nil)
	}
	var data struct {
		APIKey string `json:"api_key"`
	}
	if e = json.Unmarshal(b, &data); e != nil || data.APIKey == "" || strings.ContainsAny(data.APIKey, "\r\n\x00") {
		return newInventoryError("authentication_failed", nil)
	}
	f.key = data.APIKey
	return nil
}

// fallback fetches the whole inventory with the wildcard query.
func (f *httpFetch) fallback() ([]inventory.Device, error) {
	body, status, e := f.request(httpInventoryEndpoint, http.MethodGet, url.Values{"q": {httpFallbackQuery}, "seeallcolumns": {"true"}})
	if e != nil {
		return nil, e
	}
	if status < 200 || status >= 300 {
		return nil, newInventoryError("request_failed", nil)
	}
	data, e := decodeHTTPInventory(f.ctx, body, f.source.limits.MaxDevices, &f.count)
	if e != nil {
		return nil, e
	}
	return dedupContext(f.ctx, data)
}

// missingQuery reports whether a 400 body is the server's "Missing query"
// error, which means paging without a query is unsupported.
func missingQuery(body []byte) bool {
	var message struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(body, &message) == nil && message.Error == "Missing query"
}

// pageSet accumulates unique rows across pages and detects pages that add
// no new address.
type pageSet struct {
	seen   map[string]bool
	unique map[inventory.Device]bool
	rows   []inventory.Device
}

// newPageSet returns an empty set.
func newPageSet() *pageSet {
	return &pageSet{seen: map[string]bool{}, unique: map[inventory.Device]bool{}, rows: make([]inventory.Device, 0)}
}

// add appends the rows not yet seen and reports whether data contained an
// address not seen on earlier pages.
func (s *pageSet) add(ctx context.Context, data []inventory.Device) (advances bool, err error) {
	for _, row := range data {
		if e := ctx.Err(); e != nil {
			return false, newInventoryError("inventory_timeout", e)
		}
		if !s.seen[row.IP] {
			advances = true
		}
		s.seen[row.IP] = true
		if !s.unique[row] {
			s.unique[row] = true
			s.rows = append(s.rows, row)
		}
	}
	return advances, nil
}

func dedupContext(ctx context.Context, rows []inventory.Device) ([]inventory.Device, error) {
	seen := make(map[inventory.Device]bool)
	out := make([]inventory.Device, 0, len(rows))
	for _, r := range rows {
		if e := ctx.Err(); e != nil {
			return nil, newInventoryError("inventory_timeout", e)
		}
		if !seen[r] {
			out = append(out, r)
			seen[r] = true
		}
	}
	if e := ctx.Err(); e != nil {
		return nil, newInventoryError("inventory_timeout", e)
	}
	return out, nil
}

var inventoryFieldNames = [...]string{"ip", "name", "dns", "vendor", "os", "model", "os_ver", "serial"}

func inventoryFields(d *inventory.Device) [8]*string {
	return [8]*string{&d.IP, &d.Name, &d.DNS, &d.Vendor, &d.OS, &d.Model, &d.OSVersion, &d.Serial}
}

func decodeHTTPInventory(ctx context.Context, b []byte, maxDevices int, count *int) ([]inventory.Device, error) {
	if e := ctx.Err(); e != nil {
		return nil, newInventoryError("inventory_timeout", e)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return []inventory.Device{}, nil
	}
	if !utf8.Valid(b) {
		return nil, newInventoryError("invalid_json", nil)
	}
	// Raw-message maps keep acceptance case-sensitive and retain JSON's last-key-wins behavior.
	var data []map[string]json.RawMessage
	if e := json.Unmarshal(b, &data); e != nil || data == nil {
		return nil, newInventoryError("invalid_inventory", e)
	}
	*count += len(data)
	if *count > maxDevices {
		return nil, newInventoryError("max_devices", nil)
	}
	rows := make([]inventory.Device, len(data))
	for i, row := range data {
		if e := ctx.Err(); e != nil {
			return nil, newInventoryError("inventory_timeout", e)
		}
		if row == nil {
			return nil, newInventoryError("invalid_inventory", nil)
		}
		fields := inventoryFields(&rows[i])
		for j, name := range inventoryFieldNames {
			if e := ctx.Err(); e != nil {
				return nil, newInventoryError("inventory_timeout", e)
			}
			value, ok := row[name]
			if !ok {
				continue
			}
			// Unmarshaling null into an empty string preserves optional null-as-empty fields.
			if e := json.Unmarshal(value, fields[j]); e != nil {
				return nil, newInventoryError("invalid_inventory", nil)
			}
		}
		if strings.TrimSpace(rows[i].IP) == "" {
			return nil, newInventoryError("invalid_inventory", nil)
		}
	}
	if e := ctx.Err(); e != nil {
		return nil, newInventoryError("inventory_timeout", e)
	}
	return rows, nil
}

func errorCode(e error) string {
	if ie, ok := errors.AsType[*InventoryError](e); ok {
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
