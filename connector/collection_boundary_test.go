package connector

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
)

type boundaryStrategy struct {
	renderedConfig
	panicFinish bool
}

func (s boundaryStrategy) Finish(context.Context, Result) (string, error) {
	if s.panicFinish {
		panic("finish")
	}
	return "invented", nil
}
func TestBackupAbnormalCollectionRetiresSession(t *testing.T) {
	p := fixtureDriver()
	p.Collector = func() ConfigStrategy { return boundaryStrategy{panicFinish: true} }
	d, _ := fixtureDevice(t, p, []string{"show config\r\nbody\r\nsw#"})
	func() {
		defer func() {
			if recover() == nil {
				t.Error("missing panic")
			}
		}()
		_, _ = d.Backup(t.Context(), BackupOptions{Path: filepath.Join(t.TempDir(), "x")})
	}()
	if d.Connected() {
		t.Fatal("abnormal collection left session connected")
	}
}
func TestCollectionRejectsAllSkippedEvidence(t *testing.T) {
	p := fixtureDriver()
	p.Collector = func() ConfigStrategy { return boundaryStrategy{} }
	p.Prepare = func(_ context.Context, c Command, _ Executor) (Command, bool, error) { return c, true, nil }
	d, _ := fixtureDevice(t, p, nil)
	_, err := d.RunningConfig(t.Context())
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("got %v", err)
	}
}

func TestCollectionAbnormalExitRetiresAllEntryPoints(t *testing.T) {
	for _, backup := range []bool{false, true} {
		for _, factory := range []bool{false, true} {
			for _, goexit := range []bool{false, true} {
				t.Run(fmt.Sprintf("backup=%t/factory=%t/goexit=%t", backup, factory, goexit), func(t *testing.T) {
					abort := func() {
						if goexit {
							runtime.Goexit()
						}
						panic("strategy aborted")
					}
					p := fixtureDriver()
					p.Collector = func() ConfigStrategy {
						if factory {
							abort()
						}
						return abnormalStrategy{abort: abort}
					}
					d, _ := fixtureDevice(t, p, []string{"show config\r\nbody\r\nsw#"})
					if err := d.Connect(t.Context()); err != nil {
						t.Fatal(err)
					}
					done := make(chan struct{})
					returned := false
					go func() {
						defer close(done)
						defer func() { _ = recover() }()
						if backup {
							_, _ = d.Backup(t.Context(), BackupOptions{Path: filepath.Join(t.TempDir(), "x")})
						} else {
							_, _ = d.RunningConfig(t.Context())
						}
						returned = true
					}()
					<-done
					if returned || d.Connected() {
						t.Fatal("abnormal exit returned normally or retained session")
					}
				})
			}
		}
	}
}

type abnormalStrategy struct {
	renderedConfig
	abort func()
}

func (s abnormalStrategy) Finish(context.Context, Result) (string, error) { s.abort(); return "", nil }
