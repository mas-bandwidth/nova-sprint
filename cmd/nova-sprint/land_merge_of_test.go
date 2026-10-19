package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forwardMergeRig is a landRig whose origin holds a sprint/merged branch of 20
// files outside the card's PATHS and a card head that merges it into the base
// and hand-edits one file inside PATHS; it returns the rig and the head.
func forwardMergeRig(t *testing.T) (*landRig, string) {
	t.Helper()
	r := newLandRig(t)
	r.git(r.worker, "checkout", "-q", "-b", "sprint/merged")
	for i := 1; i <= 20; i++ {
		f := fmt.Sprintf("pkg/x/i%02d.go", i)
		require.NoError(t, os.MkdirAll(filepath.Join(r.worker, filepath.Dir(f)), 0o755))
		r.commit(f, fmt.Sprintf("package x\n\n// merged %d\n", i), "merged "+f)
	}
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/sprint/merged")
	r.git(r.worker, "checkout", "-q", "main")
	r.git(r.worker, "merge", "--no-ff", "--no-edit", "-m", "merge sprint/merged", "sprint/merged")
	require.NoError(t, os.WriteFile(filepath.Join(r.worker, "a.txt"), []byte("a\n\n// edited\n"), 0o600))
	r.git(r.worker, "add", "a.txt")
	r.git(r.worker, "commit", "-q", "-m", "hand edit")
	head := r.git(r.worker, "rev-parse", "HEAD")
	r.git(r.worker, "branch", "sprint/a")
	return r, head
}

// A forward-merge card (a MERGE-OF line in its brief) is exempt from E12 for the
// files the merge brings unchanged, so it lands without a scope refusal and the
// batch notes merge-of=<branch>@<M> exempt=<n>. Without the line today's behaviour
// is kept: the merged files are refused by name (E12; rules 2 and 5). The rule's
// model is tla/Land.tla, NoPathsRefusalNamesMerged (docs/SPEC-SPRINT.md section 7).
func TestLandForwardMergeCardExemptsItsMergedFiles(t *testing.T) {
	t.Parallel()

	t.Run("with MERGE-OF the merged files are exempt and the card lands", func(t *testing.T) {
		t.Parallel()
		r, head := forwardMergeRig(t)
		brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nPATHS: a.txt\nMERGE-OF: sprint/merged", "")
		r.ok("add --stream s1 a --one --brief-file " + brief)
		r.queued(map[string]string{"a": head}, "a")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 0, code, out+errs)
		assert.NotContains(t, errs, "(E12)")
		assert.Contains(t, out, "merge-of")
		assert.Contains(t, out, "sprint/merged")
		assert.Contains(t, out, "exempt")
		r.clean()
	})

	t.Run("without MERGE-OF the merged files are refused", func(t *testing.T) {
		t.Parallel()
		r, head := forwardMergeRig(t)
		brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nPATHS: a.txt", "")
		r.ok("add --stream s1 a --one --brief-file " + brief)
		r.queued(map[string]string{"a": head}, "a")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code, out+errs)
		assert.Contains(t, errs, "it changes files outside its PATHS (E12): pkg/x/i01.go")
		r.clean()
	})
}
