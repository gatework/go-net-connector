package connector

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"
)

func BenchmarkCommandOutput(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20, 8 << 20, 32 << 20} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			body := strings.Repeat("x", size) + "\r\nsw#"
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for b.Loop() {
				done := make(chan struct{})
				d, e := New(Profile{Name: "benchmark", LoginPrompt: regexp.MustCompile(`sw#\s*$`), CommandPrompt: regexp.MustCompile(`sw#\s*$`)}, ConnectionConfig{Host: "fixture", MaxOutputBytes: int64(size + 1024), CommandTimeout: 30 * time.Second}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
					a, c := net.Pipe()
					go func() {
						defer close(done)
						defer c.Close()
						io.WriteString(c, "sw#")
						r := bufio.NewReader(c)
						if _, e := r.ReadString('\n'); e == nil {
							io.WriteString(c, body)
						}
						io.Copy(io.Discard, c)
					}()
					return a, nil
				}))
				if e != nil {
					b.Fatal(e)
				}
				_, e = d.ExecuteCommand(b.Context(), Command{Text: "show data"}, ExecuteOptions{})
				if e != nil {
					b.Fatal(e)
				}
				if e = d.Close(); e != nil {
					b.Fatal(e)
				}
				<-done
			}
		})
	}
}
func BenchmarkCommandPaged1000(b *testing.B) {
	prompt := regexp.MustCompile(`sw#\s*$`)
	pager := regexp.MustCompile(`--More--`)
	b.ReportAllocs()
	for b.Loop() {
		done := make(chan struct{})
		d, e := New(Profile{Name: "benchmark", LoginPrompt: prompt, CommandPrompt: prompt, Pager: pager}, ConnectionConfig{Host: "fixture", CommandTimeout: 30 * time.Second}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
			a, c := net.Pipe()
			go func() {
				defer close(done)
				defer c.Close()
				io.WriteString(c, "sw#")
				r := bufio.NewReader(c)
				if _, e := r.ReadString('\n'); e != nil {
					return
				}
				for range 1000 {
					io.WriteString(c, "page-data\r\n--More--")
					if _, e := r.ReadByte(); e != nil {
						return
					}
				}
				io.WriteString(c, "\r\nsw#")
				io.Copy(io.Discard, c)
			}()
			return a, nil
		}))
		if e != nil {
			b.Fatal(e)
		}
		_, e = d.ExecuteCommand(b.Context(), Command{Text: "show pages"}, ExecuteOptions{})
		if e != nil {
			b.Fatal(e)
		}
		d.Close()
		<-done
	}
}
