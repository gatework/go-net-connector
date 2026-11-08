package fleet

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

	zh "github.com/gatework/go-net-connector/internal/display"
	"github.com/gatework/go-net-connector/storage"
)

func (r Report) Text(directory string, p Plan, workers int) string {
	b := r.batch
	s := r.Statistics()
	zone := time.FixedZone("UTC+8", 8*3600)
	lines := []string{"网络设备备份报告", strings.Repeat("=", 72), "开始时间：" + b.startedAt.In(zone).Format("2006-01-02 15:04:05 -07:00"), "结束时间：" + b.finishedAt.In(zone).Format("2006-01-02 15:04:05 -07:00"), fmt.Sprintf("清单：%d 台；选中：%d 台；并发：%d", len(p.entries), len(p.tasks), workers), fmt.Sprintf("成功：%v；部分成功：%v；失败：%v；跳过：%v", s["succeeded"], s["partial"], s["failed"], s["skipped"]), fmt.Sprintf("耗时：%.1f 秒；批次状态：%s", b.finishedAt.Sub(b.startedAt).Seconds(), zh.Status(b.Status())), fmt.Sprintf("成功策略：%s；策略通过：%s", zh.Policy(r.policy), zh.Bool(r.Success())), fmt.Sprintf("回调错误：%d；报告错误：%s", len(b.callbackErrors), displayOrFallback(b.reportError, "无"))}
	lines = append(lines, fmt.Sprintf("文本编码告警设备：%v（告警不等于采集失败；请核对各设备原文保存状态）", s["text_warning_devices"]))
	counts := b.Counts()
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", zh.Status(k), counts[k]))
	}
	lines = append(lines, "状态统计："+strings.Join(parts, ", "))
	groups := map[string]int{}
	failed := []Outcome{}
	for _, o := range b.outcomes {
		if !o.Success() && outcomeStatus(o.Status) != outcomeStatusFiltered && outcomeStatus(o.Status) != outcomeStatusSampleLimit {
			failed = append(failed, o)
			groups[displayOrFallback(o.Diagnostic.sanitized().ErrorCode, o.Status)]++
		}
	}
	keys = nil
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts = nil
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", zh.Error(k), groups[k]))
	}
	lines = append(lines, "未完成原因："+displayOrFallback(strings.Join(parts, ", "), "无"), strings.Repeat("-", 72), "未完成设备：")
	for _, o := range failed {
		name := strings.Map(func(c rune) rune {
			if unicode.IsControl(c) {
				return ' '
			}
			return c
		}, o.Device.Name)
		lines = append(lines, fmt.Sprintf("%s | %s | %s | %s | %d ms", o.Device.Host, name, zh.Status(o.Status), zh.Error(o.Diagnostic.sanitized().ErrorCode), o.Duration.Milliseconds()))
	}
	if len(failed) == 0 {
		lines = append(lines, "无")
	}
	if n, _ := s["text_warning_devices"].(int); n > 0 {
		lines = append(lines, strings.Repeat("-", 72), "编码告警设备（与备份失败分别统计）：")
	}
	for _, o := range b.outcomes {
		if a := o.Artifact.Backup; a != nil && a.TextWarning != nil {
			committed, verified := a.SourceEvidence()
			lines = append(lines, fmt.Sprintf("%s | 文本编码告警：%d 个字节；%s；详见 JSON 告警位置", o.Device.Host, a.TextWarning.Count, zh.SourceState(committed, verified)))
		}
	}
	if b.mode == modeTFTP {
		v := s["verification"].(map[string]int)
		lines = append(lines, fmt.Sprintf("TFTP 服务器文件：已核验 %d，未核验 %d。", v["verified"], v["unverified"]))
	}
	lines = append(lines, strings.Repeat("=", 72), "备份目录："+directory, "完整报告："+filepath.Join(directory, "summary.json"))
	return strings.Join(lines, "\n") + "\n"
}
func displayOrFallback(s, fallback string) string {
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
		data, e := json.MarshalIndent(report.document(), "", "  ")
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
		rows = append(rows, map[string]any{"host": nullableString(r.Host), "source_ip": r.SourceIP, "name": nullableString(r.Name), "vendor": nullableString(r.Vendor), "source_vendor": nullableString(r.SourceVendor), "model": nullableString(r.Model), "os": nullableString(r.OS), "os_version": nullableString(r.OSVersion), "serial": nullableString(r.Serial), "status": e.Status})
	}
	return map[string]any{"mode": string(p.mode), "total": len(p.entries), "selected": len(p.tasks), "counts": counts, "devices": rows}
}
