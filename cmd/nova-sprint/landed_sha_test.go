package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Work on the branch that the store does not record landed (docs/SPEC-SPRINT.md section 7,
// land-record-unreported-push-bc.w2): a merging card whose head a pass pushed and never
// reported is listed by verify-landed as LANDED-UNRECORDED, and landed --sha records it at
// that commit, by git alone. A card whose head is not on the base, a commit not on the base
// and a card landed already are refused, naming what was found, and nothing is written.
func TestLandedRecordsAPushFoundOnTheBranch(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 2")
	heads := map[string]string{"s1-1": r.head("s1-1", "main", "s1-1.txt", "s1-1\n"), "s1-2": r.head("s1-2", "main", "s1-2.txt", "s1-2\n")}
	r.queued(heads, "s1-1", "s1-2")
	// the push that was never reported: s1-1 merged onto origin's main, the store not told
	r.git(r.worker, "fetch", "-q", "origin")
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.git(r.worker, "merge", "-q", "--no-ff", "-m", "land s1-1", heads["s1-1"])
	sha := r.git(r.worker, "rev-parse", "HEAD")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	// a commit on a side branch that holds s1-2's work, never on main
	r.git(r.worker, "switch", "-q", "--detach", heads["s1-2"])
	side := r.commit("side.txt", "side\n", "side")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/side")
	require.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"))

	code, out, errs := r.do("verify-landed --repo-dir " + r.clone + " --base main")
	require.Equal(t, 1, code, "verify-landed: %s%s", out, errs)
	assert.Contains(t, out, "LANDED-UNRECORDED s1-1 stream=s1 state=merging head="+heads["s1-1"]+" base=main tip="+sha+"; run: nova-sprint landed s1-1 --sha "+sha+" --reason <text>\n")
	assert.NotContains(t, out, "s1-2")
	assert.Contains(t, out, "VERIFY-UNRECORDED checked=2 unrecorded=1 unchecked=0\n")
	assert.Contains(t, out, "VERIFY-LANDED checked=0 missing=0 unchecked=0\n")

	// refused, nothing written: a head not on the base, a commit not on the base, no reason
	before := r.applies()
	code, _, errs = r.do("landed s1-2 --sha " + sha + " --reason pushed --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "s1-2: its head "+heads["s1-2"]+" is not an ancestor of commit "+sha+": its work is not in it")
	code, _, errs = r.do("landed s1-2 --sha " + side + " --reason pushed --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "s1-2: commit "+side)
	assert.Contains(t, errs, "it is not on the base")
	code, _, errs = r.do("landed s1-1 s1-2 --sha " + sha + " --reason pushed --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, code, "all or none")
	assert.Contains(t, errs, "s1-2: its head")
	code, _, errs = r.do("landed s1-1 --sha " + sha + " --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "wants <card>... --sha <commit> --reason <text>")
	assert.Equal(t, before, r.applies(), "a refused landed wrote")
	assert.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"))

	// recorded: s1-1 lands at the commit, its merge card names it; s1-2 stays merging
	out = r.ok("landed s1-1 --sha " + sha[:12] + " --reason pushed-never-reported --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "s1-1 merging -> landed")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"))
	var v cardView
	r.json("card s1-1", &v)
	assert.Equal(t, "recorded landed at "+sha+": pushed-never-reported", v.Merge.F("note"))
	code, out, errs = r.do("verify-landed --repo-dir " + r.clone + " --base main")
	require.Equal(t, 0, code, "verify-landed after the record: %s%s", out, errs)
	assert.Contains(t, out, "VERIFY-UNRECORDED checked=1 unrecorded=0 unchecked=0\n")
	assert.Contains(t, out, "VERIFY-LANDED checked=1 missing=0 unchecked=0\n")
	// landed is final: recorded again, it is refused by the step
	code, out, errs = r.do("landed s1-1 --sha " + sha + " --reason again --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs+out, "landed already; landed is final")
	r.clean()
}

func TestLandedAcceptsACardWhoseHeadIsABranchName(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --one s1-1")
	r.ok("add --stream s1 --sentinel s1-gate --needs s1-1")
	r.ok("add --stream s1 --one s1-2")
	r.deal(1)
	r.ok("take --as m1 s1-1.w1@1")
	r.ok("finish --as m1 s1-1.w1@1 --failed --report 'attempt never pushed a commit'")
	r.ok("fleet down m1")
	r.ok("rework s1-1 --fix 'landed by hand'")

	var v cardView
	r.json("card s1-1", &v)
	require.Equal(t, "ready", v.Primary.Col)
	require.Equal(t, "s1-1.w1", v.Primary.F("head"))

	// hand landing on origin/main, and a commit on a side branch not on main
	r.git(r.worker, "fetch", "-q", "origin")
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	sha := r.commit("hand.txt", "hand\n", "hand land s1-1")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "switch", "-q", "--detach", "HEAD~1")
	side := r.commit("side.txt", "side\n", "side branch")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/side")

	// a sha not on BASE is refused naming the base, and nothing is written
	before := r.applies()
	code, _, errs := r.do("landed s1-1 --sha " + side + " --reason hand-landed --repo-dir " + r.clone + " --base main")
	require.Equal(t, 1, code)
	assert.Contains(t, errs, "origin/main")
	assert.Contains(t, errs, "it is not on the base")
	assert.Equal(t, before, r.applies(), "a refused landed wrote")

	// a ready card whose head is a branch name moves to landed and releases a sentinel that waited on it
	out := r.ok("landed s1-1 --sha " + sha + " --reason hand-landed --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "s1-1 ready -> landed")
	assert.Contains(t, out, "sentinel s1-gate waiting -> landed")
	assert.Contains(t, out, "s1-2 waiting -> ready")
	r.json("card s1-1", &v)
	assert.Equal(t, "landed", v.Primary.Col)
	assert.Equal(t, sha, v.Primary.F("head"))
	assert.Equal(t, "merged", v.Merge.Col)
	assert.Equal(t, "recorded landed at "+sha+": hand-landed", v.Merge.F("note"))
	r.json("card s1-gate", &v)
	assert.Equal(t, "landed", v.Primary.Col)
	r.json("card s1-2", &v)
	assert.Equal(t, "ready", v.Primary.Col)

	// a working card on a member lane is cancelled and its member's lane freed, with a NOTE
	r.ok("fleet up m1")
	r.deal(1)
	r.ok("take --as m1 --lane 1")
	out = r.ok("landed s1-2 --sha " + sha + " --reason hand-landed-working --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "s1-2 working -> landed")
	assert.Contains(t, out, "NOTE s1-2: cancelled running attempt s1-2.w1 on m1 lane 1; lane freed")
	r.json("card s1-2", &v)
	assert.Equal(t, "landed", v.Primary.Col)

	// m1 lane 1 is free to take another card, and cards in review and waiting may also be marked landed
	r.ok("add --stream s1 --one s1-3")
	r.ok("add --stream s1 --one s1-4 --needs s1-3")
	r.ok("add --stream s1 --one s1-5 --held")
	r.deal(1)
	r.ok("take --as m1 --lane 1")
	r.ok("finish --as m1 --lane 1 s1-3.w1@1 --failed --report 'hand landing'")
	out = r.ok("landed s1-3 --sha " + sha + " --reason hand-landed-review --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "s1-3 review -> landed")
	out = r.ok("landed s1-4 --sha " + sha + " --reason hand-landed-ready --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "s1-4 ready -> landed")
	out = r.ok("landed s1-5 --sha " + sha + " --reason hand-landed-waiting --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "s1-5 waiting -> landed")
	r.clean()
}
