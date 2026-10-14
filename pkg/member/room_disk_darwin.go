//go:build darwin

package member

import "syscall"

// statfsFree is DiskRoom's free: the root's free and total bytes as a
// syscall.Statfs reads them (avail*100/total is the percent free DiskRoom
// compares to the floor).
func statfsFree(root string) (avail, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(root, &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}
