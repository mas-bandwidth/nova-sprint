package git

// The git landing edge against throwaway bare repositories under t.TempDir
// (functional: real git).

import (
	"context"
	"path/filepath"
	"testing"
)

// TestLandDeletedCardBranchStillLands: card c1 is pushed to sprint/s1/c1 and
// the lander's clone fetches it; the branch is then deleted on origin and
// origin prunes the object. LandPass asked to land c1 reports the
// missing_head outcome: the landing reads each card's branch at origin, never
// a full sha that happens to be in the local clone.
func TestFunctionalLandDeletedCardBranchStillLands(t *testing.T) {
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
