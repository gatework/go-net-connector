package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gatework/go-net-connector/internal/progress"
	"github.com/gatework/go-net-connector/internal/workflow"
)

func TestBackupMethodsShareHumanReportLayout(t *testing.T) {
	for _, mode := range []string{"backup", "tftp"} {
		r, out, _ := fixtureRuntime(t)
		result := workflow.Result{Batch: &workflow.BatchResult{Directory: t.TempDir()}}
		if e := r.renderResult(result, options{mode: mode}, false); e != nil {
			t.Fatal(e)
		}
		for _, want := range []string{"备份方式：", "备份目录：", "文本报告：", "summary.txt", "JSON 报告：", "summary.json"} {
			if !strings.Contains(out.String(), want) {
				t.Fatal(mode, "missing", want, out.String())
			}
		}
	}
}

func TestBatchResultIsRenderedOnlyByTheCLI(t *testing.T) {
	var progressOutput bytes.Buffer
	reporter := progress.New(&progressOutput, false)
	if err := reporter.Plan(1, 1, 1, "backup"); err != nil {
		t.Fatal(err)
	}
	reporter.Start("192.0.2.1", "h3c")
	reporter.Outcome("192.0.2.1", "backed_up", "", 0)
	if err := reporter.Finish(); err != nil {
		t.Fatal(err)
	}

	r, out, _ := fixtureRuntime(t)
	result := workflow.Result{Success: true, Batch: &workflow.BatchResult{Directory: t.TempDir()}}
	if err := r.renderResult(result, options{mode: "backup"}, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(progressOutput.String(), "执行结果") {
		t.Fatalf("progress output duplicated the CLI result: %s", progressOutput.String())
	}
	if !strings.Contains(progressOutput.String(), "总耗时") {
		t.Fatalf("progress output lost the elapsed time: %s", progressOutput.String())
	}
	if count := strings.Count(out.String(), "-- 执行结果 "); count != 1 {
		t.Fatalf("CLI result sections = %d, output: %s", count, out.String())
	}
}
