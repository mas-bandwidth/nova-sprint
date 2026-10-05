package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/ntable"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestALandBatchEjectsTheCardThatCannotMergeAndItsDependentsAndLandsTheRest is
// the card's witness (tla/Land.tla, NoLandedNeedsUnlanded,
// EjectedDependentsUnlanded, LandsTheRest). The twin is landRig: a bare origin
// and two clones, no live sprint server. The 15 minutes are the rig's clock.
func TestALandBatchEjectsTheCardThatCannotMergeAndItsDependentsAndLandsTheRest(t *testing.T) {
	t.Parallel()
	t.Run("unrelated histories ejects the card and the card that needs it", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 3")
		heads := map[string]string{
			"s1-2": r.head("s1-2", "main", "s1-2.txt", "two\n"),
			"s1-3": r.head("s1-3", "main", "s1-3.txt", "three\n"),
			"s1-1": r.orphan("s1-1"),
		}
		r.queued(heads, "s1-1", "s1-2", "s1-3")
		r.setNeeds("s1-2", "s1-1")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code, out+errs)
		assert.Contains(t, out, "LAND OK stream=s1 cards=1")
		assert.Contains(t, out, "ids=s1-3")
		assert.Contains(t, errs, "LAND EJECTED")
		assert.Contains(t, errs, "s1-1")
		assert.Contains(t, errs, "s1-2")
		assert.Contains(t, errs, "unrelated")
		assert.Equal(t, []string{"land s1-3 (sprint stream s1)", "base"}, r.mainLog())
		assert.Equal(t, map[string]string{"s1-1": "review/returned", "s1-2": "review/returned", "s1-3": "landed/merged"}, r.places("s1-1", "s1-2", "s1-3"))
		assert.Equal(t, "waiting", r.streamState("s1"))
		assert.Contains(t, r.field("s1-2", "waived"), "s1-1")
		assert.Contains(t, r.field("s1-2", "return_reason"), "waited on s1-1")
		// The inbox groups one type on a stream into one judgment and shows the
		// first note's words. That note names the card, the reason, the
		// dependent, and what landed. s1-2's own note says which card it waited on.
		inbox := r.ok("inbox")
		assert.Contains(t, inbox, "ejected s1-1")
		assert.Contains(t, inbox, "dependents: s1-2")
		assert.Contains(t, inbox, "landed: s1-3")
		assert.Contains(t, inbox, "unrelated histories")
		assert.NotContains(t, inbox, "stopped conflict")
		r.clean()
	})
	t.Run("a conflict ejects only that card and the rest lands", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 3")
		heads := map[string]string{
			"s1-1": r.head("s1-1", "main", "s1-1.txt", "one\n"),
			"s1-2": r.head("s1-2", "main", "s1-1.txt", "other\n"),
			"s1-3": r.head("s1-3", "main", "s1-3.txt", "three\n"),
		}
		r.queued(heads, "s1-1", "s1-2", "s1-3")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code, out+errs)
		assert.Contains(t, out, "LAND OK stream=s1 cards=2")
		assert.Contains(t, errs, "LAND EJECTED")
		assert.Contains(t, errs, "s1-2")
		assert.Contains(t, errs, "does not merge")
		assert.Equal(t, []string{"land s1-3 (sprint stream s1)", "land s1-1 (sprint stream s1)", "base"}, r.mainLog())
		assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "review/returned", "s1-3": "landed/merged"}, r.places("s1-1", "s1-2", "s1-3"))
		assert.NotEqual(t, "stopped conflict", r.streamState("s1"))
		inbox := r.ok("inbox")
		assert.Contains(t, inbox, "ejected s1-2")
		assert.Contains(t, inbox, "landed: s1-1, s1-3")
		r.clean()
	})
	t.Run("an environment failure blames no card", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 2")
		r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n"), "s1-2": r.head("s1-2", "main", "b.txt", "b\n")}, "s1-1", "s1-2")
		r.a.gitEnv = append(append([]string(nil), r.env...), "GIT_AUTHOR_NAME=", "GIT_COMMITTER_NAME=")
		before := r.git(r.remote, "rev-parse", "main")
		code, _, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code)
		assert.Contains(t, errs, "LAND REFUSED stream=s1 cards=2 base=main tip=- ids=s1-1..s1-2")
		assert.Contains(t, errs, "the merge of s1-1 failed in git, not on its changes")
		assert.Contains(t, errs, "empty ident name")
		assert.Contains(t, errs, "no card is blamed and nothing was pushed or reported")
		assert.NotContains(t, errs, "fact=")
		assert.NotContains(t, errs, "LAND EJECTED")
		assert.Equal(t, before, r.git(r.remote, "rev-parse", "main"))
		assert.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"))
		assert.Equal(t, "merging", r.streamState("s1"))
		r.clean()
	})
	t.Run("merge stuck when the clock advances fifteen minutes", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 1 --one")
		r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}, "s1-1")
		r.a.gitEnv = append(append([]string(nil), r.env...), "GIT_AUTHOR_NAME=", "GIT_COMMITTER_NAME=")
		code, _, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code)
		assert.Contains(t, errs, "no card is blamed")
		assert.NotContains(t, r.ok("inbox"), "merge stuck")
		r.now = r.now.Add(sprint.MergeStuckAfter)
		code, _, errs = r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code, errs)
		assert.Contains(t, errs, "no card is blamed")
		assert.Contains(t, r.ok("inbox"), "merge stuck")
		assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"))
		assert.Equal(t, "merging", r.streamState("s1"))
		r.clean()
	})
}

// orphan is a head with no history in common with the base.
func (r *landRig) orphan(id string) string {
	r.t.Helper()
	r.git(r.worker, "checkout", "--orphan", "sprint/"+id)
	r.git(r.worker, "rm", "-rf", ".")
	return r.commit(id+".txt", id+"\n", "unrelated "+id)
}

// setNeeds writes the needs field after the card is already queued. An add
// with an unmet need admits the card waiting, so it would not share this batch.
func (r *landRig) setNeeds(id, needs string) {
	r.t.Helper()
	ctx := context.Background()
	st, err := r.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(r.t, err)
	snap, err := st.Load(ctx, []string{sprint.Work}, nil)
	require.NoError(r.t, err)
	c := snap.Work.Card(id)
	require.NotNil(r.t, c, id)
	_, err = r.m.Apply(ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Work), Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(snap.Work.Revision), OperationID: "needs-" + id,
		Members: []ntable.BatchMemberEntry{{ID: c.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)}, Set: map[string]string{"needs": needs}}}})
	require.NoError(r.t, err)
}

func (r *landRig) field(id, name string) string {
	r.t.Helper()
	st, err := r.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(r.t, err)
	snap, err := st.Load(context.Background(), []string{sprint.Work}, nil)
	require.NoError(r.t, err)
	c := snap.Work.Card(id)
	require.NotNil(r.t, c, id)
	return c.F(name)
}
