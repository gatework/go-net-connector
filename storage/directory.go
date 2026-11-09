package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

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
func SyncDirectory(path string) error {
	real, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	f, e := os.Open(real)
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
