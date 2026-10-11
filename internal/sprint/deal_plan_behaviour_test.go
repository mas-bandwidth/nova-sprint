package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dealCard is the work card the primary id holds after a deal.
func dealCard(w *world, id string) *Card { return w.s.Fleet.Card(w.s.Work.Card(id).F("work")) }

// Deal: "the fleet's work is off: no card is dealt to a machine; run:
// nova-sprint set --fleet on" (steps_work.go:994).
func TestEveryChosenCardIsRefusedWhenTheFleetsWorkIsOff(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Work.SetProp(PropFleet, SwitchOff)
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, p.Units, "a card was dealt with the fleet's work off: %+v", p)
	require.Len(t, p.Refused, 1, "the refusal: %+v", p.Refused)
	assert.Equal(t, "s1-1", p.Refused[0].Key)
	assert.Contains(t, p.Refused[0].Why, "run: nova-sprint set --fleet on")
	assert.Equal(t, Ready, w.state("s1-1"))
}

// Deal: "no fleet member is up: a member is up while its machine beats"
// (steps_work.go:1000) -- with none up, and with every up member held by an
// uncleared environment fault (steps_work.go:991).
func TestEveryCardIsRefusedWhenNoUpMemberCanTakeIt(t *testing.T) {
	t.Parallel()
	t.Run("no member up", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
		w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
		p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
		require.Empty(t, p.Units, "a card was dealt with nobody up: %+v", p)
		require.Len(t, p.Refused, 1, "the refusal: %+v", p.Refused)
		assert.Contains(t, p.Refused[0].Why, "no fleet member is up")
	})
	t.Run("every up member env-held", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		for _, m := range []string{"m1", "m2"} {
			ctl := w.s.MemberCtl(m)
			ctl.Fields[FieldEnvFault] = "disk full"
			ctl.Fields[FieldEnvFaultReason] = "no space on /slots"
			ctl.Rev++
		}
		p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
		require.Empty(t, p.Units, "a card was dealt to an env-held fleet: %+v", p)
		require.Len(t, p.Refused, 1, "the refusal: %+v", p.Refused)
		assert.Contains(t, p.Refused[0].Why, "no fleet member is up")
	})
}

// Deal: "a card whose brief says BENCH: is dealt only to the members it names";
// while no member of its bench is up it waits in ready (bench_deal.go:131,
// steps_work.go:1015).
func TestABenchCardWhoseMemberIsDownIsRefusedWithTheBenchsWords(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	addBenched(t, w, "s1", map[string]string{"s1-1": "m1"})
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, p.Units, "a bench card was dealt off its bench: %+v", p)
	require.Len(t, p.Refused, 1, "the refusal: %+v", p.Refused)
	assert.Contains(t, p.Refused[0].Why, "waits for its bench m1")
	assert.Contains(t, p.Refused[0].Why, "run: nova-sprint fleet beat m1")
}

// Deal: a bench card whose member is up and at its room is refused with
// benchRoom (bench_deal.go:143, steps_work.go:1020).
func TestABenchCardWhoseMemberIsAtItsRoomIsRefusedWithTheBenchRoom(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	addBenched(t, w, "s1", map[string]string{"s1-1": "m1", "s1-2": "m1", "s1-3": "m1"})
	w.s.MemberCtl("m1").Fields[FieldWidth] = "1"
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1", "s1-2", "s1-3"}}})
	require.Len(t, p.Units, 2, "the room of a width-1 bench is DealAhead*1: %+v", p)
	require.Len(t, p.Refused, 1, "the refusal: %+v", p.Refused)
	assert.Contains(t, p.Refused[0].Why, "every member of its bench m1 is at its room")
}

// Deal: "a quiet member is dealt nothing until its quiet ends"; the refusal is
// the room words, then "; " and quietWhy's words (fleet_quiet.go:126,
// steps_work.go:1022).
func TestACardWhoseOnlyUpMemberIsAtRoomAndTheOtherQuietIsRefusedWithBoth(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", IDs: []string{"s1-1", "s1-2", "s1-3"}}))
	w.s.MemberCtl("m2").Fields[FieldWidth] = "1"
	until := w.s.Now.Add(time.Hour)
	w.must(FleetStep(w.s, FleetReq{Op: "quiet", Member: "m1", Until: until, Reason: "load 64", Who: "coordinator"}))
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1", "s1-2", "s1-3"}}})
	require.Len(t, p.Units, 2, "the quiet member took cards or the room was not filled: %+v", p)
	require.Len(t, p.Refused, 1, "the refusal: %+v", p.Refused)
	assert.Contains(t, p.Refused[0].Why, noRoomWhy+"; quiet: m1 until "+stamp(until))
}

// Deal: "a card withdrawn because no member was up is the same card dealt again
// at a new generation, its attempt unchanged" (steps_work.go:951-953).
func TestAWithdrawnCardIsDealtAgainAtANewGeneration(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	first := dealCard(w, "s1-1")
	cardID := first.ID
	w.must(Take(w.s, TakeReq{As: first.Row, Sel: Sel{IDs: []string{first.ID}}, Gens: gensOf(w.s, first.ID)}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	gone := w.s.Fleet.Card(cardID)
	require.NotNil(t, gone, "the withdrawn card")
	require.Equal(t, Withdrawn, gone.Col, "the card is %s at %s", gone.Col, gone.Row)
	genW := gone.Int("gen")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	again := w.s.Fleet.Card(cardID)
	assert.Greater(t, again.Int("gen"), genW, "the card was not dealt again at a new generation")
	assert.Equal(t, 1, again.Int("attempt"), "the attempt changed on a withdrawal")
	assert.Equal(t, "s1-1.w1", again.ID, "a withdrawal cut a new attempt")
}

// Deal: "the first ... that is up and holds fewer work cards ... than its width
// ... a card no up member has room for is refused, never dealt past a width"
// (steps_work.go:947, width.go:119; tla/DirtyTick.tla, WidthRespected).
func TestASelectionOverTwoMembersOfWidthOneNeverPassesTheRoom(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", IDs: []string{"s1-1", "s1-2", "s1-3", "s1-4", "s1-5"}}))
	for _, m := range []string{"m1", "m2"} {
		w.s.MemberCtl(m).Fields[FieldWidth] = "1"
	}
	p := w.do(Deal(w.s, DealReq{Sel: Sel{Limit: 5}}))
	require.Len(t, p.Units, 4, "the deal did not fill both members to DealAhead*1: %+v", p)
	assert.Equal(t, 2, w.s.Fleet.Count("m1", Ready), "m1 not filled to its room")
	assert.Equal(t, 2, w.s.Fleet.Count("m2", Ready), "m2 not filled to its room")
	require.Len(t, p.Refused, 1, "the card past the room: %+v", p.Refused)
	assert.Contains(t, p.Refused[0].Why, "every up fleet member is at its room")
}
