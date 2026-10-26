package netdisco

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gatework/go-net-connector/storage"
)

func (r Report) Text(directory string, p Plan, workers int) string {
	b := r.batch
	s := r.Statistics()
	zone := time.FixedZone("UTC+8", 8*3600)
	lines := []string{"Network device backup report", strings.Repeat("=", 72), "Started: " + b.startedAt.In(zone).Format("2006-01-02 15:04:05 -07:00"), "Finished: " + b.finishedAt.In(zone).Format("2006-01-02 15:04:05 -07:00"), fmt.Sprintf("Inventory: %d devices; selected: %d devices; concurrency: %d", len(p.entries), len(p.tasks), workers), fmt.Sprintf("Succeeded: %v; partial: %v; failed: %v; skipped: %v", s["succeeded"], s["partial"], s["failed"], s["skipped"]), fmt.Sprintf("Elapsed: %.1f seconds; batch status: %s", b.finishedAt.Sub(b.startedAt).Seconds(), b.Status()), fmt.Sprintf("Success policy: %s; policy passed: %t", r.policy, r.Success()), fmt.Sprintf("Callback errors: %d; report error: %s", len(b.callbackErrors), display(b.reportError, "none"))}
	counts := b.Counts()
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	lines = append(lines, "Status counts: "+strings.Join(parts, ", "))
	groups := map[string]int{}
	failed := []Outcome{}
	for _, o := range b.outcomes {
		if !o.Success() && o.Status != "filtered" && o.Status != "sample_limit" {
			failed = append(failed, o)
			groups[display(o.Diagnostic.safe().ErrorCode, o.Status)]++
		}
	}
	keys = nil
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts = nil
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, groups[k]))
	}
	lines = append(lines, "Incomplete categories: "+display(strings.Join(parts, ", "), "none"), strings.Repeat("-", 72), "Incomplete devices: ")
	for _, o := range failed {
		name := strings.Map(func(c rune) rune {
			if unicode.IsControl(c) {
				return ' '
			}
			return c
		}, o.Device.Name)
		lines = append(lines, fmt.Sprintf("%s | %s | %s | %s | %d ms", o.Device.Host, name, o.Status, display(o.Diagnostic.safe().ErrorCode, "no error code"), o.Duration.Milliseconds()))
	}
	if len(failed) == 0 {
		lines = append(lines, "none")
	}
	if b.mode == "tftp" {
		v := s["verification"].(map[string]int)
		lines = append(lines, fmt.Sprintf("TFTP server files: verified %d, unverified %d.", v["verified"], v["unverified"]))
	}
	lines = append(lines, strings.Repeat("=", 72), "Backup directory: "+directory, "Full details: "+filepath.Join(directory, "summary.json"))
	return strings.Join(lines, "\n") + "\n"
}
func display(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
func WriteReport(ctx context.Context, r Report, directory string, p Plan, workers int) Report {
	destination := filepath.Join(directory, "summary.json")
	final := r
	final.batch.reportLocation = destination
	writeJSON := func(report Report) (storage.Receipt, error) {
		data, e := json.MarshalIndent(report.Summary(), "", "  ")
		if e != nil {
			return storage.Receipt{}, e
		}
		return storage.Write(ctx, destination, data)
	}
	_, e := storage.Write(ctx, filepath.Join(directory, "summary.txt"), []byte(final.Text(directory, p, workers)))
	var receipt storage.Receipt
	if e == nil {
		receipt, e = writeJSON(final)
	}
	if e == nil {
		return final
	}
	location := r.batch.reportLocation
	if receipt.Committed() {
		location = destination
	}
	var we *storage.WriteError
	if errors.As(e, &we) && we.Receipt.Committed() && we.Receipt.Path == destination {
		location = destination
	}
	failed := r
	failed.batch.reportLocation = location
	if r.batch.reportError == "" {
		failed = r.WithReportError(e, location)
	}
	saved := failed
	saved.batch.reportLocation = destination
	receipt, e = writeJSON(saved)
	if e == nil || receipt.Committed() {
		failed = saved
	}
	_, _ = storage.Write(ctx, filepath.Join(directory, "summary.txt"), []byte(failed.Text(directory, p, workers)))
	return failed
}
func (p Plan) Summary() map[string]any {
	rows := make([]map[string]any, 0, len(p.entries))
	counts := map[string]int{}
	for _, e := range p.entries {
		counts[e.Status]++
		r := e.Device
		rows = append(rows, map[string]any{"host": optional(r.Host), "source_ip": r.SourceIP, "name": optional(r.Name), "vendor": optional(r.Vendor), "source_vendor": optional(r.SourceVendor), "model": optional(r.Model), "os": optional(r.OS), "os_version": optional(r.OSVersion), "serial": optional(r.Serial), "status": e.Status})
	}
	return map[string]any{"mode": p.mode, "total": len(p.entries), "selected": len(p.tasks), "counts": counts, "devices": rows}
}
