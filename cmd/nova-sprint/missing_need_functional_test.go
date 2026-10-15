//go:build functional

package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// mnRemove takes a card off the work table keeping its record, as a drop does,
// without a step that would refuse it for its dependants: a store edit standing
// in for any path that can strand a need (every-need-names-a-live-card-checked-
// every-second, rule (b)). The edit is the tick's own drain plan, so the store
// applies the removal raw, held to no lifecycle (the path it stands in for is a
// future one that bypasses the rules; the run loop must still catch the strand).
func mnRemove(t *testing.T, r *ujRig, id string) {
	t.Helper()
	_, err := r.store().Run(r.ctx, store.Step{
		Verb: "edit", Load: []string{sprint.Work},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Drain(s, []sprint.QueuedChange{{Entry: &ntable.BatchMemberEntry{ID: id, Remove: true}}}, sprint.MachineActor)
		},
	})
	require.NoError(t, err)
}

// mnMove puts a card in a column directly, so the test drives a card into each
// unlanded state without the whole pump, held to no lifecycle (as mnRemove).
func mnMove(t *testing.T, r *ujRig, id, col string) {
	t.Helper()
	_, err := r.store().Run(r.ctx, store.Step{
		Verb: "edit", Load: []string{sprint.Work},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			c := s.Work.Placed(id)
			return sprint.Drain(s, []sprint.QueuedChange{{Entry: &ntable.BatchMemberEntry{ID: id,
				Move: &ntable.MemberMoveOp{Row: c.Row, Col: col}}}}, sprint.MachineActor)
		},
	})
	require.NoError(t, err)
}

// mnQueue puts the card's merge record in its stream's queue, as accept does,
// so the merge step can land it (the merge step reads the queue).
func mnQueue(t *testing.T, r *ujRig, id string) {
	t.Helper()
	_, err := r.store().Run(r.ctx, store.Step{
		Verb: "edit", Load: []string{sprint.Merge, sprint.Work},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			c := s.Work.Placed(id)
			return sprint.Plan{Units: []sprint.Unit{{
				Key: id, Stream: c.Row,
				Changes: []sprint.Change{{Table: sprint.Merge, Entry: ntable.BatchMemberEntry{
					ID:     id,
					Expect: &ntable.MemberExpect{Absent: true},
					Create: &ntable.MemberCreateOp{Row: c.Row, Col: sprint.Queued, Score: c.Score},
					Set:    map[string]string{"kind": "merge", "primary": id, "stream": c.Row},
				}}},
			}}}
		},
	})
	require.NoError(t, err)
}

// mnPush is whether the server's coordinator push sent the seat a judgment naming both ids.
func mnPush(t *testing.T, r *ujRig, ids ...string) bool {
	t.Helper()
	for _, p := range r.pushes() {
		hit := true
		for _, id := range ids {
			hit = hit && strings.Contains(p.Body, id)
		}
		if hit {
			return true
		}
	}
	return false
}

// Rule (b): the run loop checks every unlanded card's needs in every state, and
// a need naming a card that is gone pushes one judgment to the coordinator at
// once, naming the card and the missing need, within one second of running time.
func TestEveryStateAlertsTheCoordinatorOfAGoneNeed(t *testing.T) {
	t.Parallel()
	began := time.Now()
	for _, st := range []string{"waiting", "ready", "working", "review", "merging"} {
		t.Run(st, func(t *testing.T) {
			r := newUJRig(t)
			r.do("init --readers reader-a --members m1 --owner glenn")
			r.beat()
			r.do("fleet up m1 --width 4")
			r.beat()
			r.do("start")
			r.do("add --stream s1 --count 1 --one")              // s1-1, the need
			r.do("add --stream s2 --count 1 --one --needs s1-1") // s2-1, the dependent
			mnRemove(t, r, "s1-1")                               // the need is gone
			if st != "waiting" {
				mnMove(t, r, "s2-1", st)
			}
			// one second of running time: the run loop's needs check
			_, err := r.store().Tick(r.ctx)
			require.NoError(t, err)
			assert.True(t, mnPush(t, r, "s2-1", "s1-1"),
				"the coordinator is told of the gone need on %s", st)
		})
	}
	assert.Less(t, time.Since(began), 30*time.Second, "under 30 s")
}

// Rule (b): a gone-need judgment stays open and repeats at its deadline until
// answered, and answering it with needs set clears it.
func TestAGoneNeedRepeatsAtItsDeadlineAndNeedsSetClearsIt(t *testing.T) {
	t.Parallel()
	r := newUJRig(t)
	r.do("init --readers reader-a --members m1 --owner glenn")
	r.beat()
	r.do("fleet up m1 --width 4")
	r.beat()
	r.do("start")
	r.do("add --stream s1 --count 1 --one")              // s1-1, the need
	r.do("add --stream s2 --count 1 --one --needs s1-1") // s2-1, the dependent
	mnRemove(t, r, "s1-1")                               // the need is gone
	mnMove(t, r, "s2-1", "merging")

	// the first raise, within one second of running time
	_, err := r.store().Tick(r.ctx)
	require.NoError(t, err)
	require.True(t, mnPush(t, r, "s2-1", "s1-1"), "the first judgment names the card and the missing need")

	openBlocked := func() int {
		v, err := r.store().Inbox(r.ctx, defaultDeadline, defaultStale, 10000)
		require.NoError(t, err)
		n := 0
		for _, o := range v.Open {
			if o.Note.Type == sprint.NNeedBlocked && o.Subject() == "s1-1" {
				n++
			}
		}
		return n
	}
	require.Equal(t, 1, openBlocked(), "one open blocker judgment on s1-1")

	// past its deadline, unanswered: it stays open, is never doubled
	r.mu.Lock()
	r.now = r.now.Add(sprint.DeadlineJudgment + time.Minute)
	r.mu.Unlock()
	r.beat()
	_, err = r.store().Tick(r.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, openBlocked(), "the judgment stays open, never doubles")

	// answered by needs set in place: the judgment clears
	r.do("needs set s2-1 --clear")
	_, err = r.store().Tick(r.ctx)
	require.NoError(t, err)
	require.Zero(t, openBlocked(), "the judgment clears once the need is set away")
}

// Rule (a): drop of a card another unlanded card needs is refused, naming the
// dependent; --waive passes and leaves no stranded need.
func TestDropRefusesANeededCardAndWaiveLeavesNone(t *testing.T) {
	t.Parallel()
	r := newUJRig(t)
	r.do("init --readers reader-a --members m1 --owner glenn")
	r.beat()
	r.do("fleet up m1 --width 4")
	r.beat()
	r.do("start")
	r.do("add --stream s1 --count 1 --one")
	r.do("add --stream s2 --count 1 --one --needs s1-1")

	var out, errb bytes.Buffer
	code := runOnStore(r.a, r.addr, []string{"drop", "s1-1", "--reason", "replaced"}, &out, &errb)
	require.NotEqual(t, 0, code, "drop of a needed card is refused")
	assert.Contains(t, errb.String(), "s1-1", "the refusal names the card")
	assert.Contains(t, errb.String(), "s2-1", "the refusal names the dependent")

	// --waive: the drop proceeds and the need is gone from the dependent
	r.do("drop s1-1 --waive --reason replaced")
	s2, err := r.store().Load(r.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err)
	require.Nil(t, s2.Work.Placed("s1-1"), "s1-1 is off the table")
	require.Empty(t, sprint.Split(s2.Work.Card("s2-1").F("needs")), "no stranded need on s2-1")
}

// Rule (a): drop of a group (several cards at once, the form --group resolves
// to) that holds a card an unlanded card needs is refused the same way, naming
// the dependent, so no selector strands a need.
func TestDropGroupRefusesANeededCard(t *testing.T) {
	t.Parallel()
	r := newUJRig(t)
	r.do("init --readers reader-a --members m1 --owner glenn")
	r.beat()
	r.do("fleet up m1 --width 4")
	r.beat()
	r.do("start")
	r.do("add --stream s1 --count 1 --one")              // s1-1, the need
	r.do("add --stream s1 --count 1 --one")              // s1-2, grouped with it
	r.do("add --stream s2 --count 1 --one --needs s1-1") // s2-1, the dependent

	var out, errb bytes.Buffer
	code := runOnStore(r.a, r.addr, []string{"drop", "s1-1", "s1-2", "--reason", "obsolete"}, &out, &errb)
	require.NotEqual(t, 0, code, "drop of a group holding a needed card is refused")
	assert.Contains(t, errb.String(), "s1-1", "the refusal names the needed card")
	assert.Contains(t, errb.String(), "s2-1", "the refusal names the dependent")
}

// Rule (a): --relink passes and re-points the dependent's need at the new card.
func TestDropWithRelinkRepointsTheNeed(t *testing.T) {
	t.Parallel()
	r := newUJRig(t)
	r.do("init --readers reader-a --members m1 --owner glenn")
	r.beat()
	r.do("fleet up m1 --width 4")
	r.beat()
	r.do("start")
	r.do("add --stream s1 --count 1 --one") // s1-1, the old need
	r.do("add --stream s3 --count 1 --one") // s3-1, the replacement
	r.do("add --stream s2 --count 1 --one --needs s1-1")

	r.do("drop s1-1 --relink s3-1 --reason replaced")
	_, err := r.store().Tick(r.ctx)
	require.NoError(t, err)
	snap, err := r.store().Load(r.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err)
	require.Nil(t, snap.Work.Placed("s1-1"), "s1-1 is off the table")
	require.Equal(t, []string{"s3-1"}, sprint.Split(snap.Work.Card("s2-1").F("needs")), "s2-1 now needs the replacement")
}

// Rule (b): needs set on a merging card whose need is gone succeeds in place,
// and the card lands once its other needs are landed (Done (3)).
func TestNeedsSetOnAMergingCardWithAGoneNeed(t *testing.T) {
	t.Parallel()
	r := newUJRig(t)
	r.do("init --readers reader-a --members m1 --owner glenn")
	r.beat()
	r.do("fleet up m1 --width 4")
	r.beat()
	r.do("start")
	r.do("add --stream s1 --count 1 --one")                   // s1-1, the need that goes
	r.do("add --stream s1 --count 1 --one")                   // s1-2, the other need
	r.do("add --stream s2 --count 1 --one --needs s1-1,s1-2") // s2-1, the dependent
	_, err := r.store().Tick(r.ctx)
	require.NoError(t, err)
	mnMove(t, r, "s1-2", "landed") // the other need is landed
	mnRemove(t, r, "s1-1")         // the need is gone
	mnMove(t, r, "s2-1", "merging")
	// needs set in place: today a merging card keeps its needs and this is refused
	out := r.do("needs set s2-1 --needs s1-2")
	require.Contains(t, out, "merging", "needs set runs on the merging card in place")
	_, err = r.store().Tick(r.ctx) // the pump drains the re-point
	require.NoError(t, err)
	snap, err := r.store().Load(r.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err)
	c := snap.Work.Placed("s2-1")
	require.NotNil(t, c, "s2-1 is on the table")
	require.Equal(t, []string{"s1-2"}, sprint.Split(c.F("needs")), "the gone need is re-pointed away")
	// the card lands once its other needs are landed: the merge step no longer
	// refuses it (WaitsFor reads nothing unlanded after the re-point)
	mnMove(t, r, "s2-1", "merging") // the short-reads pass sent it back to review
	mnQueue(t, r, "s2-1")
	_, err = r.store().Run(r.ctx, store.MergeStep(sprint.MergeReq{Stream: "s2", Batch: 1, Who: ujSeat}))
	require.NoError(t, err)
	_, err = r.store().Tick(r.ctx) // the pump drains the landing
	require.NoError(t, err)
	snap, err = r.store().Load(r.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err)
	require.Equal(t, sprint.Landed, snap.Work.Placed("s2-1").Col, "s2-1 lands once its needs are landed")
}
