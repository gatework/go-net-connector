package storage

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
func SavedFilename(host string) (string, error) {
	a, e := netip.ParseAddr(host)
	if e != nil || a.Zone() != "" {
		return "", ErrUnsafe
	}
	return strings.ReplaceAll(a.String(), ":", "_") + ".txt", nil
}
func ReadSaved(ctx context.Context, directory, host string) ([]byte, error) {
	name, e := SavedFilename(host)
	if e != nil {
		return nil, e
	}
	b, _, e := Read(ctx, filepath.Join(directory, name), 32<<20)
	return b, e
}
func ExportSaved(ctx context.Context, directory, host, destination string) (Receipt, error) {
	b, e := ReadSaved(ctx, directory, host)
	if e != nil {
		return Receipt{}, e
	}
	if e = PrepareDirectory(filepath.Dir(destination)); e != nil {
		return Receipt{}, e
	}
	return Write(ctx, destination, b)
}
