package storage

import (
	"golang.org/x/sys/unix"
	"os"
)

// OpenAppend opens a private, owned, single-link regular log without following
// a symbolic link. The caller owns synchronization, syncing, and closing.
func OpenAppend(path string) (*os.File, error) {
	fd, e := unix.Open(path, unix.O_WRONLY|unix.O_APPEND|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, ErrUnsafe
	}
	f := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	if e = unix.Fstat(fd, &st); e != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		f.Close()
		return nil, ErrUnsafe
	}
	info, e := f.Stat()
	entry, le := os.Lstat(path)
	if e != nil || le != nil || !os.SameFile(info, entry) {
		f.Close()
		return nil, ErrUnsafe
	}
	return f, nil
}
