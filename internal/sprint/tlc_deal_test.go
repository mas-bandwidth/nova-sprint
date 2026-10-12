package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A card that needs TLC (its PATHS name tla/ model work, or its brief says NEEDS: tlc)
// is dealt only to a member whose beat reports it can run TLC, and while none is up it
// waits in ready with the no-stall rule saying why (tlc_deal.go; docs/SPEC-SPRINT.md
// section 5, the deal).

// tlcBrief is a card's brief with the PATHS line given.
func tlcBrief(line1, paths string) string {
	return line1 + "\nREPO: mas-bandwidth/nova-sprint\nPATHS: " + paths + "\nTEST: ./internal/sprint TestX\n\nThe task."
}

// tlcBeats is m1's beat reporting TLC and m2's beat reporting none.
func tlcBeats() map[string]Beat {
	return map[string]Beat{
		"m1": {At: t0, TLC: true, TLCJar: "/opt/tla/tla2tools.jar"},
		"m2": {At: t0},
	}
}

// tlcTick is one tick's deal with the beats given, then the level.
func tlcTick(w *world, beats map[string]Beat) {
	w.t.Helper()
	w.part(TickDeal, TickReq{Beats: beats})
	w.part(TickLevel, TickReq{})
}

func TestNeedsTLCReadsModelPathsAndTheNeedsLine(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	p := Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "m1", Brief: tlcBrief("m1: a model", "tla/Lease.tla,tla/MCLease.cfg")},
		{ID: "m2", Brief: tlcBrief("m2: a config", "tla/MCLease.cfg")},
		{ID: "m3", Brief: "m3: a needed card\nREPO: mas-bandwidth/nova-sprint\nNEEDS: tlc\nTEST: ./internal/sprint TestX\n\nThe task."},
		{ID: "g1", Brief: tlcBrief("g1: go only", "internal/sprint/*.go")},
		{ID: "r1", Brief: tlcBrief("r1: run records", "tla/RUNS.tsv,tla/CASES.tsv")},
	}})
	require.Empty(t, p.Refused)
	w.do(p)
	assert.True(t, NeedsTLC(w.s.Primary("m1")), "a tla module needs TLC")
	assert.True(t, NeedsTLC(w.s.Primary("m2")), "an MC config needs TLC")
	assert.True(t, NeedsTLC(w.s.Primary("m3")), "NEEDS: tlc needs TLC")
	assert.False(t, NeedsTLC(w.s.Primary("g1")), "a go-only card needs no TLC")
	assert.False(t, NeedsTLC(w.s.Primary("r1")), "the run records alone are no model")
}

func TestATLACardGoesToTheMemberWithTLC(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 0, "m1", "m2")
	p := Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "s1-1", Brief: tlcBrief("s1-1: a model", "tla/Lease.tla")},
		{ID: "s1-2", Brief: tlcBrief("s1-2: go only", "internal/sprint/*.go")},
	}})
	require.Empty(t, p.Refused)
	w.do(p)
	tlcTick(w, tlcBeats())
	assert.Equal(t, Working, w.s.StateOf("s1-1"), "the TLA+ card is dealt")
	assert.Equal(t, "m1", benchRow(w.s, "s1-1"), "and only to the member that reports TLC")
	assert.Equal(t, Working, w.s.StateOf("s1-2"), "a card that touches no tla file is dealt")
}

func TestATLACardWaitsWhenNoMemberWithTLCIsUp(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 0, "m2")
	p := Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "s1-1", Brief: tlcBrief("s1-1: a model", "tla/Lease.tla")},
	}})
	require.Empty(t, p.Refused)
	w.do(p)
	tlcTick(w, map[string]Beat{"m2": {At: t0}})
	assert.Equal(t, Ready, w.s.StateOf("s1-1"), "the card stays ready while no member with TLC is up")
	assert.Equal(t, "-", benchRow(w.s, "s1-1"), "no member without TLC is dealt it")
	hd := Holder(running(w), w.s.Now, "s1-1")
	assert.Contains(t, hd.Why, "needs TLC: no member with TLC is up", "the stall names the requirement")
}

func TestACardThatTouchesNoTLAFileIsDealtToEitherMember(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 0, "m1", "m2")
	p := Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "s1-1", Brief: tlcBrief("s1-1: go only", "internal/sprint/*.go")},
	}})
	require.Empty(t, p.Refused)
	w.do(p)
	tlcTick(w, map[string]Beat{"m1": {At: t0}, "m2": {At: t0}})
	assert.Equal(t, Working, w.s.StateOf("s1-1"), "a card that needs no TLC is dealt to a member that reports none")
}
