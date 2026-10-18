package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func landingMarkSoftWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = time.Unix(1000, 0).UTC()
	w.readersTable()
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	brief := func(id, after string) string {
		soft := ""
		if after != "" {
			soft = "LAND-AFTER: " + after + "\n"
		}
		return "RESULT: " + id + " sha=000000000000 tier: flash\nREPO: mas-bandwidth/nova-sprint\nBASE: main\n" + soft + "PATHS: internal/" + id + ".go\n\ntest"
	}
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "a", Brief: brief("a", "c")},
		{ID: "b", Brief: brief("b", "")},
		{ID: "c", Brief: brief("c", "")},
	}}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 3}}))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"a.w1", "b.w1", "c.w1"}}, Gens: gensOf(w.s, "a.w1", "b.w1", "c.w1")}))
	w.must(Ask(w.s, AskReq{}))
	for _, id := range []string{"a", "b", "c"} {
		for _, rc := range readsAt(w.s, w.s.Work.Card(id), 1) {
			w.must(Read(w.s, ReadReq{Usage: "input=1 output=1", As: rc.F("reader"), Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
		}
		w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{id}}}))
	}
	return w
}

func TestMarkLandingStopsAtSoftBlockedPinAndMarksPrefix(t *testing.T) {
	t.Parallel()
	w := landingMarkSoftWorld(t)
	head := func(id string) string { return w.s.Work.Card(id).F("head") }
	p := MarkLanding(w.s, "s1", []PushedPin{{ID: "b", Head: head("b")}, {ID: "a", Head: head("a")}})
	require.Len(t, p.Refused, 1)
	assert.Equal(t, "a", p.Refused[0].Key)
	assert.Equal(t, "soft dependency on c not landed; a is skipped this batch", p.Refused[0].Why)
	w.do(p)
	assert.True(t, LandingMarked(w.s, "b"))
	assert.False(t, LandingMarked(w.s, "a"))
}

func TestMarkLandingAcceptsSoftTargetAheadInPins(t *testing.T) {
	t.Parallel()
	w := landingMarkSoftWorld(t)
	head := func(id string) string { return w.s.Work.Card(id).F("head") }
	p := MarkLanding(w.s, "s1", []PushedPin{{ID: "c", Head: head("c")}, {ID: "a", Head: head("a")}})
	require.Empty(t, p.Refused)
	w.must(p)
	assert.True(t, LandingMarked(w.s, "c"))
	assert.True(t, LandingMarked(w.s, "a"))
}
