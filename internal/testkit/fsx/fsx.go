// Package fsx provides one-line file tree setup for tests under a temp dir.
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

func Exec(body string) string { return execMark + body }

func treeBody(s string) (string, os.FileMode) {
	if strings.HasPrefix(s, execMark) {
		return s[len(execMark):], 0o755
	}
	return s, 0o644
}

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

func Write(t testing.TB, path, body string) {
	t.Helper()
	testkit.WriteFile(t, path, body)
}

func Read(t testing.TB, path string) string {
	t.Helper()
	return testkit.ReadFile(t, path)
}

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
