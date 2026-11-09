package progress

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	vt "github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

type synchronizedOutput struct {
	sync.Mutex
	data []byte
}

func (o *synchronizedOutput) Write(p []byte) (int, error) {
	o.Lock()
	defer o.Unlock()
	o.data = append(o.data, p...)
	return len(p), nil
}

func (o *synchronizedOutput) String() string {
	o.Lock()
	defer o.Unlock()
	return string(o.data)
}

func TestActualPTYPanelWidths(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	for _, width := range []uint16{40, 80, 120} {
		master, slave, err := pty.Open()
		if err != nil {
			t.Fatal(err)
		}
		if err = pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: width}); err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		done := make(chan struct{})
		go func() { io.Copy(&b, master); close(done) }()
		p := New(slave, true)
		if !p.tty {
			t.Fatal("PTY not detected")
		}
		p.Plan(50, 50, 50, "backup")
		for range 50 {
			p.Start("中文👩‍💻é交换机", "h3c")
			p.Event("中文👩‍💻é交换机", "login_complete", "ok")
			p.Outcome("中文👩‍💻é交换机", "backed_up", "", time.Second)
			p.tick()
		}
		p.Finish()
		slave.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			master.Close()
			<-done
		}
		master.Close()
		screen := vt.NewEmulator(int(width), 24)
		screen.Write(b.Bytes())
		view := screen.String()
		if strings.Contains(view, "排队") || strings.Contains(view, "备份成功") || strings.Contains(view, "批次完成") {
			t.Fatal(view)
		}
	}
}

func TestActualPTYKeepsProgressPanelStableWhenEnterIsEchoed(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	for _, tc := range []struct {
		name, mode, visible string
		setup               func(*Reporter) error
	}{
		{name: "backup", mode: "backup", visible: "备份配置", setup: func(p *Reporter) error {
			if err := p.Start("192.0.2.1", "h3c"); err != nil {
				return err
			}
			return p.Event("192.0.2.1", "login_complete", "ok")
		}},
		{name: "tftp", mode: "tftp", visible: "预检并发 4/4", setup: func(p *Reporter) error {
			return p.ArchivePreflight(ArchivePreflight{Stage: "正在预检 TFTP 归档服务", Completed: 0, Total: 13, Active: 4, Workers: 4, Group: 1, Groups: 1})
		}},
		{name: "topology", mode: "topology", visible: "重新核验并应用拓扑计划", setup: func(p *Reporter) error {
			if err := p.Phase("重新核验并应用拓扑计划"); err != nil {
				return err
			}
			return p.BeginPhase(1, 50)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			if err = pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
				t.Fatal(err)
			}
			var output synchronizedOutput
			readDone := make(chan struct{})
			go func() { _, _ = io.Copy(&output, master); close(readDone) }()
			defer func() {
				select {
				case <-readDone:
				case <-time.After(time.Second):
					t.Error("PTY output reader did not stop")
				}
			}()
			defer master.Close()
			defer slave.Close()
			p := New(slave, false)
			if !p.tty {
				t.Fatal("PTY not detected")
			}
			if err := p.Plan(976, 976, 50, tc.mode); err != nil {
				t.Fatal(err)
			}
			p.interval = 10 * time.Millisecond
			stop := p.RunUpdates(t.Context())
			defer stop()
			if err := tc.setup(p); err != nil {
				t.Fatal(err)
			}
			for range 5 {
				if _, err := master.Write([]byte("\n")); err != nil {
					t.Fatal(err)
				}
				p.tick()
				time.Sleep(20 * time.Millisecond)
			}
			time.Sleep(30 * time.Millisecond)
			rawOutput := output.String()
			if count := strings.Count(rawOutput, "-- 当前进度"); count != 1 {
				t.Fatalf("progress title was emitted %d times during one live panel", count)
			}
			screen := vt.NewEmulator(80, 24)
			screen.Write([]byte(rawOutput))
			view := screen.String()
			if count := strings.Count(view, "-- 当前进度"); count != 1 {
				t.Fatalf("progress heading visible %d times after echoed Enter: %s", count, view)
			}
			for _, want := range []string{tc.visible} {
				if !strings.Contains(view, want) {
					t.Errorf("missing %q from live PTY screen: %s", want, view)
				}
			}
			stop()
			_ = p.Finish()
		})
	}
}
