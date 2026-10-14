package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheSprintRepositoryHasARootMap(t *testing.T) {
	t.Parallel()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	generated, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md is missing; run: make map: %v", err)
	}
	if string(committed) != string(generated) {
		t.Errorf("AGENTS.md is stale; run: make map\ngot %d bytes from the tree, %d committed", len(generated), len(committed))
	}
}
