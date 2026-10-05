package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/ntable"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// setFields writes fields of a merging primary by hand, in one store step: a DEPENDS-ON: line
// re-pointed after its deal leaves needs the rig's cards are not dealt with, and an eject's
// count is what earlier landings left.
func (r *landRig) setFields(id string, set map[string]string) {
	r.t.Helper()
	st, err := r.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(r.t, err)
	res, err := st.Run(context.Background(), store.Step{Verb: "test-fields", Args: store.ArgsOf(set), Load: []string{sprint.Work},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			pr := s.Work.Placed(id)
			e := sprint.Change{Table: sprint.Work, Entry: ntable.BatchMemberEntry{ID: pr.ID, Set: set,
				Expect: &ntable.MemberExpect{Revision: strconv.FormatUint(pr.Rev, 10), Place: &ntable.PlaceExpect{Row: pr.Row, Col: pr.Col}}}}
			return sprint.Plan{Units: []sprint.Unit{{Key: id, Stream: pr.Row, Changes: []sprint.Change{e}, Moved: id + " fields set"}}}
		}})
	require.NoError(r.t, err)
	require.Len(r.t, res.Moved, 1, "%+v", res)
}

// setNeeds writes a merging card's needs.
func (r *landRig) setNeeds(id, needs string) {
	r.t.Helper()
	r.setFields(id, map[string]string{"needs": needs})
	require.Equal(r.t, needs, r.primary(id).F("needs"))
}

// orphan is a card's head with history unrelated to the base: the commit the stream
// rate-tools-dev-2026-10-05 stalled on ("refusing to merge unrelated histories").
func (r *landRig) orphan(id, file string) string {
	r.t.Helper()
	r.git(r.worker, "switch", "-q", "--orphan", "sprint/"+id)
	return r.commit(file, id+"\n", "unrelated work of "+id)
}

// One card that cannot be merged no longer stalls its batch (docs/SPEC-SPRINT.md section 7,
// the eject; tla/Land.tla, THE EJECT, EjectLands): it is ejected back to review with its
// reason, every later card whose needs reach it goes with it naming the card it waited on,
// the rest lands in order in the same run, and the coordinator is told in one judgment. A
// failure of the environment still blames no card, and a stream that lands nothing for
// fifteen minutes while it holds cards raises "merge stuck".
func TestALandBatchEjectsTheCardThatCannotMergeAndItsDependentsAndLandsTheRest(t *testing.T) {
	t.Parallel()
	t.Run("unrelated history ejects the card and the one that needs it, and lands the third", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 3")
		heads := map[string]string{"s1-1": r.orphan("s1-1", "a.txt"), "s1-2": r.head("s1-2", "main", "b.txt", "b\n"), "s1-3": r.head("s1-3", "main", "c.txt", "c\n")}
		r.queued(heads, "s1-1", "s1-2", "s1-3")
		// the store refuses a merging card a need that has not landed (check rule 11), so the
		// need is written by hand, and the check names it before the land as after
		r.setNeeds("s1-2", "s1-1")
		const ruleEleven = "VIOLATION rule 11: s1-2 is "
		_, out, errs := r.do("check")
		var code int
		require.Contains(t, out+errs, ruleEleven+"merging and needs s1-1, not landed")
		code, out, errs = r.do("land --repo-dir " + r.clone + " --base main")
		require.Equal(t, 0, code, out+errs)
		tip := r.git(r.remote, "rev-parse", "main")
		assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main tip="+tip+" ids=s1-3")
		assert.Contains(t, out, "ejected=s1-1,s1-2")
		assert.Contains(t, out, "NOTE land ejected s1-1: the merge of s1-1 failed in git, not on its changes")
		assert.Contains(t, out, "refusing to merge unrelated histories")
		assert.Contains(t, out, "NOTE land ejected s1-2: it needs s1-1, which land ejected")
		assert.Contains(t, out, "LAND DONE batches=1 cards=1 refused=0")
		assert.NotContains(t, out+errs, "no card is blamed")
		assert.Equal(t, []string{"land s1-3 (sprint stream s1)", "base"}, r.mainLog())
		assert.Equal(t, map[string]string{"s1-1": "review/returned", "s1-2": "review/returned", "s1-3": "landed/merged"}, r.places("s1-1", "s1-2", "s1-3"))
		assert.NotContains(t, r.streamState("s1"), "stopped", "an eject stops no stream")
		// one judgment group for the eject: the card, its reason, its dependents, what landed
		inbox := r.ok("inbox")
		assert.Equal(t, 1, strings.Count(inbox, "JUDGMENT "), inbox)
		assert.Contains(t, inbox, "land ejected s1-1: the merge of s1-1 failed in git")
		assert.Contains(t, inbox, "ejected with it: s1-2 (waited on s1-1); landed: s1-3")
		assert.Contains(t, r.primary("s1-2").F("return_reason"), "land ejected s1-2 with s1-1: it needs s1-1")
		assert.Equal(t, sprint.EjectedGit, r.primary("s1-1").F(sprint.FieldEjectWay))
		assert.Equal(t, "1", r.primary("s1-1").F(sprint.FieldEjects))
		_, out, errs = r.do("check")
		assert.Contains(t, out+errs, ruleEleven+"review and needs s1-1, not landed")
		assert.Contains(t, out+errs, "violations=1", "the eject adds no violation of its own")
	})
	t.Run("a head built on the ejected head goes with it, and the check is clean", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 3")
		heads := map[string]string{"s1-1": r.orphan("s1-1", "a.txt")}
		r.git(r.worker, "switch", "-q", "-c", "sprint/s1-2", "sprint/s1-1")
		heads["s1-2"] = r.commit("b.txt", "b\n", "work of s1-2 on s1-1")
		heads["s1-3"] = r.head("s1-3", "main", "c.txt", "c\n")
		r.queued(heads, "s1-1", "s1-2", "s1-3")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		require.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "ids=s1-3")
		assert.Contains(t, out, "NOTE land ejected s1-2: its head "+heads["s1-2"]+" is built on the head of s1-1, which land ejected")
		assert.Equal(t, map[string]string{"s1-1": "review/returned", "s1-2": "review/returned", "s1-3": "landed/merged"}, r.places("s1-1", "s1-2", "s1-3"))
		assert.Contains(t, r.ok("inbox"), "ejected with it: s1-2 (waited on s1-1); landed: s1-3")
		r.clean()
	})
	t.Run("a conflict nothing needs is ejected and the rest lands", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 3")
		heads := map[string]string{"s1-1": r.head("s1-1", "main", "README", "theirs\n"), "s1-2": r.head("s1-2", "main", "b.txt", "b\n"), "s1-3": r.head("s1-3", "main", "c.txt", "c\n")}
		r.moveBase("main", "README")
		r.queued(heads, "s1-1", "s1-2", "s1-3")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		require.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
		assert.Contains(t, out, "ids=s1-2..s1-3")
		assert.Contains(t, out, "ejected=s1-1\n")
		assert.Contains(t, out, "NOTE land ejected s1-1: the head "+heads["s1-1"]+" of s1-1 does not merge")
		assert.Equal(t, []string{"land s1-3 (sprint stream s1)", "land s1-2 (sprint stream s1)", "moved README", "base"}, r.mainLog())
		assert.Equal(t, map[string]string{"s1-1": "review/returned", "s1-2": "landed/merged", "s1-3": "landed/merged"}, r.places("s1-1", "s1-2", "s1-3"))
		assert.NotContains(t, r.streamState("s1"), "stopped")
		inbox := r.ok("inbox")
		assert.Contains(t, inbox, "ejected with it: none; landed: s1-2, s1-3")
		assert.Equal(t, sprint.RefusedConflict, r.primary("s1-1").F(sprint.FieldEjectWay))
		r.clean()
	})
	t.Run("the environment's failure blames no card, and fifteen minutes of it is merge stuck", func(t *testing.T) {
		t.Parallel()
		r := newLandRig(t)
		r.ok("add --stream s1 --count 2")
		r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n"), "s1-2": r.head("s1-2", "main", "b.txt", "b\n")}, "s1-1", "s1-2")
		r.a.gitEnv = append(append([]string(nil), r.env...), "GIT_AUTHOR_NAME=", "GIT_COMMITTER_NAME=")
		before := r.git(r.remote, "rev-parse", "main")
		code, _, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code)
		assert.Contains(t, errs, "LAND REFUSED stream=s1 cards=2 base=main tip=- ids=s1-1..s1-2")
		assert.Contains(t, errs, "no card is blamed and nothing was pushed or reported")
		assert.NotContains(t, errs, "ejected")
		assert.Equal(t, before, r.git(r.remote, "rev-parse", "main"))
		assert.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"))
		assert.Equal(t, "merging", r.streamState("s1"))
		assert.NotContains(t, r.ok("inbox"), sprint.NMergeStuck, "not yet: the clock has not run")
		r.a.sleep(sprint.MergeStuckAfter - time.Minute)
		_, _, errs = r.do("land --repo-dir " + r.clone + " --base main")
		assert.NotContains(t, errs, "merge stuck")
		r.a.sleep(2 * time.Minute)
		_, _, errs = r.do("land --repo-dir " + r.clone + " --base main")
		assert.Contains(t, errs, "NOTE merge stuck: the coordinator was told")
		_, _, _ = r.do("land --repo-dir " + r.clone + " --base main")
		inbox := r.ok("inbox")
		assert.Equal(t, 1, strings.Count(inbox, "has landed nothing since"), "one judgment, never one a run: "+inbox)
		assert.Contains(t, inbox, "empty ident name")
		// the environment mended: the batch lands and the judgment is answered by it
		r.a.gitEnv = r.env
		out := r.ok("land --repo-dir " + r.clone + " --base main")
		assert.Contains(t, out, "LAND OK stream=s1 cards=2")
		assert.NotContains(t, r.ok("inbox"), "has landed nothing since")
		r.clean()
	})
}

// A card ejected twice the same way is not ejected a third time: its brief is wrong, not the
// worker, and the lander stops the stream on it with that judgment, the cards before it landed.
func TestALandStopsOnACardEjectedTwiceTheSameWay(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 2")
	heads := map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n"), "s1-2": r.orphan("s1-2", "b.txt")}
	r.queued(heads, "s1-1", "s1-2")
	r.setFields("s1-2", map[string]string{sprint.FieldEjectWay: sprint.EjectedGit, sprint.FieldEjects: "2"})
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, out, "LAND OK stream=s1 cards=1")
	assert.Contains(t, errs, "ids=s1-2 fact=conflict")
	assert.NotContains(t, out+errs, "ejected=")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "merging/stuck"}, r.places("s1-1", "s1-2"))
	assert.Equal(t, "stopped conflict", r.streamState("s1"))
	inbox := r.ok("inbox")
	assert.Contains(t, inbox, "the brief is wrong, not the worker")
	assert.Contains(t, inbox, "s1-2 was ejected 2 times for the same reason (git)")
	r.clean()
}
