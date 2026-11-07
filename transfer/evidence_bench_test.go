package transfer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gatework/go-net-connector/connector"
)

func BenchmarkCompleted(b *testing.B) {
	for _, test := range []struct{ vendor, success string }{
		{"cisco_ios", "[OK - 123 bytes]"},
		{"cisco_nxos", "123 bytes copied in 0.25 secs (492 bytes/sec)"},
		{"fortinet", "Send config file to tftp server OK."},
		{"centec", "Sent 123 bytes in 1.2 seconds"},
		{"hillstone", "Export ok, target file name backup.cfg"},
		{"palo_alto", "Sent 123 bytes in 1.2 seconds [820 bit/s]"},
		{"radware", "Current config successfully transferred to 192.0.2.9:backup.tgz"},
		{"h3c", "File transfer completed successfully."},
		{"h3c_wireless", "TFTP: 123 bytes sent in 0.25 second(s)"},
		{"huawei", "100 123k 0 0 100 123k 0 0 0"},
	} {
		policy := tftpPolicyForTest(b, test.vendor)
		for _, count := range []int{1, 1000, 10000} {
			for _, outcome := range []string{"success_last", "absent"} {
				b.Run(fmt.Sprintf("%s/lines=%d/%s", test.vendor, count, outcome), func(b *testing.B) {
					noise := []string{
						"Sending configuration block 00421 / 01000, 64512 bytes acknowledged",
						"Transfer in progress: 73 percent, waiting for acknowledgement",
						"99 123k 0 0 99 123k 0 0 0",
						"Configuration archive contains 123 bytes; remote upload pending",
					}
					lines := make([]string, count)
					for i := range lines {
						lines[i] = noise[i%len(noise)]
					}
					want := outcome == "success_last"
					if want {
						lines[len(lines)-1] = test.success
					}
					b.ReportAllocs()
					var got bool
					for b.Loop() {
						_, got, _ = policy.Completed(lines, TFTPOptions{Server: "192.0.2.9", Path: "backup.tgz"})
					}
					if got != want {
						b.Fatalf("completion = %t, want %t", got, want)
					}
				})
			}
		}
	}
}

func BenchmarkEvidence(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20, 16 << 20} {
		b.Run(fmt.Sprintf("bytes=%d", size), func(b *testing.B) {
			line := "Interface Ethernet1 telemetry progress 73 percent\r"
			output := strings.Repeat(line, size/len(line)+1)[:size]
			steps := []evidenceStep{{command: connector.Command{Text: "show configuration"}, prompt: "switch#", output: output}}
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				_, _, err := collectEvidence(steps, failed)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
