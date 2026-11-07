package git

import (
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
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
