package steps

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// same requires the folded view to equal a fresh build at the same seq.
func (w *world) same(what string) {
	w.t.Helper()
	w.fold()
	fresh := Build(w.snap(), t0)
	require.Equal(w.t, fresh.core(), w.v.core(), "after %s the fold differs from a fresh build", what)
}

// TestViewFoldEqualsBuild folds one event of every kind the store logs and
// checks the view after each equals a fresh build (list item 1).
func TestViewFoldEqualsBuild(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.stream("s1")
	w.same("put stream")
	w.row("r1", 2, "pro")
	w.same("put row and seen")
	w.row("r2", 1, "pro", "heavy")
	w.producer("p1", "s1", "pro", "a/x")
	w.producer("p2", "s1", "pro", "a/y")
	w.must("needs", store.PutCard{ID: "p3", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p3", Needs: []string{"p1"}, After: []string{"p2"}})
	w.must("sentinel", store.PutCard{ID: "g1", Kind: "sentinel", Stream: "s1", Tier: "pro", Needs: []string{"p3"}})
	w.same("put cards")
	assert.Equal(t, []string{"p3", "p2", "p1", "g1"}[3:], w.v.Dependants("p3"), "g1 needs p3")
	w.move(w.plan("p1", card.Resolve{}))
	w.move(w.plan("p2", card.Resolve{}))
	w.same("resolve")
	assert.Equal(t, []string{"p1", "p2"}, w.v.Producers("ready"))
	w.move(w.plan("p1", card.Start{Row: "r1", Branch: "sprint/s1/p1", Base: base}))
	w.same("start")
	assert.Equal(t, []string{"p1.w1"}, w.v.Live("r1"))
	w.same("take")
	assert.Equal(t, 1, w.v.Working("r1"))
	assert.Equal(t, w.now, w.v.TakenAt["p1.w1"], "the fold witnessed the take")
	w.report("p1.w1", "ok", sha(1), "done")
	w.same("report ok")
	w.move(w.plan("p1", card.AskReads{Rows: []string{"r2"}}))
	w.same("ask reads")
	w.report("p1.r1.1", "ok", sha(1), "fine")
	w.move(w.plan("p1", card.ReadYes{}))
	w.same("read yes")
	w.move(w.plan("p1", card.Landed{Head: sha(1)}))
	w.same("landed")
	assert.Equal(t, w.now, w.v.Landed["s1"])
	w.move(w.plan("p2", card.SetPriority{Level: "blocker", Reason: "now"}))
	w.same("set priority")
	assert.Equal(t, set{"p2": {}}, w.v.byRung["blocker"])
	w.move(w.plan("p2", card.Edit{Paths: []string{"a/z"}, Reason: "scope"}))
	w.same("edit paths")
	w.must("needs", store.SetCardNeeds{Card: "p3", Needs: nil})
	w.same("needs changed")
	assert.Empty(t, w.v.Dependants("p1"))
	w.must("hold stream", store.SetStreamHeld{Stream: "s1", Held: true})
	w.same("hold stream")
	w.must("hold row", store.SetRowHeld{Row: "r2", Held: true, Reason: "rest"})
	w.must("gone", store.SetRowState{Row: "r2", State: "down"}, store.SetRowFlaps{Row: "r2", Flaps: 1})
	w.same("row hold and gone")
	assert.Equal(t, w.now, w.v.Down["r2"])
	w.must("judge", store.PutJudgement{ID: "unserved.heavy.1", Kind: "unserved", About: "heavy", Question: "?", Command: "judge", Group: "unserved"})
	w.same("judgement")
	w.must("answer", store.AnswerJudgement{ID: "unserved.heavy.1", Answer: "ok"})
	w.must("policy", store.PutPolicy{Kind: "script", Name: "v1", Body: "def order(c): return ()"})
	w.must("activate", store.SetActivePolicy{Version: 1})
	w.same("policy")
	assert.Equal(t, 1, w.v.Policy)
	w.must("cancel", store.SetCardState{Card: "p2", State: "fail"}, store.SetCardReason{Card: "p2", Reason: "dropped"})
	w.same("fail")
	assert.Empty(t, w.v.Overlaps(w.card("p3")), "a failed producer holds no paths")
	rc, err := w.f.Clear(context.Background(), store.Op{ID: "clear", Epoch: 1, Actor: "test", Kind: "clear", At: w.now.Add(time.Minute)})
	require.NoError(t, err)
	require.True(t, rc.Accepted, rc.Reason)
	w.same("clear")
	assert.Equal(t, 2, w.v.Epoch)
	assert.Empty(t, w.v.Cards)
}

// TestViewFoldKeepsAConsumersModel: the view folds a card as store.Fold
// does, so a report from a pre-model worker (an after-image with no model)
// keeps the cut's model, and the fold equals a fresh build of the rebuilt
// snapshot at that seq.
func TestViewFoldKeepsAConsumersModel(t *testing.T) {
	t.Parallel()
	cut := store.Card{ID: "p1.w4", Kind: "work", Of: "p1", Stream: "s", State: "working", Holder: "amd-2", Model: "inception/mercury-3"}
	done := cut
	done.State, done.Model, done.Gen = "done", "", 1
	events := []store.Event{
		{Seq: 1, Accepted: true, Kind: "tick", At: t0, After: &store.State{Cards: map[string]store.Card{cut.ID: cut}}},
		{Seq: 2, Accepted: true, Kind: "report", At: t0, After: &store.State{Cards: map[string]store.Card{cut.ID: done}}},
	}
	rebuilt := store.Rebuild(events)
	v := Build(store.Rebuild(nil), t0)
	v.Fold(events)
	assert.Equal(t, rebuilt.Cards[cut.ID], v.Cards[cut.ID], "the view folds a card as store.Fold does")
	assert.Equal(t, Build(rebuilt, t0).core(), v.core(), "the fold equals a fresh build at seq 2")
}

// TestViewRefusedEventMovesSeqOnly: a refused op is an event too; it moves
// the cursor and nothing else.
func TestViewRefusedEventMovesSeqOnly(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.stream("s1")
	w.fold()
	before := w.v.core()
	rc := w.apply("bad", []store.Expect{store.StreamHeld{Stream: "s1", Held: true}}, store.SetStreamHeld{Stream: "s1", Held: true})
	require.False(t, rc.Accepted)
	w.fold()
	assert.Equal(t, rc.Seq, w.v.Seq)
	w.v.Seq = before.Seq
	assert.Equal(t, before, w.v.core())
}

// TestViewOverlaps: the paths index holds ready and started producers of
// one stream, and a path shared across streams is no overlap.
func TestViewOverlaps(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.stream("s1")
	w.stream("s2")
	w.producer("p1", "s1", "pro", "a/x", "a/y")
	w.producer("p2", "s1", "pro", "a/y")
	w.producer("p3", "s2", "pro", "a/y")
	w.fold()
	assert.Empty(t, w.v.Overlaps(w.card("p2")), "a waiting producer holds no paths")
	w.move(w.plan("p1", card.Resolve{}))
	w.fold()
	assert.Equal(t, []string{"p1"}, w.v.Overlaps(w.card("p2")))
	assert.Empty(t, w.v.Overlaps(w.card("p3")), "another stream")
}

// TestViewConsumersInCutOrder: consumers come back in the order they were
// cut, which the model numbers as slots.
func TestViewConsumersInCutOrder(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.stream("s1")
	w.row("r1", 2, "pro")
	w.row("r2", 2, "pro")
	w.producer("p1", "s1", "pro", "a/x")
	w.move(w.plan("p1", card.Resolve{}))
	w.move(w.plan("p1", card.Start{Row: "r1", Branch: "b", Base: base}))
	w.report("p1.w1", "fail", "", "broke")
	w.move(w.plan("p1", card.Start{Row: "r1", Branch: "b", Base: base}))
	w.report("p1.w2", "ok", sha(2), "done")
	w.move(w.plan("p1", card.AskReads{Rows: []string{"r2"}}))
	w.fold()
	var ids []string
	for _, c := range w.v.Consumers("p1") {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"p1.w1", "p1.w2", "p1.r1.1"}, ids)
	cold := Build(w.snap(), t0)
	ids = nil
	for _, c := range cold.Consumers("p1") {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"p1.r1.1", "p1.w1", "p1.w2"}, ids, "a cold view orders consumers it did not see cut by id")
}
