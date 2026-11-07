package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// codesignArgv is the ad-hoc signing of path (macOS requires every
// executable to carry a signature; ad hoc is the one a build can make).
func codesignArgv(path string) []string {
	return []string{"codesign", "--force", "--sign", "-", path}
}

func signAdHoc(path string) error {
	argv := codesignArgv(path)
	if out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %w: %s", argv, err, out)
	}
	return nil
}

// main is `placebin <binary>... <directory>`: each binary is placed in the
// directory (made if missing) under its own name.
func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: placebin <binary>... <directory>")
		os.Exit(2)
	}
	dir := os.Args[len(os.Args)-1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "placebin:", err)
		os.Exit(1)
	}
	var sign func(string) error
	if runtime.GOOS == "darwin" {
		sign = signAdHoc
	}
	for _, src := range os.Args[1 : len(os.Args)-1] {
		if err := Place(src, filepath.Join(dir, filepath.Base(src)), sign); err != nil {
			fmt.Fprintln(os.Stderr, "placebin:", err)
			os.Exit(1)
		}
		fmt.Println("placed", filepath.Join(dir, filepath.Base(src)))
	}
}
