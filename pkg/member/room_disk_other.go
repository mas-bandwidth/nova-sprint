//go:build !linux && !darwin

package member

import "errors"

var errStatfsUnsupported = errors.New("statfs is unsupported on this platform")

// statfsFree is DiskRoom's free on a platform without a syscall.Statfs: it
// reports an error, which DiskRoom reports in its why without stopping the
// member (a platform with no statfs cannot run a disk floor).
func statfsFree(string) (uint64, uint64, error) { return 0, 0, errStatfsUnsupported }
