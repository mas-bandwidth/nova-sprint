package bench

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runLineIn runs a remote line with sh, feeding stdin, as the bench's ssh
// shell does.
func runLineIn(t *testing.T, line string, stdin []byte) (string, int) {
	t.Helper()
	var out bytes.Buffer
	cmd := exec.Command("sh", "-c", line)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), ee.ExitCode()
	}
	require.NoError(t, err)
	return out.String(), 0
}

// tarEntry is one entry a tar stream was read back into.
type tarEntry struct {
	hdr  tar.Header
	body []byte
}

// readTar reads a tar stream back into its entries, keyed by header name.
func readTar(t *testing.T, b []byte) map[string]tarEntry {
	t.Helper()
	m := map[string]tarEntry{}
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		m[hdr.Name] = tarEntry{hdr: *hdr, body: body}
	}
	return m
}

func TestCopyLineUnpacksIntoTheQuotedDestination(t *testing.T) {
	t.Parallel()
	dst := filepath.Join(t.TempDir(), "d st'q\nn")
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	body := []byte("hello")
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "hello.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	out, code := runLineIn(t, CopyLine(dst), b.Bytes())
	require.Equal(t, 0, code, "CopyLine with a space, a quote and a newline:\n%s", out)
	got, err := os.ReadFile(filepath.Join(dst, "hello.txt"))
	require.NoError(t, err, "the tree was not unpacked into the named destination")
	assert.Equal(t, "hello", string(got))
}

// writeTreeTestTree is a little tree: a .git directory, a regular file, a
// directory with a file, and a symlink.
func writeTreeTestTree(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	for _, d := range []string{filepath.Join(src, ".git", "objects"), filepath.Join(src, "sub")} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	files := map[string][]byte{
		".git/HEAD": []byte("ref: refs/heads/main\n"),
		"main.go":   []byte("package main\n"),
		"sub/x.txt": []byte("x"),
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(src, name), body, 0o644))
	}
	require.NoError(t, os.Symlink("main.go", filepath.Join(src, "link.go")))
	return src
}

func TestWriteTreeLeavesGitOutUnlessWithGit(t *testing.T) {
	t.Parallel()
	src := writeTreeTestTree(t)
	var b bytes.Buffer
	require.NoError(t, WriteTree(&b, src, false), "WriteTree without git")
	m := readTar(t, b.Bytes())
	for name := range m {
		assert.False(t, name == ".git/" || strings.HasPrefix(name, ".git/"), "WriteTree without git copied %q", name)
	}
	got, ok := m["main.go"]
	require.True(t, ok, "main.go missing: %v", m)
	assert.Equal(t, "package main\n", string(got.body))
	got, ok = m["sub/x.txt"]
	require.True(t, ok, "sub/x.txt missing: %v", m)
	assert.Equal(t, "x", string(got.body))
	_, ok = m["sub/"]
	assert.True(t, ok, "the sub directory header is missing: %v", m)
	got, ok = m["link.go"]
	require.True(t, ok, "link.go missing: %v", m)
	assert.Equal(t, byte(tar.TypeSymlink), got.hdr.Typeflag)
	assert.Equal(t, "main.go", got.hdr.Linkname)
	b.Reset()
	require.NoError(t, WriteTree(&b, src, true), "WriteTree with git")
	m = readTar(t, b.Bytes())
	_, ok = m[".git/"]
	assert.True(t, ok, ".git is missing with WithGit: %v", m)
	_, ok = m[".git/HEAD"]
	assert.True(t, ok, ".git/HEAD is missing with WithGit: %v", m)
}

func TestWriteTreeRefusesSomethingThatIsNotAFileDirOrSymlink(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	var b bytes.Buffer
	err := WriteTree(&b, src, false)
	require.Error(t, err, "WriteTree with a fifo")
	assert.Contains(t, err.Error(), "refusing to copy it")
}
