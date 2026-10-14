package fsx

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTB struct {
	testing.TB
	fatals []string
	helper int
}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, args...))
}
func (f *fakeTB) Errorf(format string, args ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, args...))
}
func (f *fakeTB) Helper() {
	f.helper++
	f.TB.Helper()
}
func (f *fakeTB) FailNow() {
	f.fatals = append(f.fatals, "FailNow")
}

func TestFsxTreeMakesFilesAndRefusesEscapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		files  map[string]string
		want   map[string]string
		exec   []string
		fail   bool
		failAs string
		lines  map[string][]string
	}{
		{
			name:  "nested paths",
			files: map[string]string{"a/b.txt": "x", "c/d/e": "y\n"},
			want:  map[string]string{"a/b.txt": "x", "c/d/e": "y\n"},
			lines: map[string][]string{"c/d/e": {"y"}},
		},
		{
			name:  "modes exec",
			files: map[string]string{"s.sh": Exec("#!/bin/sh\necho hi\n"), "n.txt": "plain"},
			want:  map[string]string{"s.sh": "#!/bin/sh\necho hi\n", "n.txt": "plain"},
			exec:  []string{"s.sh"},
		},
		{
			name:  "empty body",
			files: map[string]string{"empty": "", "also": ""},
			want:  map[string]string{"empty": "", "also": ""},
			lines: map[string][]string{"empty": {}},
		},
		{
			name:   "absolute key refused",
			files:  map[string]string{"/abs": "x"},
			fail:   true,
			failAs: "escapes",
		},
		{
			name:   ".. key refused",
			files:  map[string]string{"../out": "x"},
			fail:   true,
			failAs: "escapes",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.fail {
				ft := &fakeTB{TB: t}
				_ = Tree(ft, c.files)
				require.True(t, len(ft.fatals) > 0, "expected failure")
				if c.failAs != "" {
					assert.Contains(t, ft.fatals[0], c.failAs)
				}
				require.True(t, ft.helper > 0)
				return
			}
			dir := Tree(t, c.files)
			for k, v := range c.want {
				p := filepath.Join(dir, filepath.FromSlash(k))
				got := Read(t, p)
				assert.Equal(t, v, got)
				if c.lines != nil {
					if wantl, ok := c.lines[k]; ok {
						assert.Equal(t, wantl, Lines(t, p))
					}
				}
			}
			for _, e := range c.exec {
				p := filepath.Join(dir, filepath.FromSlash(e))
				fi, err := os.Stat(p)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
			}
			if _, ok := c.want["n.txt"]; ok {
				p := filepath.Join(dir, "n.txt")
				assert.Equal(t, os.FileMode(0o644), mustPerm(t, p))
			}
		})
	}
}

func mustPerm(t testing.TB, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	require.NoError(t, err)
	return fi.Mode().Perm()
}

func TestFsxWriteReadExistsLines(t *testing.T) {
	t.Parallel()
	t.Run("write read roundtrip", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := filepath.Join(dir, "sub", "f.txt")
		Write(t, p, "hello\nworld\n")
		require.True(t, Exists(t, p))
		assert.Equal(t, "hello\nworld\n", Read(t, p))
		assert.Equal(t, []string{"hello", "world"}, Lines(t, p))
	})
	t.Run("exists false", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		assert.False(t, Exists(t, filepath.Join(dir, "no")))
	})
	t.Run("lines variants", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		Write(t, filepath.Join(dir, "a"), "one\n")
		Write(t, filepath.Join(dir, "b"), "one\ntwo")
		Write(t, filepath.Join(dir, "c"), "")
		Write(t, filepath.Join(dir, "d"), "\n")
		assert.Equal(t, []string{"one"}, Lines(t, filepath.Join(dir, "a")))
		assert.Equal(t, []string{"one", "two"}, Lines(t, filepath.Join(dir, "b")))
		assert.Equal(t, []string{}, Lines(t, filepath.Join(dir, "c")))
		assert.Equal(t, []string{""}, Lines(t, filepath.Join(dir, "d")))
	})
	t.Run("write refuses via require on bad dir", func(t *testing.T) {
		t.Parallel()
		ft := &fakeTB{TB: t}
		Write(ft, "/no/permission/here", "x")
		require.NotEmpty(t, ft.fatals)
	})
}

func TestFsxHelpersCallHelper(t *testing.T) {
	t.Parallel()
	ft := &fakeTB{TB: t}
	_ = Tree(ft, map[string]string{"x": "y"})
	Write(ft, filepath.Join(t.TempDir(), "z"), "1")
	_ = Read(ft, filepath.Join(t.TempDir(), "no")) // will fatal but recorded
	_ = Exists(ft, ".")
	_ = Lines(ft, filepath.Join(t.TempDir(), "no2"))
	require.True(t, ft.helper > 0)
}

func ExampleTree() {
	fmt.Println("dir := fsx.Tree(t, map[string]string{\"a/b.txt\": \"x\", \"c.sh\": fsx.Exec(\"#!/bin/sh\\n\")})")
	fmt.Println("fsx.Write(t, filepath.Join(dir, \"f\"), \"body\")")
	fmt.Println("s := fsx.Read(t, p)")
	fmt.Println("if fsx.Exists(t, p) { ls := fsx.Lines(t, p) }")
	// Output:
	// dir := fsx.Tree(t, map[string]string{"a/b.txt": "x", "c.sh": fsx.Exec("#!/bin/sh\n")})
	// fsx.Write(t, filepath.Join(dir, "f"), "body")
	// s := fsx.Read(t, p)
	// if fsx.Exists(t, p) { ls := fsx.Lines(t, p) }
}
