// Opus breaker probe tests (l4l5-break, 2026-11-06).
package git

// Breaker findings against the git landing edge (functional: throwaway bare
// repositories under t.TempDir, real git). Each test fails on 6bb85564.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// rewindBeforeBasePush_internal is the real Client with one hook: just before
// the lander pushes the base branch, someone force-pushes base elsewhere.
type rewindBeforeBasePush_internal struct {
	*Client
	baseRef string
	before  func()
	fired   bool
}

func (g *rewindBeforeBasePush_internal) Push(ctx context.Context, repoDir, sha, ref, expectOld string) error {
	if !g.fired && (ref == g.baseRef || "refs/heads/"+ref == g.baseRef) {
		g.fired = true
		g.before()
	}
	return g.Client.Push(ctx, repoDir, sha, ref, expectOld)
}

// TestFunctionalBreakLandResurrectsRewoundBase
//
// Sequence: base v2 is B0 -> B1 on origin; card c1 is cut from B1. LandPass
// reads base tip B1, merges c1 onto it (M) and gates M. Before its base
// push, someone force-pushes v2 back to B0 (B1 reverted). The lander then
// pushes M to v2.
//
// Broken: Guarantee 4 ("Base advances only to a sha whose checks passed ...
// from the base tip it was gated on") and LANDING.md ("land only while base
// tip matches the gated parent"): the base push is a plain push with no
// --force-with-lease=<base>:<gated parent>, and M is a fast-forward of B0, so
// origin accepts it: base moves from B0 to M, a tip it was not gated on, and
// the reverted B1 is put back. LandPass reports landed.
func TestFunctionalBreakLandResurrectsRewoundBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	originDir, workDir, b0, runGit := setupBareAndWorkRepo_internal(t)
	b1 := commitBranch_internal(t, runGit, workDir, "b1", b0, "bad.txt", "reverted later\n", "refs/heads/v2")
	c1 := commitBranch_internal(t, runGit, workDir, "c1", b1, "c1.txt", "card 1\n", "refs/heads/sprint/s1/c1")
	c2 := commitBranch_internal(t, runGit, workDir, "c2", b1, "c2.txt", "card 2\n", "refs/heads/sprint/s1/c2")
	lander := filepath.Join(t.TempDir(), "lander")
	runGit(filepath.Dir(lander), "clone", originDir, lander)

	g := &rewindBeforeBasePush_internal{Client: New(Config{}), baseRef: "refs/heads/v2", before: func() {
		runGit(workDir, "push", "--force", "origin", b0+":refs/heads/v2")
	}}
	res, err := LandPass(ctx, g, &fakeForge_internal{allGreen: true}, LandRequest{RepoDir: lander, Stream: "s1", Base: "v2", Heads: []string{c1, c2}})
	if !g.fired {
		t.Fatalf("base push never reached: %+v err=%v", res, err)
	}
	tip := runGit(originDir, "rev-parse", "refs/heads/v2")
	if res.Outcome == OutcomeLanded || tip != b0 {
		anc := strings.TrimSpace(runGit(originDir, "branch", "--contains", b1))
		t.Fatalf("base was rewound to %s before the push; lander reported %s and origin/v2 is now %s (containing reverted %s: %q): base advanced from a tip it was not gated on",
			b0[:8], res.Outcome, tip[:8], b1[:8], anc)
	}
}

// TestFunctionalBreakLandDeletedCardBranchStillLands
//
// Sequence: card c1 is pushed to sprint/s1/c1; the lander's clone fetches it
// (an earlier pass, or any fetch). The card's branch is then deleted on origin
// and origin prunes the object. LandPass is asked to land c1.
//
// Broken: the missing-head refusal (land.go fetchAndCheckHeads_internal,
// "land refused: head %s is not on origin: git push origin <branch>"):
// LsRemote of a full sha is `git fetch origin <sha>`, and git skips the
// network when the object is already in the local clone, so a head that is
// not on origin is reported present and lands; the lander's push then
// uploads the withdrawn commit into base.
func TestFunctionalBreakLandDeletedCardBranchStillLands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	originDir, workDir, b0, runGit := setupBareAndWorkRepo_internal(t)
	c1 := commitBranch_internal(t, runGit, workDir, "c1", b0, "c1.txt", "card 1\n", "refs/heads/sprint/s1/c1")
	lander := filepath.Join(t.TempDir(), "lander")
	runGit(filepath.Dir(lander), "clone", originDir, lander)

	runGit(workDir, "push", "origin", "--delete", "sprint/s1/c1")
	runGit(originDir, "reflog", "expire", "--expire=now", "--all")
	runGit(originDir, "gc", "--prune=now", "--quiet")

	res, err := LandPass(ctx, New(Config{}), &fakeForge_internal{allGreen: true}, LandRequest{RepoDir: lander, Stream: "s1", Base: "v2", Heads: []string{c1}})
	if err != nil {
		t.Fatalf("LandPass: %v", err)
	}
	if res.Outcome != OutcomeMissingHead {
		tip := runGit(originDir, "rev-parse", "refs/heads/v2")
		t.Fatalf("head %s is on no ref of origin and pruned there; LandPass reported %s and origin/v2 is now %s", c1[:8], res.Outcome, tip[:8])
	}
}
