package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every move of an attempt holds the card's GATE-NEEDS against the member's facts and gates
// (gateEligible, gates_only.go; tla/GatesOnly.tla, Move): the next attempt's deal, the redeal
// of a member gone down, the level and the rebalance, as the first deal does. Before v1.6.8
// the first deal gave a GATE-NEEDS darwin, arm64 card to the one Mac and the level, the
// rebalance and the rework's next attempt moved it to Linux benches, where every attempt
// ended HOLD and the bound marked the brief defective. Unit tests: no time but the world's,
// no sockets.

const darwinBrief = "c: a macOS card tier: pro\nREPO: mas-bandwidth/nova-tools\nGATE-NEEDS: darwin, arm64\n\nThe task."

// macFacts gives mac the facts of the fleet's Mac and lin those of a Linux bench.
func macFacts(w *world, mac, lin string) {
	w.s.MemberCtl(mac).Fields[FieldFacts] = "darwin,arm64,tla,go"
	w.s.MemberCtl(lin).Fields[FieldFacts] = "linux,amd64,landlock,tla,go"
}

func TestGateEligibleHoldsGatesAndFactsTogether(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 2, "mac", "lin", "win")
	macFacts(w, "mac", "lin")
	w.s.MemberCtl("win").Fields[FieldFacts] = "windows,amd64"
	w.s.MemberCtl("win").Fields[FieldGatesOnly] = "windows"
	darwin := &Card{ID: "d", Fields: map[string]string{"brief": darwinBrief}}
	generic := &Card{ID: "g", Fields: map[string]string{"brief": fleetBrief("pro")}}
	unknown := &Card{ID: "u", Fields: map[string]string{"brief": "c: x\nGATE-NEEDS: plan9\n\nThe task."}}
	all := []string{"mac", "lin", "win"}
	assert.Equal(t, []string{"mac"}, w.s.AdmitGate(darwin, all), "a darwin, arm64 card: the Mac alone")
	assert.Equal(t, []string{"mac", "lin"}, w.s.AdmitGate(generic, all), "a generic card: every member but the gates-only one")
	assert.Empty(t, w.s.AdmitGate(unknown, all), "a need no member carries: no member, never a silent deal")
	assert.Equal(t, []string{"mac", "lin", "win"}, all, "the pool given is never changed")
	w.s.MemberCtl("lin").Fields[FieldFacts] = ""
	assert.False(t, w.s.gateEligible(darwin, "lin"), "a member with no facts satisfies no need")
	assert.True(t, w.s.gateEligible(generic, "lin"), "and still takes a card that names none")
}

// The level evens the queues only onto a member the card's needs admit.
func TestTheLevelNeverMovesADarwinCardToALinuxBench(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, brief string
		onLin       int
	}{
		{"darwin card stays on the Mac", darwinBrief, 0},
		{"generic card is levelled as before", fleetBrief("pro"), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := fleetWorld(t, 0, 2, "mac", "lin")
			macFacts(w, "mac", "lin")
			for i, id := range []string{"a", "b", "c", "d"} {
				col := Working
				if i >= 2 {
					col = Ready
				}
				putWorkCard(w, id, "mac", col, float64(i+1), nil)
				w.s.Work.Card(id).Fields["brief"] = tc.brief
			}
			w.part(TickLevel, TickReq{})
			assert.Equal(t, tc.onLin, w.s.Fleet.Count("lin", Ready))
			assert.Equal(t, 2-tc.onLin, w.s.Fleet.Count("mac", Ready))
		})
	}
}

// A member gone down: its darwin card is withdrawn to wait for a member that satisfies it,
// never dealt again to the Linux bench up beside it.
func TestADownMacsCardIsWithdrawnNeverRedealtToLinux(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 0, 4, "mac", "lin")
	w.s.Work.SetRows([]string{"s1"})
	macFacts(w, "mac", "lin")
	putWorkCard(w, "a", "mac", Ready, 1, nil)
	w.s.Work.Card("a").Fields["brief"] = darwinBrief
	putWorkCard(w, "g", "mac", Ready, 2, nil)
	w.s.Work.Card("g").Fields["brief"] = fleetBrief("pro")
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "mac", Who: "coordinator"}))
	assert.Equal(t, Withdrawn, w.s.Fleet.Card("a.w1").Col, "the darwin card is taken back")
	assert.NotEqual(t, "lin", w.s.Fleet.Card("a.w1").Row)
	assert.Equal(t, "lin", w.s.Fleet.Card("g.w1").Row, "the generic card moves to the bench up as before")
}

// The rebalance moves a queued card to an idle machine lane only when the machine satisfies it.
func TestTheRebalanceNeverMovesADarwinCardToALinuxBench(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, brief, want string
	}{
		{"darwin card stays on the Mac", darwinBrief, "mac"},
		{"generic card takes the idle lane", fleetBrief("pro"), "lin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := rbWorld(t, "mac", "lin")
			macFacts(w, "mac", "lin")
			rbPlace(w, "s1-1", "pro", "mac", Working, "pr.brief", tc.brief)
			wc := rbPlace(w, "s1-2", "pro", "mac", Ready, "pr.brief", tc.brief)
			w.must(Rebalance(w.s, nil, "machine"))
			assert.Equal(t, tc.want, w.s.Fleet.Card(wc.ID).Row)
		})
	}
}

// A rework's next attempt avoids the member that failed it, but never by giving the card to
// a member that cannot satisfy it: with the Mac the one darwin member, attempt 2 is the Mac's.
func TestAReworkOfADarwinCardIsNeverDealtToLinux(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	macFacts(w, "m1", "m2")
	w.s.Work.Card("s1-1").Fields["brief"] = darwinBrief
	pushedFirstAttempt(w, "s1-1", carryHead1)
	require.Equal(t, "m1", w.s.Fleet.Card("s1-1.w1").Row, "the first deal gives it the Mac")
	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "run the proofs"}))
	if wc := w.s.Fleet.Card("s1-1.w2"); wc != nil {
		assert.NotEqual(t, "m2", wc.Row, "attempt 2 is never on the Linux bench")
	}
	w.part(TickDeal, TickReq{})
	wc := w.s.Fleet.Card("s1-1.w2")
	require.NotNil(t, wc, "attempt 2 is dealt")
	assert.Equal(t, "m1", wc.Row, "attempt 2 is the Mac's")
}

// A conflict redo deals the next attempt as the rework does: never to a member the card's
// needs refuse, here with the Mac the member that held attempt 1 (the one the redo avoids).
func TestAConflictRedoOfADarwinCardIsNeverDealtToLinux(t *testing.T) {
	t.Parallel()
	w := stoppedForConflict(t)
	mac := w.s.Fleet.Card("s1-2.w1").Row
	lin := "m2"
	if mac == "m2" {
		lin = "m1"
	}
	macFacts(w, mac, lin)
	w.s.Work.Card("s1-2").Fields["brief"] = darwinBrief
	w.must(Redo(w.s, RedoReq{Sel: Sel{IDs: []string{"s1-2"}}, Who: "coordinator"}))
	if wc := w.s.Fleet.Card("s1-2.w2"); wc != nil {
		assert.Equal(t, mac, wc.Row, "attempt 2 is the Mac's, never the Linux bench's")
	}
}

// The other direction of the same rule, path by path: a gates-only member (captain, gates_only
// windows) is never moved a card whose GATE-NEEDS names none of its gates. Seen live on
// v1.6.7: elixir-defaults-and-table-rename (GATE-NEEDS: linux) was rebalanced from amd-1 to
// captain at 18:49 and levelled from amd-3 to captain at 19:13. The table covers every move:
// the level, the rebalance, the redeal of a member gone down, the rework and the conflict redo.
func TestNoMoveGivesAGatesOnlyMemberACardNamingNoneOfItsGates(t *testing.T) {
	t.Parallel()
	const linuxBrief = "c: a linux card tier: pro\nREPO: mas-bandwidth/nova-tools\nGATE-NEEDS: linux\n\nThe task."
	winFacts := func(w *world, win string) {
		w.s.MemberCtl(win).Fields[FieldFacts] = "windows,amd64,go,dotnet,java"
		w.s.MemberCtl(win).Fields[FieldGatesOnly] = "windows"
	}
	for _, brief := range []string{linuxBrief, fleetBrief("pro")} {
		t.Run("level", func(t *testing.T) {
			t.Parallel()
			w := fleetWorld(t, 0, 2, "lin", "win")
			w.s.MemberCtl("lin").Fields[FieldFacts] = "linux,amd64,go"
			winFacts(w, "win")
			for i, id := range []string{"a", "b", "c", "d"} {
				col := Working
				if i >= 2 {
					col = Ready
				}
				putWorkCard(w, id, "lin", col, float64(i+1), nil)
				w.s.Work.Card(id).Fields["brief"] = brief
			}
			w.part(TickLevel, TickReq{})
			assert.Equal(t, 0, w.s.Fleet.Count("win", Ready), "the level never feeds the gates-only member")
		})
		t.Run("rebalance", func(t *testing.T) {
			t.Parallel()
			w := rbWorld(t, "lin", "win")
			w.s.MemberCtl("lin").Fields[FieldFacts] = "linux,amd64,go"
			winFacts(w, "win")
			rbPlace(w, "s1-1", "pro", "lin", Working, "pr.brief", brief)
			wc := rbPlace(w, "s1-2", "pro", "lin", Ready, "pr.brief", brief)
			w.must(Rebalance(w.s, nil, "machine"))
			assert.Equal(t, "lin", w.s.Fleet.Card(wc.ID).Row, "the rebalance never moves it to the gates-only member")
		})
		t.Run("redeal of a member gone down", func(t *testing.T) {
			t.Parallel()
			w := fleetWorld(t, 0, 4, "lin", "win")
			w.s.Work.SetRows([]string{"s1"})
			w.s.MemberCtl("lin").Fields[FieldFacts] = "linux,amd64,go"
			winFacts(w, "win")
			putWorkCard(w, "a", "lin", Ready, 1, nil)
			w.s.Work.Card("a").Fields["brief"] = brief
			w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "lin", Who: "coordinator"}))
			assert.Equal(t, Withdrawn, w.s.Fleet.Card("a.w1").Col, "withdrawn, never redealt to the gates-only member")
		})
		t.Run("rework", func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			w.s.Work.Card("s1-1").Fields["brief"] = brief
			w.s.MemberCtl("m1").Fields[FieldFacts] = "linux,amd64,go"
			w.s.MemberCtl("m2").Fields[FieldFacts] = "linux,amd64,go"
			pushedFirstAttempt(w, "s1-1", carryHead1)
			first := w.s.Fleet.Card("s1-1.w1").Row
			other := "m2"
			if first == "m2" {
				other = "m1"
			}
			winFacts(w, other)
			w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "again"}))
			w.part(TickDeal, TickReq{})
			if wc := w.s.Fleet.Card("s1-1.w2"); wc != nil {
				assert.Equal(t, first, wc.Row, "attempt 2 never goes to the gates-only member")
			}
		})
		t.Run("conflict redo", func(t *testing.T) {
			t.Parallel()
			w := stoppedForConflict(t)
			first := w.s.Fleet.Card("s1-2.w1").Row
			other := "m2"
			if first == "m2" {
				other = "m1"
			}
			w.s.MemberCtl(first).Fields[FieldFacts] = "linux,amd64,go"
			winFacts(w, other)
			w.s.Work.Card("s1-2").Fields["brief"] = brief
			w.must(Redo(w.s, RedoReq{Sel: Sel{IDs: []string{"s1-2"}}, Who: "coordinator"}))
			if wc := w.s.Fleet.Card("s1-2.w2"); wc != nil {
				assert.Equal(t, first, wc.Row, "the redo never deals it to the gates-only member")
			}
		})
	}
}
