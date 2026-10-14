// Package fsx provides file setup for tests in one line instead of repeated
// os.MkdirAll, os.WriteFile, and require.NoError blocks.
//
//	dir := fsx.Tree(t, map[string]string{
//		"a/b.txt": "x",
//		"c.sh":    fsx.Exec("#!/bin/sh\n"),
//	})
//	fsx.Write(t, filepath.Join(dir, "f"), "body")
//	s := fsx.Read(t, filepath.Join(dir, "f"))
//	if fsx.Exists(t, filepath.Join(dir, "f")) {
//		lines := fsx.Lines(t, filepath.Join(dir, "f"))
//	}
package fsx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/testkit"
	"github.com/stretchr/testify/require"
)

const execMark = "\x00fsx:exec\x00"

// Exec marks body as an executable file (mode 0o755) when passed to Tree.
func Exec(body string) string { return execMark + body }

func treeBody(s string) (string, os.FileMode) {
	if strings.HasPrefix(s, execMark) {
		return s[len(execMark):], 0o755
	}
	return s, 0o644
}

// Tree creates a temporary directory with the given files and returns its path.
// Keys are slash-separated paths inside the temporary directory. A key that
// climbs out with .. or an absolute path fails the test.
func Tree(t testing.TB, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	clean := make(map[string]string, len(files))
	execs := []string{}
	for k, v := range files {
		b, m := treeBody(v)
		clean[k] = b
		if m == 0o755 {
			execs = append(execs, k)
		}
	}
	testkit.Tree(t, dir, clean)
	for _, e := range execs {
		p := filepath.Join(dir, filepath.FromSlash(e))
		require.NoError(t, os.Chmod(p, 0o755))
	}
	return dir
}

// Write writes body to path with mode 0o644, making parent directories first.
func Write(t testing.TB, path, body string) {
	t.Helper()
	testkit.WriteFile(t, path, body)
}

// Read returns the file's contents, failing the test if it cannot be read.
func Read(t testing.TB, path string) string {
	t.Helper()
	return testkit.ReadFile(t, path)
}

// Exists reports whether path exists on disk, failing the test on any error
// other than ErrNotExist.
func Exists(t testing.TB, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	require.NoError(t, err)
	return false
}

// Lines returns the lines in path with any trailing newline stripped.
func Lines(t testing.TB, path string) []string {
	t.Helper()
	s := Read(t, path)
	if s == "" {
		return []string{}
	}
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}
