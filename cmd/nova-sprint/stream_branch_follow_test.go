package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// followRig is a stream s1 whose branch sprint/s1 was cut from main with no commits of its
// own, holding card a (PATHS: a.txt, BASE: sprint/s1), after main moved three commits past
// the cut: the branch is behind its trunk. It returns the trunk's tip.
func followRig(t *testing.T) (r *landRig, trunk string) {
	t.Helper()
	r = newLandRig(t)
	r.stubGreenGate()
	// a.txt is on main before the cut: the card's PATHS name a file at its base's tip
	r.commit("a.txt", "seed\n", "seed a.txt")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "push", "-q", "origin", "main:refs/heads/sprint/s1")
	r.git(r.worker, "fetch", "-q", "origin")
	brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nREPO: "+r.remote+"\nBASE: sprint/s1\nPATHS: a.txt", "")
	r.ok("add --stream s1 a --one --brief-file " + brief)
	for _, f := range []string{"t1.txt", "t2.txt", "t3.txt"} {
		r.moveBase("main", f)
	}
	r.git(r.worker, "fetch", "-q", "origin")
	trunk = r.git(r.remote, "rev-parse", "main")
	require.NotEqual(t, trunk, r.git(r.remote, "rev-parse", "sprint/s1"), "the stream branch is behind its trunk")
	return r, trunk
}

// A stream branch cut from its trunk falls behind as the trunk moves, and a card staged on
// the newer trunk carries the trunk's commits relative to the branch: the lander counted
// their files as the card's and refused a correct head (E12, 2026-10-12: five stream
// branches fast-forwarded by hand in one night). Now a stream branch with no commits of its
// own is fast-forwarded to its trunk before the batch is cut (followTrunk), said on one
// NOTE line, and the card is judged by its own commits; a card that itself changes a file
// outside its PATHS is still refused.
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
			head := r.head("a", "main", "a.txt", "a\n")
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
			assert.Equal(t, 1, strings.Count(out+errs, "NOTE the stream branch sprint/s1 had no commits of its own and was fast-forwarded to main ("), "one NOTE line says so:\n%s%s", out, errs)
			assert.Contains(t, out+errs, "3 commit(s) behind")
			assert.Equal(t, trunk, r.git(r.remote, "rev-parse", "main"), "the trunk itself is not moved")
			r.clean()
		})
	}
}

// levelRig is a stream s1 whose branch sprint/s1 is level with both trunks: main and dev
// both at the seed commit, sprint/s1 cut from there, card a (PATHS: a.txt) on it.
func levelRig(t *testing.T) *landRig {
	t.Helper()
	r := newLandRig(t)
	r.stubGreenGate()
	r.commit("a.txt", "seed\n", "seed a.txt")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/dev", "HEAD:refs/heads/sprint/s1")
	r.git(r.worker, "fetch", "-q", "origin")
	brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nREPO: "+r.remote+"\nBASE: sprint/s1\nPATHS: a.txt", "")
	r.ok("add --stream s1 a --one --brief-file " + brief)
	return r
}

// A stream branch level with a trunk is never moved, whatever another trunk holds beyond
// it: a branch equal to main's tip while dev is one commit ahead is a strict ancestor of
// dev, and the first cut of the follow (2026-10-12, a cold reader's fixture) skipped the
// equal trunk and fast-forwarded the branch to dev, importing dev into a stream cut on
// main. Now a tip equal to any trunk's is no follow at once; the mirror (level with dev,
// main ahead) holds the same. The card lands on the branch as it is, with no NOTE, and
// neither trunk's commit reaches the branch.
func TestAStreamBranchLevelWithATrunkIsNeverMovedToAnother(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, ahead, file string }{
		{"level with main, dev ahead", "dev", "d1.txt"},
		{"level with dev, main ahead", "main", "m1.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := levelRig(t)
			r.moveBase(tc.ahead, tc.file)
			r.git(r.worker, "fetch", "-q", "origin")
			before := r.git(r.remote, "rev-parse", "sprint/s1")
			aheadTip := r.git(r.remote, "rev-parse", tc.ahead)
			require.NotEqual(t, before, aheadTip, "the branch is a strict ancestor of the trunk that moved")
			head := r.head("a", "sprint/s1", "a.txt", "a\n")
			r.queued(map[string]string{"a": head}, "a")

			code, out, errs := r.do("land")
			assert.Equal(t, 0, code, out+errs)
			assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=sprint/s1")
			assert.NotContains(t, out+errs, "fast-forwarded", "a branch level with a trunk is not moved")
			assert.Equal(t, map[string]string{"a": "landed/merged"}, r.places("a"))
			log := strings.Split(r.git(r.remote, "log", "--first-parent", "--format=%s", "sprint/s1"), "\n")
			assert.Equal(t, []string{"land a (sprint stream s1)", "seed a.txt", "base"}, log, "no trunk commit reached the branch")
			assert.Equal(t, before, r.git(r.remote, "rev-parse", "sprint/s1^"), "the landing's parent is the branch as it was")
			assert.Equal(t, aheadTip, r.git(r.remote, "rev-parse", tc.ahead), "the trunk ahead is not moved")
			r.clean()
		})
	}
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
	assert.Equal(t, map[string]string{"a": "landed/merged"}, r.places("a"))
	log := strings.Split(r.git(r.remote, "log", "--first-parent", "--format=%s", "sprint/s1"), "\n")
	assert.Equal(t, []string{"land a (sprint stream s1)", "own work of the stream", "seed a.txt", "base"}, log)
	assert.Equal(t, own, r.git(r.remote, "rev-parse", "sprint/s1^"), "the branch's own commit is the landing's parent")
	assert.Equal(t, trunk, r.git(r.remote, "rev-parse", "main"), "the trunk is not moved")
	r.clean()
}
