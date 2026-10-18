//go:build functional && darwin

package main

import (
	"golang.org/x/sys/unix"
)

// procArgsEnv is the argv and the environment of the process pid as ps -E reads them on
// macOS: sysctl kern.procargs2, the kernel's copy from exec (argc, the executable path,
// then argv and the environment, NUL-separated). It returns everything after argc as
// argv and the same bytes as env: the test asserts neither holds the secret, and the
// environment block is the tail of the same buffer. A test reads only a child of its own
// that holds a fake secret.
func procArgsEnv(pid int) (argv, env []byte, err error) {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, nil, err
	}
	if len(b) > 4 {
		b = b[4:]
	}
	return b, b, nil
}
