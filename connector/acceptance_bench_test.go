package connector

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
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
				d, e := New(StaticDriver{Grammar: Terminal{Name: "benchmark", LoginPrompt: testPattern(`sw#\s*$`), CommandPrompt: testPattern(`sw#\s*$`)}}, ConnectionConfig{Host: "fixture", MaxOutputBytes: int64(size + 1024), CommandTimeout: 30 * time.Second}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
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
	prompt := testPattern(`sw#\s*$`)
	pager := testPattern(`--More--`)
	b.ReportAllocs()
	for b.Loop() {
		done := make(chan struct{})
		d, e := New(StaticDriver{Grammar: Terminal{Name: "benchmark", LoginPrompt: prompt, CommandPrompt: prompt, Pager: pager}}, ConnectionConfig{Host: "fixture", CommandTimeout: 30 * time.Second}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
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

func BenchmarkCommandMultiline(b *testing.B) {
	for _, size := range []int{1 << 20, 8 << 20} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			const line = "interface Ethernet1\r\n description uplink\r\n!\r\n"
			body := strings.Repeat(line, size/len(line)) + "\r\nsw#"
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for b.Loop() {
				done := make(chan struct{})
				d, e := New(StaticDriver{Grammar: Terminal{Name: "benchmark", LoginPrompt: testPattern(`sw#\s*$`), CommandPrompt: testPattern(`sw#\s*$`)}}, ConnectionConfig{Host: "fixture", MaxOutputBytes: int64(size + 1024), CommandTimeout: 30 * time.Second}, WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
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

func BenchmarkCommandLogging(b *testing.B) {
	for _, level := range []string{"nil", "info", "debug"} {
		b.Run(level, func(b *testing.B) {
			cfg := ConnectionConfig{Host: "fixture", Password: "password", CommandTimeout: 30 * time.Second}
			if level != "nil" {
				cfg.Logger, cfg.LogLevel = io.Discard, LogLevel(level)
			}
			prompt := testPattern(`sw#\s*$`)
			done := make(chan struct{})
			d, err := New(StaticDriver{Grammar: Terminal{Name: "benchmark", LoginPrompt: prompt, CommandPrompt: prompt}}, cfg,
				WithTransportFactory(func(context.Context, ConnectionConfig) (io.ReadWriteCloser, error) {
					a, peer := net.Pipe()
					go func() {
						defer close(done)
						defer peer.Close()
						if _, err := io.WriteString(peer, "sw#"); err != nil {
							return
						}
						r := bufio.NewReader(peer)
						for {
							if _, err := r.ReadString('\n'); err != nil {
								return
							}
							if _, err := io.WriteString(peer, "show status\r\nstatus: healthy\r\nsw#"); err != nil {
								return
							}
						}
					}()
					return a, nil
				}))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = d.Close(); <-done })
			if err := d.Connect(b.Context()); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := d.ExecuteCommand(b.Context(), Command{Text: "show status"}, ExecuteOptions{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
