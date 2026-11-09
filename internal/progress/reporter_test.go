package progress

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}
func (b *lockedBuffer) WriteString(s string) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.WriteString(s)
}
func (b *lockedBuffer) text() string { b.Lock(); defer b.Unlock(); return b.Buffer.String() }

type broken struct {
	err   error
	calls int
}

func (b *broken) Write([]byte) (int, error) { b.calls++; return 0, b.err }
func TestReporterShowsPlanStartStagesAndOutcome(t *testing.T) {
	var b lockedBuffer
	p := New(&b, true)
	p.Phase("正在读取设备清单")
	p.Plan(3, 2, 4, "backup")
	p.Start("192.0.2.1", "h3c")
	p.Event("192.0.2.1", "login_complete", "ok")
	p.Event("192.0.2.1", "command_start", "")
	p.Outcome("192.0.2.1", "succeeded", "backed_up", "", time.Second)
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"读取设备清单", "清单 3 台", "选中 2 台", "并发 4", "执行进度", "总耗时"} {
		if !strings.Contains(b.text(), want) {
			t.Errorf("missing %q: %s", want, b.text())
		}
	}
	if strings.Contains(b.text(), "执行结果") || strings.Contains(b.text(), "批次完成") {
		t.Fatalf("progress reporter rendered a final result: %s", b.text())
	}
	if count := strings.Count(b.text(), "总耗时"); count != 1 {
		t.Fatalf("elapsed-time lines = %d: %s", count, b.text())
	}
}
func TestReporterHeartbeatStopsAndRetainsPhase(t *testing.T) {
	var b lockedBuffer
	p := New(&b, false)
	p.interval = time.Millisecond
	p.Phase("正在读取设备清单")
	ctx, cancel := context.WithCancel(t.Context())
	stop := p.RunUpdates(ctx)
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(b.text(), "等待中") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	stop()
	stop()
	n := len(b.text())
	time.Sleep(4 * time.Millisecond)
	if len(b.text()) != n || !strings.Contains(b.text(), "正在读取设备清单") {
		t.Fatal(b.text())
	}
}

func TestReporterShowsArchivePreflightSeparateFromDeviceTasks(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, false)
	if err := p.Plan(976, 976, 50, "tftp"); err != nil {
		t.Fatal(err)
	}
	if err := p.ArchivePreflight(ArchivePreflight{Stage: "正在预检 TFTP 归档服务", Completed: 0, Total: 13, Active: 4, Workers: 4, Group: 1, Groups: 1}); err != nil {
		t.Fatal(err)
	}
	p.tick()
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"归档预检 0/13", "活动 4", "预检并发 4/4", "设备任务尚未开始 0/976", "设备并发 50"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in progress output: %s", want, b.String())
		}
	}
}

func TestReporterPrintsArchivePreflightStageChanges(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, false)
	if err := p.Plan(976, 976, 50, "tftp"); err != nil {
		t.Fatal(err)
	}
	base := ArchivePreflight{Completed: 0, Total: 13, Active: 4, Workers: 4, Group: 1, Groups: 1}
	base.Stage = "正在连接 TFTP 归档服务器 SSH"
	if err := p.ArchivePreflight(base); err != nil {
		t.Fatal(err)
	}
	base.Stage = "正在验证归档程序与目录权限"
	if err := p.ArchivePreflight(base); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), base.Stage) {
		t.Fatalf("non-TTY output did not print the changed preflight stage: %s", b.String())
	}
}

func TestReporterBeginsTopologyApplyPhaseWithoutRepeatingPlan(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, false)
	if err := p.Plan(3, 3, 2, "topology"); err != nil {
		t.Fatal(err)
	}
	p.Start("192.0.2.1", "h3c")
	p.Outcome("192.0.2.1", "succeeded", "discovered", "", time.Second)
	if err := p.Phase("重新核验并应用拓扑计划"); err != nil {
		t.Fatal(err)
	}
	if err := p.BeginPhase(1, 2); err != nil {
		t.Fatal(err)
	}
	p.Start("192.0.2.2", "h3c")
	p.Outcome("192.0.2.2", "succeeded", "applied", "", time.Second)
	p.tick()
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(b.String(), "-- 任务计划"); got != 1 {
		t.Fatalf("task plan section count = %d: %s", got, b.String())
	}
	if !strings.Contains(b.String(), "重新核验并应用拓扑计划") || !strings.Contains(b.String(), "已完成 1/1") {
		t.Fatalf("apply phase was not rendered: %s", b.String())
	}
}
func TestReporterOutputFailureIsSticky(t *testing.T) {
	cause := errors.New("closed")
	w := &broken{err: cause}
	p := New(w, false)
	p.Phase("读取清单")
	p.Start("192.0.2.1", "h3c")
	_ = p.Finish()
	if !errors.Is(p.Err(), cause) || w.calls != 1 {
		t.Fatal(p.Err(), w.calls)
	}
}
func TestReporterDoesNotPrintUnknownMetadataOrControls(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.Start("host\x1b[31m\nforged", "h3c")
	p.Event("host", "PRIVATE_CONFIG secret=abc", "")
	p.Outcome("host", "failed", "", "PRIVATE_SECRET", time.Second)
	if strings.ContainsAny(b.String(), "\x1b\r") || strings.Contains(b.String(), "PRIVATE") {
		t.Fatal(b.String())
	}
}

func TestReporterBuildsTaskDetailsOnlyWhenVerbose(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		t.Run(map[bool]string{false: "quiet", true: "verbose"}[verbose], func(t *testing.T) {
			p := New(io.Discard, verbose)
			p.Plan(1, 1, 1, "backup")
			p.Start("192.0.2.1", "h3c")
			if verbose && !strings.Contains(p.detail, "正在连接与登录") || !verbose && p.detail != "" {
				t.Fatalf("start detail=%q verbose=%v", p.detail, verbose)
			}
			p.Event("192.0.2.1", "login_complete", "ok")
			if verbose && !strings.Contains(p.detail, "正在采集配置") || !verbose && p.detail != "" {
				t.Fatalf("event detail=%q verbose=%v", p.detail, verbose)
			}
		})
	}
}

func TestFailedEventsDoNotClaimCompletion(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.Start("192.0.2.1", "h3c")
	p.Event("192.0.2.1", "command_complete", "failed")
	p.Event("192.0.2.1", "operation_complete", "failed")
	if strings.Contains(b.String(), "命令响应已接收") || strings.Contains(b.String(), "设备脚本执行完成") || !strings.Contains(p.detail, "失败") {
		t.Fatal(b.String())
	}
}
func TestSkippedAndReviewOutcomesAreNotFailures(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, false)
	p.Plan(2, 2, 1, "topology")
	p.Start("192.0.2.1", "unsupported")
	p.Outcome("192.0.2.1", "stopped", "", "", time.Second)
	p.Start("192.0.2.2", "h3c")
	p.Outcome("192.0.2.2", "partial", "partial_planned", "", time.Second)
	if p.failed != 0 || p.skipped != 1 || p.pending != 1 {
		t.Fatal(b.String())
	}
	p.Plan(2, 1, 1, "topology")
	p.Start("192.0.2.2", "h3c")
	p.Outcome("192.0.2.2", "succeeded", "applied", "", time.Second)
	if p.completed != 1 || p.total != 1 || p.failed != 0 {
		t.Fatal(b.String())
	}
}
func TestTTYHeartbeatClearsAllPanelRows(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, false)
	p.tty = true
	p.Phase("读取清单")
	p.tick()
	p.tick()
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\r\x1b[2K") || p.rows != 0 || !strings.Contains(b.String(), "总耗时") || strings.Contains(b.String(), "批次完成") {
		t.Fatal(b.String())
	}
}

func TestReporterSeparatesPlanProgressAndWarnings(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, false)
	p.Plan(3, 3, 2, "backup")
	for _, host := range []string{"192.0.2.1", "192.0.2.2"} {
		p.Start(host, "h3c")
		p.Outcome(host, "failed", "", "login_timeout", time.Second)
	}
	p.Start("192.0.2.3", "h3c")
	p.Outcome("192.0.2.3", "succeeded", "backed_up", "", time.Second)
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, label := range []string{"任务计划", "执行进度", "异常与警告"} {
		index := strings.Index(b.String(), "-- "+label+" ")
		if index <= last || strings.Count(b.String(), "-- "+label+" ") != 1 {
			t.Fatalf("missing or repeated section %s in %q", label, b.String())
		}
		last = index
	}
	if strings.Contains(b.String(), "-- 执行结果 ") {
		t.Fatalf("progress reporter rendered a final result: %s", b.String())
	}
}
