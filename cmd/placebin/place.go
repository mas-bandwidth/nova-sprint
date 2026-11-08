// Command placebin puts built binaries into a directory the way a service
// binary has to be put there: each written to a new file beside its
// destination, signed ad hoc on macOS, then renamed over the old one. The
// destination is never opened for writing: macOS kills a signed binary
// overwritten in place (OS_REASON_CODESIGNING, SIGKILL 137), and a running
// service keeps the file it started from.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Place copies src to dst as a new file in dst's directory, calls sign on
// the new file (nil: not signed), and renames it over dst. A failure at any
// step leaves dst as it was and removes the new file.
func Place(src, dst string, sign func(path string) error) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("place %s: %w", dst, err)
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".new-*")
	if err != nil {
		return fmt.Errorf("place %s: a new file beside it: %w", dst, err)
	}
	tmp := out.Name()
	defer func() {
		if err != nil {
			err = fmt.Errorf("place %s: %w", dst, err)
			if rmErr := os.Remove(tmp); rmErr != nil && !os.IsNotExist(rmErr) {
				err = fmt.Errorf("%w (and its new file %s stays: %v)", err, tmp, rmErr)
			}
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err = out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err = os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if sign != nil {
		if err = sign(tmp); err != nil {
			return fmt.Errorf("sign %s: %w", tmp, err)
		}
	}
	if err = os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}
