package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"

	vt "github.com/charmbracelet/x/vt"
)

func TestSuccessfulTasksDoNotAppendLogs(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.Plan(1000, 1000, 50, "backup")
	for i := 0; i < 1000; i++ {
		p.Start("host", "h3c")
		p.Event("host", "login_complete", "ok")
		p.Outcome("host", "backed_up", "", time.Second)
	}
	if strings.Contains(b.String(), "备份成功") || strings.Contains(b.String(), "开始任务") || strings.Count(b.String(), "\n") > 3 {
		t.Fatalf("normal tasks flooded output: %d lines", strings.Count(b.String(), "\n"))
	}
}

func TestPanelScreenKeepsErrorsAndDoesNotScrollSuccesses(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		var b bytes.Buffer
		p := New(&b, verbose)
		p.tty = true
		p.Plan(1000, 1000, 50, "backup")
		for i := 0; i < 1000; i++ {
			p.Start("交换机👩‍💻", "h3c")
			p.Event("交换机👩‍💻", "login_complete", "ok")
			p.lastRender = time.Time{}
			p.Outcome("交换机👩‍💻", "backed_up", "", time.Second)
		}
		screen := vt.NewEmulator(80, 24)
		screen.Write(b.Bytes())
		view := screen.String()
		if strings.Contains(view, "备份成功") || strings.Contains(view, "开始任务") || strings.Count(view, "成功 1000") != 1 || !strings.Contains(view, "1000/1000") {
			t.Fatal(view)
		}
		b.Reset()
		p.Start("192.0.2.1", "h3c")
		p.Outcome("192.0.2.1", "failed", "authentication_failed", time.Second)
		p.tick()
		screen.Write(b.Bytes())
		view = screen.String()
		if !strings.Contains(view, "认证失败") {
			t.Fatal(view)
		}
	}
}

func TestResumeAfterStopCannotRedraw(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, false)
	p.tty = true
	resume := p.Pause()
	stop := p.RunUpdates(t.Context())
	stop()
	n := b.Len()
	resume()
	if b.Len() != n {
		t.Fatal("panel redrawn after stop")
	}
}
func TestPauseSuppressesHeartbeat(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.tty = true
	p.tick()
	resume := p.Pause()
	n := b.Len()
	p.tick()
	if b.Len() != n {
		t.Fatal("prompt overwritten")
	}
	resume()
	if p.rows == 0 {
		t.Fatal("panel not restored")
	}
}

func TestTFTPStagesKeepWorkflowPhase(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.Plan(1, 1, 1, "tftp")
	p.Phase("正在执行 TFTP 备份")
	p.Start("host", "h3c")
	p.Event("host", "login_complete", "ok")
	p.Stage("host", "upload")
	p.Event("host", "command_start", "")
	if p.active["host"].stage != stageTFTPUpload {
		t.Fatal(p.active)
	}
	if !strings.Contains(p.activityLocked(), "上传 1") {
		t.Fatal(p.activityLocked())
	}
	p.Stage("host", "archive")
	if !strings.Contains(p.activityLocked(), "归档 1") {
		t.Fatal(p.activityLocked())
	}
}
