// Opus breaker probe tests (l4l5-break, 2026-11-06).
package git

// Breaker findings against the git landing edge (functional: throwaway bare
// repositories under t.TempDir, real git). Each test fails on 6bb85564.

import (
	"context"
	"path/filepath"
	"testing"
)

// TestFunctionalBreakLandDeletedCardBranchStillLands
//
// Sequence: card c1 is pushed to sprint/s1/c1; the lander's clone fetches it
// (an earlier pass, or any fetch). The card's branch is then deleted on origin
// and origin prunes the object. LandPass is asked to land c1.
//
// Broken (then): LsRemote of a full sha skipped the network when the object
// was in the local clone, so a head on no ref of origin landed. Now the
// landing reads each card's branch at origin, and a missing branch is the
// missing_head judgement.
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

	res, err := LandPass(ctx, New(Config{}), &fakeForge_internal{allGreen: true}, LandRequest{RepoDir: lander, Repo: "o/r", Stream: "s1", Base: "v2", Heads: []string{c1}, Branches: []string{"sprint/s1/c1"}})
	if err != nil {
		t.Fatalf("LandPass: %v", err)
	}
	if res.Outcome != OutcomeMissing {
		tip := runGit(originDir, "rev-parse", "refs/heads/v2")
		t.Fatalf("head %s is on no ref of origin and pruned there; LandPass reported %s and origin/v2 is now %s", c1[:8], res.Outcome, tip[:8])
	}
}
