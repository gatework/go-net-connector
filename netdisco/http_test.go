package netdisco

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPInventoryAuthenticationAndPagination(t *testing.T) {
	var offsets []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/base/login" {
			u, p, ok := r.BasicAuth()
			if !ok || u != "u" || p != "p" {
				t.Error("basic auth")
			}
			io.WriteString(w, `{"api_key":"key"}`)
			return
		}
		if r.Header.Get("Authorization") != "Apikey key" {
			t.Error("missing token")
		}
		offsets = append(offsets, r.URL.Query().Get("offset"))
		if len(offsets) == 1 {
			io.WriteString(w, `[{"ip":"192.0.2.1","name":"sw","vendor":"H3C"}]`)
		} else {
			io.WriteString(w, `[]`)
		}
	}))
	defer s.Close()
	source, e := NewHTTP(HTTPOptions{URL: s.URL + "/base", Username: "u", Password: "p"})
	if e != nil {
		t.Fatal(e)
	}
	rows, e := source.Devices(t.Context())
	if e != nil || len(rows) != 1 || rows[0].Name != "sw" || len(offsets) != 2 {
		t.Fatalf("%v %v %v", rows, e, offsets)
	}
}
func TestHTTPFallbackAndNoPartialResults(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "%" {
			json.NewEncoder(w).Encode([]map[string]string{{"ip": "192.0.2.1"}})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"Missing query"}`)
	}))
	defer s.Close()
	source, _ := NewHTTP(HTTPOptions{URL: s.URL, APIKey: "secret"})
	rows, e := source.Devices(t.Context())
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `[{"ip":"192.0.2.1"}]`) }))
	defer bad.Close()
	source, _ = NewHTTP(HTTPOptions{URL: bad.URL, APIKey: "secret"})
	rows, e = source.Devices(t.Context())
	if e == nil || rows != nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(rows, e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = source.Devices(ctx)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestHTTPInjectedClientRejectsRedirects(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("https_%v", secure), func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetCalls.Add(1)
				io.WriteString(w, `[]`)
			}))
			defer target.Close()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) })
			var origin *httptest.Server
			if secure {
				origin = httptest.NewTLSServer(handler)
			} else {
				origin = httptest.NewServer(handler)
			}
			defer origin.Close()
			client := origin.Client()
			var callerPolicyCalls atomic.Int32
			client.CheckRedirect = func(*http.Request, []*http.Request) error { callerPolicyCalls.Add(1); return nil }
			source, e := NewHTTP(HTTPOptions{URL: origin.URL, APIKey: "secret", Client: client, RequireHTTPS: secure})
			if e != nil {
				t.Fatal(e)
			}
			rows, e := source.Devices(t.Context())
			if rows != nil || errorCode(e) != "request_failed" || targetCalls.Load() != 0 || callerPolicyCalls.Load() != 0 {
				t.Fatalf("redirect boundary: rows=%v err=%v target=%d policy=%d", rows, e, targetCalls.Load(), callerPolicyCalls.Load())
			}
			resp, e := client.Get(origin.URL)
			if e != nil {
				t.Fatal(e)
			}
			resp.Body.Close()
			if targetCalls.Load() != 1 || callerPolicyCalls.Load() != 1 {
				t.Fatal("caller client was mutated")
			}
		})
	}
}

func TestHTTPRejectsBlankIPWithoutPartialResult(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallback_%v", fallback), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if fallback && r.URL.Query().Get("q") != "%" {
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, `{"error":"Missing query"}`)
					return
				}
				if !fallback && r.URL.Query().Get("offset") == "0" {
					io.WriteString(w, `[{"ip":"192.0.2.1"}]`)
					return
				}
				if fallback || r.URL.Query().Get("offset") == "500" {
					io.WriteString(w, `[{"ip":" \t "}]`)
					return
				}
				io.WriteString(w, `[]`)
			}))
			defer s.Close()
			source, e := NewHTTP(HTTPOptions{URL: s.URL, APIKey: "secret"})
			if e != nil {
				t.Fatal(e)
			}
			rows, e := source.Devices(t.Context())
			if rows != nil || errorCode(e) != "invalid_inventory" {
				t.Fatalf("rows=%v err=%v", rows, e)
			}
		})
	}
}
