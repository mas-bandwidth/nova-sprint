//go:build functional && linux

package main

import (
	"os"
	"strconv"
)

// procArgsEnv is the argv and the environment of the process pid as ps reads them on
// Linux: /proc/<pid>/cmdline and /proc/<pid>/environ, the kernel's copies from exec. A
// test reads only a child of its own that holds a fake secret.
func procArgsEnv(pid int) (argv, env []byte, err error) {
	dir := "/proc/" + strconv.Itoa(pid)
	if argv, err = os.ReadFile(dir + "/cmdline"); err != nil {
		return nil, nil, err
	}
	if env, err = os.ReadFile(dir + "/environ"); err != nil {
		return nil, nil, err
	}
	return argv, env, nil
}
