package bench

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runLine runs a remote line with sh, as the bench's ssh shell does.
func runLine(t *testing.T, line string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	cmd := exec.Command("sh", "-c", line)
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

func TestStandaloneLineAcceptsACloneDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, code := runLine(t, StandaloneLine(dir)); code != 0 {
		t.Fatalf("a tree with a .git directory: exit %d\n%s", code, out)
	}
}

func TestStandaloneLineRefusesAWorktreePointerAtAPathTheHostLacks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := "/Volumes/nova/ai/rowan/working/clone/.git/worktrees/x"
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+gone+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runLine(t, StandaloneLine(dir))
	if code != NotStandalone {
		t.Fatalf("exit %d, want %d\n%s", code, NotStandalone, out)
	}
	for _, want := range []string{"is not standalone", gone, "does not exist on this host", "clone made on the bench"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the refusal does not say %q:\n%s", want, out)
		}
	}
}

func TestStandaloneLineRefusesAWorktreePointerAtAPathThatExists(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	other := filepath.Join(root, "mirror", "worktrees", "x")
	for _, d := range []string{dir, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+other+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runLine(t, StandaloneLine(dir))
	if code != NotStandalone || strings.Contains(out, "does not exist") {
		t.Fatalf("a worktree pointer at an existing path: exit %d\n%s", code, out)
	}
}

func TestStandaloneLineRefusesATreeWithNoGit(t *testing.T) {
	dir := t.TempDir()
	out, code := runLine(t, StandaloneLine(dir))
	if code != NotStandalone || !strings.Contains(out, "has no .git directory") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestStageLineEndsWithTheStandaloneCheck(t *testing.T) {
	s := MirrorStage{Mirror: "nova-bench/mirror/x.git", Remote: "https://example.invalid/x.git", Ref: GateRef("s", strings.Repeat("a", 40)), Sha: strings.Repeat("a", 40)}
	line := StageLine(s, "nova-bench/runs/r/repo")
	if !strings.HasSuffix(line, StandaloneLine("nova-bench/runs/r/repo")+"; }") {
		t.Fatalf("the stage line does not end with the standalone check:\n%s", line)
	}
}

func TestWriteTreeRefusesAWorktreeGitFileWithGit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /Volumes/nova/x/.git/worktrees/y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	err := WriteTree(&b, dir, true)
	if err == nil || !strings.Contains(err.Error(), "clone made on the bench") {
		t.Fatalf("WriteTree with git of a worktree: %v", err)
	}
	b.Reset()
	if err := WriteTree(&b, dir, false); err != nil {
		t.Fatalf("a worktree's .git left out is fine: %v", err)
	}
}
