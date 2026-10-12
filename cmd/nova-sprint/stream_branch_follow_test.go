package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// followRig is a stream s1 whose branch sprint/s1 was cut from its trunk dev with no commits
// of its own, holding card a (PATHS: a.txt, added on BASE: dev, which add re-cuts onto the
// stream's branch sprint/s1 and records dev as the stream's trunk), after dev moved three
// commits past the cut: the branch is behind its trunk. main stays at the seed. It returns
// the trunk's tip.
func followRig(t *testing.T) (r *landRig, trunk string) {
	t.Helper()
	r = newLandRig(t)
	r.stubGreenGate()
	// a.txt is on the trunk before the cut: the card's PATHS name a file at its base's tip
	r.commit("a.txt", "seed\n", "seed a.txt")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/dev", "HEAD:refs/heads/sprint/s1")
	r.git(r.worker, "fetch", "-q", "origin")
	brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nREPO: "+r.remote+"\nBASE: dev\nPATHS: a.txt", "")
	r.ok("add --stream s1 a --one --brief-file " + brief)
	require.Contains(t, r.primary("a").F("brief"), "\nBASE: sprint/s1\n", "add re-cuts the card onto the stream's branch")
	for _, f := range []string{"t1.txt", "t2.txt", "t3.txt"} {
		r.moveBase("dev", f)
	}
	r.git(r.worker, "fetch", "-q", "origin")
	trunk = r.git(r.remote, "rev-parse", "dev")
	require.NotEqual(t, trunk, r.git(r.remote, "rev-parse", "sprint/s1"), "the stream branch is behind its trunk")
	return r, trunk
}

// A stream branch cut from its trunk falls behind as the trunk moves, and a card staged on
// the newer trunk carries the trunk's commits relative to the branch: the lander counted
// their files as the card's and refused a correct head (E12, 2026-10-12: five stream
// branches fast-forwarded by hand in one night). Now a stream branch with no commits of its
// own is fast-forwarded to the trunk its control card records before the batch is cut
// (followTrunk), said on one NOTE line, and the card is judged by its own commits; a card
// that itself changes a file outside its PATHS is still refused.
func TestAnEmptyStreamBranchFollowsItsTrunkBeforeTheFirstLand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, outside string
		landed        bool
	}{
		{"a card touching only its PATHS", "", true},
		{"a card touching a file outside its PATHS", "b.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, trunk := followRig(t)
			mainTip := r.git(r.remote, "rev-parse", "main")
			head := r.head("a", "dev", "a.txt", "a\n")
			if tc.outside != "" {
				head = r.commit(tc.outside, "The lane is one a machine.\n", "outside of a")
			}
			r.queued(map[string]string{"a": head}, "a")
			code, out, errs := r.do("land")
			if tc.landed {
				assert.Equal(t, 0, code, out+errs)
				assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=sprint/s1")
				assert.NotContains(t, out+errs, "(E12)")
				assert.Equal(t, map[string]string{"a": "landed/merged"}, r.places("a"))
				log := strings.Split(r.git(r.remote, "log", "--first-parent", "--format=%s", "sprint/s1"), "\n")
				assert.Equal(t, []string{"land a (sprint stream s1)", "moved t3.txt", "moved t2.txt", "moved t1.txt", "seed a.txt", "base"}, log, "the card landed on the followed branch")
			} else {
				assert.Equal(t, 1, code, out+errs)
				assert.Contains(t, errs, "it changes files outside its PATHS (E12): b.go")
				assert.NotContains(t, errs, "t1.txt", "the trunk's files are not the card's")
				assert.Equal(t, map[string]string{"a": "review/returned"}, r.places("a"))
				assert.Equal(t, trunk, r.git(r.remote, "rev-parse", "sprint/s1"), "the branch followed its trunk before the refusal")
			}
			assert.Equal(t, 1, strings.Count(out+errs, "NOTE the stream branch sprint/s1 had no commits of its own and was fast-forwarded to dev ("), "one NOTE line says so:\n%s%s", out, errs)
			assert.Contains(t, out+errs, "3 commit(s) behind")
			assert.NotContains(t, out+errs, "LAND BRANCH", "the branch existed on origin: nothing was created")
			assert.Equal(t, trunk, r.git(r.remote, "rev-parse", "dev"), "the trunk itself is not moved")
			assert.Equal(t, mainTip, r.git(r.remote, "rev-parse", "main"), "main is no part of it")
			r.clean()
		})
	}
}

// A stream branch level with its trunk is never moved, whatever another protected branch
// holds beyond it. The first cut of the follow guessed its trunk among dev and main, and a
// branch level with main while dev was one commit ahead was fast-forwarded to dev (a cold
// reader's fixture, 2026-10-12); the trunk is now the one the stream's control card records
// (dev here, from the card's founding base), so the only question is whether the branch is
// level with THAT branch: level with dev while main is one ahead, nothing moves, no NOTE, and
// main's commit never reaches the branch. (The fixture's own shape, a branch level with main
// while dev is ahead, is under the recorded trunk dev simply a branch behind its trunk, and
// follows dev: TestAnEmptyStreamBranchFollowsItsTrunkBeforeTheFirstLand.)
func TestAStreamBranchLevelWithATrunkIsNeverMovedToAnother(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.stubGreenGate()
	r.commit("a.txt", "seed\n", "seed a.txt")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/dev", "HEAD:refs/heads/sprint/s1")
	r.git(r.worker, "fetch", "-q", "origin")
	brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nREPO: "+r.remote+"\nBASE: dev\nPATHS: a.txt", "")
	r.ok("add --stream s1 a --one --brief-file " + brief)
	require.Contains(t, r.primary("a").F("brief"), "\nBASE: sprint/s1\n", "add re-cuts the card onto the stream's branch")
	r.moveBase("main", "m1.txt")
	r.git(r.worker, "fetch", "-q", "origin")
	before := r.git(r.remote, "rev-parse", "sprint/s1")
	mainTip := r.git(r.remote, "rev-parse", "main")
	require.Equal(t, before, r.git(r.remote, "rev-parse", "dev"), "the branch is level with its trunk")
	require.NotEqual(t, before, mainTip, "and a strict ancestor of main, which is not its trunk")
	head := r.head("a", "sprint/s1", "a.txt", "a\n")
	r.queued(map[string]string{"a": head}, "a")

	code, out, errs := r.do("land")
	assert.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=sprint/s1")
	assert.NotContains(t, out+errs, "fast-forwarded", "a branch level with its trunk is not moved")
	assert.Equal(t, map[string]string{"a": "landed/merged"}, r.places("a"))
	log := strings.Split(r.git(r.remote, "log", "--first-parent", "--format=%s", "sprint/s1"), "\n")
	assert.Equal(t, []string{"land a (sprint stream s1)", "seed a.txt", "base"}, log, "main's commit did not reach the branch")
	assert.Equal(t, before, r.git(r.remote, "rev-parse", "sprint/s1^"), "the landing's parent is the branch as it was")
	assert.Equal(t, mainTip, r.git(r.remote, "rev-parse", "main"), "main is not moved")
	assert.Equal(t, before, r.git(r.remote, "rev-parse", "dev"), "the trunk is not moved")
	r.clean()
}

// A stream branch with a commit of its own is never moved by the follow: it is no longer
// its trunk, and its card lands on it as before, with no NOTE and the trunk's commits
// left where they are.
func TestAStreamBranchWithItsOwnCommitsIsNotMovedToItsTrunk(t *testing.T) {
	t.Parallel()
	r, trunk := followRig(t)
	r.git(r.worker, "switch", "-q", "--detach", "origin/sprint/s1")
	own := r.commit("own.txt", "own\n", "own work of the stream")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/sprint/s1")
	r.git(r.worker, "fetch", "-q", "origin")
	head := r.head("a", "sprint/s1", "a.txt", "a\n")
	r.queued(map[string]string{"a": head}, "a")

	code, out, errs := r.do("land")
	assert.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=sprint/s1")
	assert.NotContains(t, out+errs, "fast-forwarded", "a branch with its own commits is not moved")
	assert.NotContains(t, out+errs, "LAND BRANCH", "the branch existed on origin: nothing was created or pushed over it")
	assert.Equal(t, map[string]string{"a": "landed/merged"}, r.places("a"))
	log := strings.Split(r.git(r.remote, "log", "--first-parent", "--format=%s", "sprint/s1"), "\n")
	assert.Equal(t, []string{"land a (sprint stream s1)", "own work of the stream", "seed a.txt", "base"}, log)
	assert.Equal(t, own, r.git(r.remote, "rev-parse", "sprint/s1^"), "the branch's own commit is the landing's parent")
	assert.Equal(t, trunk, r.git(r.remote, "rev-parse", "dev"), "the trunk is not moved")
	r.clean()
}
