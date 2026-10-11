package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The deal's plan and its refusals, driven through Deal (steps_work.go:954) and
// asserted on what a caller sees: the units it places and the text of each
// refusal. The contract is Deal's doc comment (steps_work.go:943-953), with
// round.go, width.go, fleet_quiet.go, bench_deal.go and tla/DirtyTick.tla
// Room and WidthRespected. dealPlan is never called directly.

// TestFleetOffRefusesEveryChosenCard: with the fleet's work off (nova-sprint
// set --fleet off) every chosen card is refused and nothing is dealt
// (steps_work.go:992-996; Deal's doc comment).
func TestFleetOffRefusesEveryChosenCard(t *testing.T) {
	t.Parallel()
	w := setup(t, 3)
	w.s.Work.SetProp(PropFleet, "off")
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 3}})
	require.Len(t, p.Refused, 3, "every chosen card is refused: %+v", p)
	for _, r := range p.Refused {
		assert.Contains(t, r.Why, "run: nova-sprint set --fleet on", "refusal of %s: %s", r.Key, r.Why)
	}
	assert.Empty(t, p.Units, "nothing is dealt while the fleet's work is off: %+v", p.Units)
}

// TestDealRefusesEveryCardWhenNoMemberIsUp: with no member up, and with every
// up member held by an uncleared environment fault, each card is refused
// "no fleet member is up" and nothing is dealt (steps_work.go:998-1003;
// tla/ProviderBudget.tla).
func TestDealRefusesEveryCardWhenNoMemberIsUp(t *testing.T) {
	t.Parallel()
	t.Run("no member up", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, "reader-a")
		w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 2}))
		p := Deal(w.s, DealReq{Sel: Sel{Limit: 2}})
		require.Len(t, p.Refused, 2, "each card is refused: %+v", p)
		for _, r := range p.Refused {
			assert.Contains(t, r.Why, "no fleet member is up", "refusal of %s: %s", r.Key, r.Why)
		}
		assert.Empty(t, p.Units, "nothing is dealt: %+v", p.Units)
	})
	t.Run("every up member environment-held", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Probe: EnvProbePassed}))
		w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 1}))
		w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
		w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1")}))
		w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Failed: true,
			Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"}))
		require.True(t, w.s.EnvHeld("m1"), "m1 is held by its environment fault")
		p := Deal(w.s, DealReq{Sel: Sel{Limit: 1}})
		require.Len(t, p.Refused, 1, "the card is refused: %+v", p)
		for _, r := range p.Refused {
			assert.Contains(t, r.Why, "no fleet member is up", "refusal of %s: %s", r.Key, r.Why)
		}
		assert.Empty(t, p.Units, "nothing is dealt: %+v", p.Units)
	})
}

// TestABenchCardWhoseMemberIsDownIsRefusedWithTheBenchWords: a card whose BENCH
// line names a member that is down waits for its bench and is refused with the
// bench's words (steps_work.go:1008-1011; bench_deal.go:135-140).
func TestABenchCardWhoseMemberIsDownIsRefusedWithTheBenchWords(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Probe: EnvProbePassed}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Probe: EnvProbePassed}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	w.must(Add(w.s, AddReq{Brief: benchBrief("m2"), Stream: "s1", Count: 3}))
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 3}})
	require.Len(t, p.Refused, 3, "every bench card waits: %+v", p)
	for _, r := range p.Refused {
		assert.Contains(t, r.Why, "waits for its bench m2", "refusal of %s: %s", r.Key, r.Why)
		assert.Contains(t, r.Why, "nova-sprint fleet beat m2", "refusal of %s: %s", r.Key, r.Why)
	}
	assert.Empty(t, p.Units, "a card of a down bench is never dealt: %+v", p.Units)
}

// TestABenchCardWhoseMemberIsAtItsRoomIsRefusedWithTheRoomWords: a card whose
// bench is up and every member of it is at its room (DealAhead times its width)
// is refused with the bench room words (steps_work.go:1012-1014; bench_deal.go:141-147).
func TestABenchCardWhoseMemberIsAtItsRoomIsRefusedWithTheRoomWords(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Probe: EnvProbePassed, Width: 1}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Probe: EnvProbePassed, Width: 1}))
	w.must(Add(w.s, AddReq{Brief: benchBrief("m2"), Stream: "s1", Count: 3}))
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 3}})
	require.Len(t, p.Units, 2, "m2 takes DealAhead times its width, 2: %+v", p.Units)
	require.Len(t, p.Refused, 1, "the card past the room is refused: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "every member of its bench m2 is at its room", "refusal: %s", p.Refused[0].Why)
	for _, u := range p.Units {
		assert.Contains(t, u.Moved, "member=m2", "a bench card is dealt only to its bench: %+v", u)
	}
}

// TestAQuietAndARoomRefusalNamesTheRoomThenTheQuiet: with one member quiet and
// the other at its room the refusal is the room words, then "; " and quietWhy's
// words naming "<m> until <t>" (steps_work.go:1012-1016; fleet_quiet.go:126).
func TestAQuietAndARoomRefusalNamesTheRoomThenTheQuiet(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Probe: EnvProbePassed, Width: 1}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Probe: EnvProbePassed, Width: 1}))
	until := w.s.Now.Add(time.Hour)
	w.must(FleetStep(w.s, FleetReq{Op: "quiet", Member: "m1", Until: until, Reason: "the gocache trim", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 3}))
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 3}})
	require.Len(t, p.Units, 2, "the unquiet, roomy member takes 2: %+v", p.Units)
	require.Len(t, p.Refused, 1, "the card past the room is refused: %+v", p)
	why := p.Refused[0].Why
	assert.Contains(t, why, "every up fleet member is at its room", "room words come first: %s", why)
	assert.Contains(t, why, "; quiet: m1 until "+stamp(until), "then the quiet words naming m1 until t: %s", why)
}

// TestASelectionPastTheRoomIsRefusedWithTheRoomWords: a selection over two
// members of width 1 is dealt round the fleet, never past DealAhead times a
// member's width, and the card past the room is refused with the room words
// (Deal's doc comment; width.go DealAhead, tla/DirtyTick.tla WidthRespected).
func TestASelectionPastTheRoomIsRefusedWithTheRoomWords(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Probe: EnvProbePassed, Width: 1}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Probe: EnvProbePassed, Width: 1}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 5}))
	p := Deal(w.s, DealReq{Sel: Sel{Limit: 5}})
	require.Len(t, p.Units, 4, "two members of room 2 take four: %+v", p.Units)
	require.LessOrEqual(t, w.s.Fleet.Count("m1", Ready)+w.s.Fleet.Count("m1", Working), DealAhead, "m1 is never past DealAhead times its width")
	require.LessOrEqual(t, w.s.Fleet.Count("m2", Ready)+w.s.Fleet.Count("m2", Working), DealAhead, "m2 is never past DealAhead times its width")
	require.Len(t, p.Refused, 1, "the card past the room is refused: %+v", p)
	assert.Equal(t, noRoomWhy, p.Refused[0].Why, "the refusal is the room words")
}
