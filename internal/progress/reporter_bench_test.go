package progress

import (
	"fmt"
	"io"
	"testing"
	"time"
)

func BenchmarkReporterSuccessfulTask(b *testing.B) {
	for _, tc := range []struct {
		name         string
		buildDetails bool
	}{{"current", false}, {"previous_detail_work", true}} {
		b.Run(tc.name, func(b *testing.B) {
			reporter := New(io.Discard, false)
			if err := reporter.Plan(b.N, b.N, 50, "backup"); err != nil {
				b.Fatal(err)
			}
			host, vendor := "192.0.2.1", "h3c"
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := reporter.Start(host, vendor); err != nil {
					b.Fatal(err)
				}
				if tc.buildDetails {
					reporter.detail = fmt.Sprintf("[%s] 正在连接与登录（%s）", safeText(host), safeText(vendor))
				}
				if err := reporter.Event(host, "login_complete", "ok"); err != nil {
					b.Fatal(err)
				}
				if tc.buildDetails {
					reporter.detail = fmt.Sprintf("[%s] 登录成功，正在采集配置", safeText(host))
				}
				if err := reporter.Event(host, "command_start", ""); err != nil {
					b.Fatal(err)
				}
				if tc.buildDetails {
					reporter.detail = fmt.Sprintf("[%s] 正在采集，等待设备响应", safeText(host))
				}
				if err := reporter.Outcome(host, "succeeded", "backed_up", "", time.Second); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if err := reporter.Finish(); err != nil {
				b.Fatal(err)
			}
		})
	}
}
