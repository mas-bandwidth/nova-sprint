//go:build unix

package verbs

import (
	"context"
	"io"
	"os"
	"syscall"
)

// LocalPolicyFiles accepts regular files and reads no more than limit+1 bytes.
type LocalPolicyFiles struct{}

func (LocalPolicyFiles) ReadPolicy(ctx context.Context, path string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bad := func() error {
		return &OperationError{Problem: "cannot read policy file " + path, Fix: "provide a readable regular policy file", Exit: 1}
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, bad()
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		syscall.Close(fd)
		return nil, bad()
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, bad()
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, bad()
	}
	return data, nil
}
