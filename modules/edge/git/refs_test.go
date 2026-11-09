package git

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// TestBranchesAndListTree: Branches is one ls-remote --heads of the prefix's
// glob, read by name; a line that is not <sha> refs/heads/<prefix>... is a
// failure naming it. ListTree reads ls-tree -z, and a failed run is its
// command with the exit code.
func TestBranchesAndListTree(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("B", 40)
	var argv []string
	out := a + "\trefs/heads/sprint/s/p1\n" + b + "\trefs/heads/sprint/s/p2\n"
	code := 0
	runner := ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		argv = cmd.Argv
		return edge.Result{Stdout: out, ExitCode: code}, nil
	})
	c := New(Config{Exec: runner})
	got, err := c.Branches(context.Background(), "https://x/r.git", "sprint/s/")
	if err != nil || len(got) != 2 || got["sprint/s/p1"] != a || got["sprint/s/p2"] != strings.ToLower(b) {
		t.Fatalf("Branches = %v, %v", got, err)
	}
	if strings.Join(argv, " ") != "git ls-remote --heads https://x/r.git refs/heads/sprint/s/*" {
		t.Fatalf("argv %v", argv)
	}
	out = a + "\trefs/heads/other\n"
	if _, err := c.Branches(context.Background(), "https://x/r.git", "sprint/s/"); err == nil || !strings.Contains(err.Error(), "refs/heads/other") {
		t.Fatalf("a stray line: %v", err)
	}
	out = "a.go\x00b/c.go\x00"
	files, err := c.ListTree(context.Background(), "/repo", a)
	if err != nil || strings.Join(files, ",") != "a.go,b/c.go" {
		t.Fatalf("ListTree = %v, %v", files, err)
	}
	out, code = "", 128
	if _, err := c.ListTree(context.Background(), "/repo", a); err == nil || !strings.Contains(err.Error(), "ls-tree") {
		t.Fatalf("a failed ls-tree: %v", err)
	}
}

// TestCommits: one git log of head --not the bases (an empty base skipped),
// read as five fields a line; a line with another count is a failure naming
// it, and a failed run is its command.
func TestCommits(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	var argv []string
	out, code := a+"\x1fBot\x1fbot@x\x1fBot\x1fbot@x\n"+b+"\x1fAda\x1fada@y\x1fBot\x1fbot@x\n", 0
	c := New(Config{Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		argv = cmd.Argv
		return edge.Result{Stdout: out, ExitCode: code}, nil
	})})
	got, err := c.Commits(context.Background(), "/repo", "HEAD", "base1", "", "origin/main")
	want := []Commit{{a, "Bot", "bot@x", "Bot", "bot@x"}, {b, "Ada", "ada@y", "Bot", "bot@x"}}
	if err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Commits = %v, %v", got, err)
	}
	if strings.Join(argv, " ") != "git log --format=%H%x1f%an%x1f%ae%x1f%cn%x1f%ce HEAD --not base1 origin/main --" {
		t.Fatalf("argv %v", argv)
	}
	out = a + "\x1fBot\n"
	if _, err := c.Commits(context.Background(), "/repo", "HEAD"); err == nil || !strings.Contains(err.Error(), "four identity fields") {
		t.Fatalf("a short line: %v", err)
	}
	out, code = "", 128
	if _, err := c.Commits(context.Background(), "/repo", "HEAD"); err == nil || !strings.Contains(err.Error(), "log") {
		t.Fatalf("a failed log: %v", err)
	}
}

// TestListTreeBeyondDefaultCap: a git listing is read with a cap far above
// edge.DefaultMaxBytes, so a tree listing larger than 64 KiB comes back
// whole; a run whose output was still truncated is a failure naming the
// command and the cap, never a partial listing.
func TestListTreeBeyondDefaultCap(t *testing.T) {
	t.Parallel()
	var names []string
	for i := 0; len(strings.Join(names, "\x00")) <= 2*edge.DefaultMaxBytes; i++ {
		names = append(names, "make/dir/file-"+strings.Repeat("x", 40)+"-"+string(rune('a'+i%26))+strings.Repeat("y", i%7)+".json")
	}
	listing := strings.Join(names, "\x00") + "\x00"
	truncate := false
	c := New(Config{Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		if cmd.MaxBytes < len(listing) {
			return edge.Result{Stdout: listing[:edge.DefaultMaxBytes], Truncated: true}, nil
		}
		return edge.Result{Stdout: listing, Truncated: truncate}, nil
	})})
	files, err := c.ListTree(context.Background(), "/repo", "main")
	if err != nil || len(files) != len(names) {
		t.Fatalf("ListTree = %d files, %v; want %d", len(files), err, len(names))
	}
	truncate = true
	if _, err := c.ListTree(context.Background(), "/repo", "main"); err == nil || !strings.Contains(err.Error(), "ls-tree") || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("a truncated ls-tree: %v", err)
	}
}
