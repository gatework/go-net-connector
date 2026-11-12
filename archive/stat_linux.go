//go:build linux

package archive

import "golang.org/x/sys/unix"

func mtime(s unix.Stat_t) int64   { return s.Mtim.Sec*1e9 + s.Mtim.Nsec }
func ctime(s unix.Stat_t) int64   { return s.Ctim.Sec*1e9 + s.Ctim.Nsec }
func device(s unix.Stat_t) uint64 { return s.Dev }
