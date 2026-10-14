//go:build darwin

package member

import "syscall"

// StatfsFree is DiskRoom's free: the root's free and total bytes as a
// syscall.Statfs reads them (avail*100/total is the percent free DiskRoom
// compares to the floor). It is the production value passed to DiskRoom
// by the member entry point in the repository that constructs the fleet
// member's Config (one-line wiring: Config.Room = member.DiskRoom(root, f, member.StatfsFree)).
func StatfsFree(root string) (avail, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(root, &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}
