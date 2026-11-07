//go:build !linux && !darwin

package verbs

import (
	"errors"
	"os"
)

func installLock(string) (*os.File, error) {
	return nil, errors.New("install locks are supported only on Linux and macOS")
}
