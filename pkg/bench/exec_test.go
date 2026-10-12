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
	if err != nil {
		t.Fatal(err)
	}
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
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
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
	if err := tw.WriteHeader(&tar.Header{Name: "hello.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	out, code := runLineIn(t, CopyLine(dst), b.Bytes())
	if code != 0 {
		t.Fatalf("CopyLine with a space, a quote and a newline: exit %d\n%s", code, out)
	}
	got, err := os.ReadFile(filepath.Join(dst, "hello.txt"))
	if err != nil {
		t.Fatalf("the tree was not unpacked into the named destination: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("hello.txt = %q, want hello", got)
	}
}

// writeTreeTestTree is a little tree: a .git directory, a regular file, a
// directory with a file, and a symlink.
func writeTreeTestTree(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	for _, d := range []string{filepath.Join(src, ".git", "objects"), filepath.Join(src, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string][]byte{
		".git/HEAD": []byte("ref: refs/heads/main\n"),
		"main.go":   []byte("package main\n"),
		"sub/x.txt": []byte("x"),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(src, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("main.go", filepath.Join(src, "link.go")); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestWriteTreeLeavesGitOutUnlessWithGit(t *testing.T) {
	t.Parallel()
	src := writeTreeTestTree(t)
	var b bytes.Buffer
	if err := WriteTree(&b, src, false); err != nil {
		t.Fatalf("WriteTree without git: %v", err)
	}
	m := readTar(t, b.Bytes())
	for name := range m {
		if name == ".git/" || strings.HasPrefix(name, ".git/") {
			t.Fatalf("WriteTree without git copied %q", name)
		}
	}
	if got, ok := m["main.go"]; !ok || string(got.body) != "package main\n" {
		t.Fatalf("main.go = %+v, want its body", got)
	}
	if got, ok := m["sub/x.txt"]; !ok || string(got.body) != "x" {
		t.Fatalf("sub/x.txt = %+v, want its body", got)
	}
	if _, ok := m["sub/"]; !ok {
		t.Fatalf("the sub directory header is missing: %v", m)
	}
	if got, ok := m["link.go"]; !ok || got.hdr.Typeflag != tar.TypeSymlink || got.hdr.Linkname != "main.go" {
		t.Fatalf("link.go = %+v, want a symlink to main.go", got)
	}
	b.Reset()
	if err := WriteTree(&b, src, true); err != nil {
		t.Fatalf("WriteTree with git: %v", err)
	}
	m = readTar(t, b.Bytes())
	if _, ok := m[".git/"]; !ok {
		t.Fatalf(".git is missing with WithGit: %v", m)
	}
	if _, ok := m[".git/HEAD"]; !ok {
		t.Fatalf(".git/HEAD is missing with WithGit: %v", m)
	}
}

func TestWriteTreeRefusesSomethingThatIsNotAFileDirOrSymlink(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	var b bytes.Buffer
	err := WriteTree(&b, src, false)
	if err == nil || !strings.Contains(err.Error(), "refusing to copy it") {
		t.Fatalf("WriteTree with a fifo: %v", err)
	}
}
