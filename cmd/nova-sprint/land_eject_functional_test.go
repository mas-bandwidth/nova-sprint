//go:build functional

package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/ntable"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The eject (tla/Land.tla, THE EJECT; docs/SPEC-SPRINT.md section 7, the land batch): one
// card that cannot merge no longer stalls its stream's batch. It goes back to review with
// its reason, every card of the batch whose needs reach it goes with it naming the card it
// waited on, the rest lands in order in the same run, and the coordinator is told once. A
// failure of the environment still blames no card (the owner, 2026-10-05: a card whose
// branch had history unrelated to the base held 16 cards for minutes).

// setWork sets fields of a primary in the work table by hand: a queued card that needs
// another queued card, or the count earlier ejects left. The first is a state check's rule 11
// names (a primary past waiting has every need landed), so the lander's eject is the defense
// behind it; checkOnlyNeeds says it is the one violation the land leaves.
func (ta *testApp) setWork(id string, set map[string]string) {
	ta.t.Helper()
	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(ta.t, err)
	snap, err := st.Load(ctx, []string{sprint.Work}, nil)
	require.NoError(ta.t, err)
	c := snap.Work.Card(id)
	require.NotNil(ta.t, c, id)
	_, err = ta.m.Apply(ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Work), Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(snap.Work.Revision), OperationID: "set-work-" + id + "-" + fmt.Sprint(c.Rev),
		Members: []ntable.BatchMemberEntry{{ID: c.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)}, Set: set}}})
	require.NoError(ta.t, err)
}

// checkOnlyNeeds is check finding only the need set by hand: the land added none.
func (r *landRig) checkOnlyNeeds(id, need string) {
	r.t.Helper()
	code, out, errs := r.do("check")
	assert.Equal(r.t, 1, code)
	assert.Contains(r.t, out+errs, "VIOLATION rule 11: "+id+" is review and needs "+need+", not landed")
	assert.Contains(r.t, out+errs, "CHECK FAILED violations=1 ")
}

// unrelated is a card's head with no history in common with the base: an orphan commit.
func (r *landRig) unrelated(id, file string) string {
	r.t.Helper()
	r.git(r.worker, "switch", "-q", "--orphan", "sprint/"+id)
	r.git(r.worker, "rm", "-rq", "--cached", "--ignore-unmatch", ".")
	return r.commit(file, id+"\n", "unrelated work of "+id)
}

func TestALandBatchEjectsTheCardThatCannotMergeAndItsDependentsAndLandsTheRest(t *testing.T) {
	t.Parallel()
	t.Run("a head of unrelated history is ejected with the card that needs it, and the rest lands", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 3")
		heads := map[string]string{"s1-1": r.unrelated("s1-1", "a.txt"), "s1-2": r.head("s1-2", "main", "b.txt", "b\n"), "s1-3": r.head("s1-3", "main", "c.txt", "c\n")}
		r.queued(heads, "s1-1", "s1-2", "s1-3")
		r.setWork("s1-2", map[string]string{"needs": "s1-1"})
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main")
		assert.Contains(t, out, "ids=s1-3")
		assert.Contains(t, errs, "LAND EJECTED stream=s1 cards=2 base=main tip=- ids=s1-1..s1-2")
		assert.Contains(t, errs, "NOTE ejected s1-1: the head "+heads["s1-1"]+" of s1-1: git refused to merge it:")
		assert.Contains(t, errs, "refusing to merge unrelated histories")
		assert.Contains(t, errs, "NOTE ejected s1-2: it needs s1-1, which was ejected from the batch")
		assert.Contains(t, out, "LAND DONE batches=1 cards=1 refused=0 ejected=2")
		assert.NotContains(t, out+errs, "no card is blamed")
		assert.Equal(t, []string{"land s1-3 (sprint stream s1)", "base"}, r.mainLog())
		assert.Equal(t, map[string]string{"s1-1": "review/returned", "s1-2": "review/returned", "s1-3": "landed/merged"}, r.places("s1-1", "s1-2", "s1-3"))
		assert.NotContains(t, r.streamState("s1"), "stopped", "an eject stops no stream")
		// one judgment, pushed to the coordinator: the card, its reason, the card ejected with
		// it and what landed
		inbox := r.ok("inbox")
		assert.Contains(t, inbox, "returned to review")
		assert.Contains(t, inbox, "ejected s1-1: the head "+heads["s1-1"]+" of s1-1: git refused to merge it")
		assert.Contains(t, inbox, "ejected with it: s1-2 (needs s1-1)")
		assert.Contains(t, inbox, "landed: s1-3")
		var v cardView
		r.json("card s1-2", &v)
		assert.Contains(t, fmt.Sprint(v), "it needs s1-1, which was ejected from the batch")
		r.checkOnlyNeeds("s1-2", "s1-1")
	})
	t.Run("a card ranked ahead of the card it needs goes out with it, and the batch is built again", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 3")
		heads := map[string]string{"s1-1": r.unrelated("s1-1", "a.txt"), "s1-2": r.head("s1-2", "main", "b.txt", "b\n"), "s1-3": r.head("s1-3", "main", "c.txt", "c\n")}
		r.queued(heads, "s1-2", "s1-1", "s1-3")
		r.setWork("s1-2", map[string]string{"needs": "s1-1"})
		r.ok("rank s1-2 --first")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 0, code, out+errs)
		assert.Contains(t, errs, "NOTE ejected s1-2: it needs s1-1, which was ejected from the batch")
		assert.Equal(t, []string{"land s1-3 (sprint stream s1)", "base"}, r.mainLog(), "s1-2 was merged before s1-1 failed: it is not pushed")
		assert.Equal(t, map[string]string{"s1-1": "review/returned", "s1-2": "review/returned", "s1-3": "landed/merged"}, r.places("s1-1", "s1-2", "s1-3"))
		r.checkOnlyNeeds("s1-2", "s1-1")
	})
	t.Run("a conflict that nothing needs is ejected and the rest lands", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 3")
		heads := map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "one\n"), "s1-2": r.head("s1-2", "main", "a.txt", "other\n"), "s1-3": r.head("s1-3", "main", "c.txt", "c\n")}
		r.queued(heads, "s1-1", "s1-2", "s1-3")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
		assert.Contains(t, errs, "LAND EJECTED stream=s1 cards=1 base=main tip=- ids=s1-2 ")
		assert.Contains(t, errs, "reason=the head "+heads["s1-2"]+" of s1-2 does not merge")
		assert.Equal(t, []string{"land s1-3 (sprint stream s1)", "land s1-1 (sprint stream s1)", "base"}, r.mainLog())
		assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "review/returned", "s1-3": "landed/merged"}, r.places("s1-1", "s1-2", "s1-3"))
		assert.NotContains(t, r.streamState("s1"), "stopped")
		assert.NotContains(t, r.ok("inbox"), "stream stopped: conflict on a card")
		r.clean()
	})
	t.Run("the environment's failure still blames no card, and fifteen minutes of it is merge stuck", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 2")
		r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n"), "s1-2": r.head("s1-2", "main", "b.txt", "b\n")}, "s1-1", "s1-2")
		r.a.gitEnv = append(append([]string(nil), r.env...), "GIT_AUTHOR_NAME=", "GIT_COMMITTER_NAME=")
		before := r.git(r.remote, "rev-parse", "main")
		code, _, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code)
		assert.Contains(t, errs, "no card is blamed and nothing was pushed or reported")
		assert.NotContains(t, errs, "EJECTED")
		assert.Equal(t, before, r.git(r.remote, "rev-parse", "main"))
		assert.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"))
		assert.NotContains(t, r.ok("inbox"), "merge stuck")
		// fourteen minutes on: not yet
		r.mu.Lock()
		r.now = r.now.Add(14 * time.Minute)
		r.mu.Unlock()
		_, _, errs = r.do("land --repo-dir " + r.clone + " --base main")
		assert.NotContains(t, errs, "merge stuck")
		r.mu.Lock()
		r.now = r.now.Add(2 * time.Minute)
		r.mu.Unlock()
		_, _, errs = r.do("land --repo-dir " + r.clone + " --base main")
		assert.Contains(t, errs, "merge stuck for 16m0s: the coordinator is told")
		inbox := r.ok("inbox")
		assert.Contains(t, inbox, sprint.NMergeStuck)
		assert.Contains(t, inbox, "has landed nothing for 16m0s")
		assert.Contains(t, inbox, "empty ident name")
		// said once
		_, _, errs = r.do("land --repo-dir " + r.clone + " --base main")
		assert.NotContains(t, errs, "merge stuck")
		// the environment mended, the batch lands and the judgment is answered
		r.a.gitEnv = r.env
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "LAND OK stream=s1 cards=2")
		inbox = r.ok("inbox")
		assert.Contains(t, inbox, sprint.NMergeStuck+"  stream=s1  size=2  (s1-1,s1-2)  answered by merge")
		assert.Contains(t, inbox, "INBOX OK judgments=0 ")
		r.clean()
	})
	t.Run("a batch refused hours after its queue was emptied by hand begins its count again", func(t *testing.T) {
		t.Parallel()
		// the reader's bench case: the stamp of a stall the coordinator ended by hand (a
		// return, not a landing or an eject) is never the count of the next batch's stall
		r := newLandRig(t)
		r.ok("add --stream s1 --count 2")
		r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n"), "s1-2": r.head("s1-2", "main", "b.txt", "b\n")}, "s1-1", "s1-2")
		r.a.gitEnv = append(append([]string(nil), r.env...), "GIT_AUTHOR_NAME=", "GIT_COMMITTER_NAME=")
		land := "land --repo-dir " + r.clone + " --base main"
		code, _, errs := r.do(land)
		assert.Equal(t, 1, code)
		assert.Contains(t, errs, "no card is blamed and nothing was pushed or reported")
		r.ok("return s1-1 s1-2 --reason 'held by hand'")
		assert.Equal(t, map[string]string{"s1-1": "review/returned", "s1-2": "review/returned"}, r.places("s1-1", "s1-2"))
		r.mu.Lock()
		r.now = r.now.Add(3 * time.Hour)
		r.mu.Unlock()
		r.ok("add --stream s1 --count 1 --one")
		r.queued(map[string]string{"s1-3": r.head("s1-3", "main", "c.txt", "c\n")}, "s1-3")
		// the queue filled again: the returned cards with the new one, the stamped batch's
		// cards among them, so only the hours without a refusal tell the stalls apart
		assert.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued", "s1-3": "merging/queued"}, r.places("s1-1", "s1-2", "s1-3"))
		code, _, errs = r.do(land)
		assert.Equal(t, 1, code)
		assert.Contains(t, errs, "no card is blamed and nothing was pushed or reported")
		assert.NotContains(t, errs, "merge stuck", "a first refusal is no stall")
		assert.NotContains(t, r.ok("inbox"), "merge stuck")
		// the new stall, refused on as the lander refuses (every LandEvery), is counted from
		// its own first refusal
		r.mu.Lock()
		r.now = r.now.Add(8 * time.Minute)
		r.mu.Unlock()
		_, _, errs = r.do(land)
		assert.NotContains(t, errs, "merge stuck")
		r.mu.Lock()
		r.now = r.now.Add(8 * time.Minute)
		r.mu.Unlock()
		_, _, errs = r.do(land)
		assert.Contains(t, errs, "merge stuck for 16m0s: the coordinator is told")
		assert.Contains(t, r.ok("inbox"), "the batch s1-1 .. s1-3 of stream s1 has landed nothing for 16m0s")
		r.a.gitEnv = r.env
		r.clean()
	})
	t.Run("a card ejected twice for one cause raises the brief is wrong instead of a third eject", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 2")
		heads := map[string]string{"s1-1": r.unrelated("s1-1", "a.txt"), "s1-2": r.head("s1-2", "main", "b.txt", "b\n")}
		r.queued(heads, "s1-1", "s1-2")
		r.setWork("s1-1", map[string]string{sprint.FieldEjectCause: "refused", sprint.FieldEjectCount: "2"})
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 0, code, out+errs)
		assert.Contains(t, errs, "LAND EJECTED stream=s1 cards=1")
		assert.Equal(t, map[string]string{"s1-1": "review/returned", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"))
		inbox := r.ok("inbox")
		assert.Contains(t, inbox, "the brief is wrong")
		assert.Contains(t, inbox, "ejected 3 times in a row for one cause (refused)")
		assert.NotContains(t, inbox, "returned to review", "the third eject is not another eject's judgment")
		r.clean()
	})
}
