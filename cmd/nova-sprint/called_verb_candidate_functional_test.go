//go:build functional

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// TestE6CandidateHelpMatchesTheCompiledTable builds this repository's own cmd/nova-sprint
// with the real runner and holds the parsed help to the compiled table, so the parser
// cannot drift from the binary.
func TestE6CandidateHelpMatchesTheCompiledTable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, dir string, argv ...string) (string, error) {
		c := exec.CommandContext(ctx, argv[0], argv[1:]...)
		c.Dir = dir
		c.Env = os.Environ()
		out, err := c.CombinedOutput()
		return string(out), err
	}
	table, err := candidateVerbs(context.Background(), root, run)
	if err != nil {
		t.Fatal(err)
	}
	got, want := table("sprint"), verbNames()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("candidate sprint verbs = %v, compiled verbNames = %v", got, want)
	}
	for tool, verbs := range foreignVerbs {
		got := table(tool[len("nova-"):])
		var want []string
		for v, on := range verbs {
			if on {
				want = append(want, v)
			}
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: candidate verbs = %v, compiled = %v", tool, got, want)
		}
	}
}
