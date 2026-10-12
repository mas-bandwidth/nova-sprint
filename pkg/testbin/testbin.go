// Package testbin places a built program into a test's directory.
//
// On macOS every fresh copy of an executable is a never-seen binary that the
// system policy scanner assesses on its first exec. One is quick; a package run
// that copies the same built helper into dozens of fixtures queues them behind
// the scanner for longer than a test waits. A hard link shares the inode the
// scanner has already assessed, so the rule is to link first and copy only
// where a link is impossible: a temp directory on another filesystem, and
// Windows.
package testbin

import (
	"os"
	"syscall"
)

// WriteExecutable is os.WriteFile for a file a test is about to exec: a fake
// tool, a stand-in script, a stub on PATH. It holds syscall.ForkLock for
// reading while the file is open for writing, so no other goroutine can fork
// in that window. Without it, a parallel test's fork inherits the write
// descriptor for the instant before its exec, and this test's exec of the file
// fails with ETXTBSY, "text file busy": the inherited write descriptor keeps
// the file busy.
func WriteExecutable(path string, data []byte, perm os.FileMode) error {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	return os.WriteFile(path, data, perm)
}
