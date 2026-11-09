package textfsm

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkEmbeddedTemplate(b *testing.B) {
	for _, count := range []int{1, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			input := strings.Repeat("interface Ethernet1\n description uplink\n!\n", count)
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for b.Loop() {
				rows, err := ParseTemplate("cisco/show_running_config.textfsm", input)
				if err != nil || len(rows) != count {
					b.Fatal(err)
				}
			}
		})
	}
}
