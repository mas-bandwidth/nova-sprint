package steps

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verb runs a request against the world and applies it when legal.
func (w *world) verb(r Request) (store.Receipt, *card.Refusal) {
	w.t.Helper()
	w.fold()
	r.Coordinators = []string{"friendF"}
	op, ref := Verb(w.v, w.inputs(), r)
	if ref != nil {
		return store.Receipt{}, ref
	}
	w.n++
	rc, err := w.f.Apply(w.t.Context(), op)
	require.NoError(w.t, err)
	return rc, nil
}

// TestVerbActorTable: every verb and actor (list item 8): a verb refused for
// its actor writes nothing.
func TestVerbActorTable(t *testing.T) {
	w := newWorld(t)
	w.stream("s1")
	w.row("r1", 2, "pro")
	w.producer("p1", "s1", "pro", "a/x")
	w.producer("p2", "s1", "pro", "a/y")
	coord := []struct {
		name string
		ev   card.Event
	}{
		{"SetPriority", card.SetPriority{Level: "high", Reason: "why"}}, {"Rescore", card.Rescore{Score: 3}}, {"Edit", card.Edit{Prose: "new", Reason: "why"}},
		{"Cancel", card.Cancel{Reason: "why"}}, {"Replace", card.Replace{By: "p2", Reason: "why"}}, {"Unblock", card.Unblock{Reason: "why"}},
		{"WaiveRed", card.WaiveRed{Reason: "why"}}, {"Waive", card.Waive{Need: "p2", Reason: "why"}}, {"Relink", card.Relink{Need: "p2", To: "p1", Reason: "why"}},
	}
	before := w.snap().Seq
	for _, c := range coord {
		_, ref := w.verb(Request{Actor: "friendC", Card: "p1", Event: c.ev})
		require.NotNil(t, ref, c.name)
		assert.Equal(t, c.name+" is the coordinator's; ask on the bus: `nova-bus send --as friendC --to friendF ...`", ref.Reason)
	}
	for _, c := range []struct {
		name string
		ev   card.Event
	}{{"Hold", card.Hold{Reason: "why"}}, {"Unhold", card.Unhold{Reason: "why"}}} {
		_, ref := w.verb(Request{Actor: "friendC", Row: "r1", Event: c.ev})
		require.NotNil(t, ref)
		assert.Contains(t, ref.Reason, c.name+" is the coordinator's")
	}
	_, ref := w.verb(Request{Actor: "friendC", Stream: "s1", Event: card.HoldStream{Reason: "why"}})
	assert.Contains(t, ref.Reason, "HoldStream is the coordinator's")
	_, ref = w.verb(Request{Actor: "friendC", Judgement: "x", Answer: "retry"})
	assert.Contains(t, ref.Reason, "judge is the coordinator's")
	_, ref = w.verb(Request{Actor: "friendC", Row: "r1", Event: card.Gone{}})
	assert.Equal(t, "Gone is the run loop's; presence is the bus's fact", ref.Reason)
	assert.Equal(t, before, w.snap().Seq, "a verb refused for its actor writes nothing")

	rc, ref := w.verb(Request{Actor: "friendF", Card: "p1", Event: card.SetPriority{Level: "high", Reason: "why"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	assert.Equal(t, "high", w.card("p1").Priority)
	rc, ref = w.verb(Request{Actor: "friendF", Card: "p1", Event: card.Rescore{Score: 3}})
	require.Nil(t, ref)
	assert.True(t, rc.Accepted)
	_, ref = w.verb(Request{Actor: "friendF", Card: "p1", Event: card.Unblock{Reason: "why"}})
	require.NotNil(t, ref, "the machine's refusal passes through")
	assert.Contains(t, ref.Reason, "Unblock needs blocked")
	rc, ref = w.verb(Request{Actor: "friendF", Stream: "s1", Event: card.HoldStream{Reason: "pause"}})
	require.Nil(t, ref)
	assert.True(t, rc.Accepted, rc.Reason)
	assert.True(t, w.snap().Streams["s1"].Held)
	rc, ref = w.verb(Request{Actor: "friendF", Row: "r1", Event: card.Hold{Reason: "rest"}})
	require.Nil(t, ref)
	assert.True(t, rc.Accepted, rc.Reason)
	rc, ref = w.verb(Request{Actor: Actor, Row: "r1", Event: card.Gone{}})
	require.Nil(t, ref)
	assert.True(t, rc.Accepted, rc.Reason)
}

// TestVerbAddReport: Add is anyone's; Report is the holder's, of a card in
// its hand from the deal (there is no Take event).
func TestVerbAddReport(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	rc, ref := s.verb(Request{Actor: "friendC", Event: card.Add{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p1", Paths: []string{"a/x"}}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	_, ref = s.verb(Request{Actor: "friendC", Event: card.Add{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p2", Needs: []string{"p9"}}})
	require.NotNil(t, ref)
	assert.Equal(t, "need p9 of producer p2 is not a card", ref.Reason)
	s.run(2)
	s.requireState("p1.w1", "working")
	_, ref = s.verb(Request{Actor: "r2", Card: "p1.w1", Event: card.Report{Row: "r2", Result: "ok", Head: sha(1), Reason: "done"}})
	assert.Equal(t, "consumer p1.w1 is held by r1, not r2", ref.Reason)
	rc, ref = s.verb(Request{Actor: "r1", Card: "p1.w1", Event: card.Report{Row: "r1", Result: "ok", Head: sha(1), Reason: "done"}})
	require.Nil(t, ref)
	assert.True(t, rc.Accepted, rc.Reason)
	s.requireState("p1.w1", "done")
	s.requireState("p1", "working") // a Report through Verb moves the consumer alone; the tick's report step or ReportPlan carries the producer
}

// TestVerbDropNamesItsDependants: a Cancel fails its closure in the same op,
// a dependant waived or relinked as the drop names.
func TestVerbDropNamesItsDependants(t *testing.T) {
	w := newWorld(t)
	w.stream("s1")
	w.row("r1", 2, "pro")
	w.producer("p1", "s1", "pro", "a/x")
	w.producer("p0", "s1", "pro", "a/v")
	w.must("add", store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p2", Needs: []string{"p1"}, Paths: []string{"a/y"}})
	w.must("add", store.PutCard{ID: "p3", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p3", Needs: []string{"p1"}, Paths: []string{"a/z"}})
	w.must("add", store.PutCard{ID: "p4", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p4", Needs: []string{"p1"}, Paths: []string{"a/w"}})
	w.must("add", store.PutCard{ID: "p5", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p5", Needs: []string{"p3"}, Paths: []string{"a/u"}})
	rc, ref := w.verb(Request{Actor: "friendF", Card: "p1", Event: card.Cancel{Reason: "dropped"},
		Drop: map[string]card.Event{"p2": card.Waive{Reason: "not needed"}, "p4": card.Relink{To: "p0", Reason: "p0 instead"}}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	s := w.snap()
	assert.Equal(t, "fail", s.Cards["p1"].State)
	assert.Equal(t, "waiting", s.Cards["p2"].State)
	assert.Empty(t, s.Cards["p2"].Needs)
	assert.Equal(t, "fail", s.Cards["p3"].State)
	assert.Equal(t, "fail", s.Cards["p5"].State, "the closure: p5 needed p3")
	assert.Equal(t, "need p3 failed", s.Cards["p5"].Reason)
	assert.Equal(t, "waiting", s.Cards["p4"].State)
	assert.Equal(t, []string{"p0"}, s.Cards["p4"].Needs)
}

// TestVerbDropReleasesLiveConsumers: a Cancel of a producer under a live work
// consumer (working from its deal: the deal is the take) releases the
// consumer in the same op, with the released record and no result, then
// fails the producer.
func TestVerbDropReleasesLiveConsumers(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.run(2)
	s.requireState("p1.w1", "working")
	rc, ref := s.verb(Request{Actor: "friendF", Card: "p1", Event: card.Cancel{Reason: "not needed"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	c := s.card("p1.w1")
	assert.Equal(t, "fail", c.State)
	assert.Empty(t, c.Result, "released is no result")
	assert.Equal(t, card.ReleasedPrefix+"not needed", c.Reason)
	assert.Equal(t, "fail", s.card("p1").State)
	assert.Equal(t, "not needed", s.card("p1").Reason)
}

// TestVerbJudgementAnswer: an open judgement is answered once with its move
// in one op; a second answer is refused naming the first.
func TestVerbJudgementAnswer(t *testing.T) {
	w := newWorld(t)
	w.stream("s1")
	w.row("r1", 2, "pro")
	w.producer("p1", "s1", "pro", "a/x")
	w.must("block", store.SetCardState{Card: "p1", State: "blocked"}, store.SetCardReason{Card: "p1", Reason: "bounced"})
	w.must("judge", store.PutJudgement{ID: "blocked.p1.1", Kind: "blocked", About: "p1", Question: "?", Command: "judge", Group: "blocked"})
	_, ref := w.verb(Request{Actor: "friendF", Judgement: "blocked.p1.1"})
	assert.Equal(t, "judgement blocked.p1.1 needs a choice: unblock | drop", ref.Reason)
	_, ref = w.verb(Request{Actor: "friendF", Judgement: "blocked.p1.9", Answer: "unblock"})
	assert.Equal(t, "judgement blocked.p1.9 does not exist: run `nova-sprint judge`", ref.Reason)
	rc, ref := w.verb(Request{Actor: "friendF", Judgement: "blocked.p1.1", Answer: "unblock", Card: "p1", Event: card.Unblock{Reason: "reviewed"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	assert.Equal(t, "waiting", w.card("p1").State)
	assert.Equal(t, "answered", w.snap().Judgements["blocked.p1.1"].State)
	_, ref = w.verb(Request{Actor: "friendF", Judgement: "blocked.p1.1", Answer: "drop"})
	assert.Equal(t, "judgement blocked.p1.1 was answered by friendF; nothing to answer", ref.Reason)
}
