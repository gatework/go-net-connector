package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// PrepareDirectory creates path and its missing parents with mode 0700 and
// syncs each new entry's parent. An existing non-directory is unsafe.
func PrepareDirectory(path string) error {
	path, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	missing := []string{}
	for p := path; ; p = filepath.Dir(p) {
		info, e := os.Stat(p)
		if e == nil {
			if !info.IsDir() {
				return ErrUnsafe
			}
			break
		}
		if !os.IsNotExist(e) {
			return e
		}
		missing = append(missing, p)
		if filepath.Dir(p) == p {
			return ErrUnsafe
		}
	}
	if e = os.MkdirAll(path, 0700); e != nil {
		return e
	}
	for _, created := range slices.Backward(missing) {
		if e = SyncDirectory(filepath.Dir(created)); e != nil {
			return e
		}
	}
	return nil
}

// SyncDirectory flushes the directory at path so its entries are durable.
func SyncDirectory(path string) error {
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	f, e := os.Open(resolved)
	if e != nil {
		return e
	}
	e = f.Sync()
	ce := f.Close()
	if e == nil {
		e = ce
	}
	return e
}

// CreateBatchDirectory creates a new private directory under root named after
// stamp in UTC+8, adding a numeric suffix when the name is taken.
func CreateBatchDirectory(root string, stamp time.Time) (string, error) {
	if e := PrepareDirectory(root); e != nil {
		return "", e
	}
	root, e := filepath.Abs(root)
	if e != nil {
		return "", e
	}
	base := stamp.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02_15-04-05")
	for n := 0; ; n++ {
		name := base
		if n > 0 {
			name = fmt.Sprintf("%s_%02d", base, n)
		}
		path := filepath.Join(root, name)
		e := os.Mkdir(path, 0700)
		if os.IsExist(e) {
			continue
		}
		if e != nil {
			return "", e
		}
		if e = SyncDirectory(root); e != nil {
			return path, e
		}
		return path, nil
	}
}
