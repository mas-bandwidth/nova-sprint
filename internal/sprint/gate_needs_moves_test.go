package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestARebalanceKeepsAGateNeedsCardWhereTheNeedIs: the rebalance picks the target row
// round the units that may take a card, and a card no member of the fleet may take it
// (a rebalance that would pick the darwin member as the idlest leaves the card on the
// linux source instead).
// tla/GateFacts.tla Move.
func TestARebalanceKeepsAGateNeedsCardWhereTheNeedIs(t *testing.T) {
	t.Parallel()
	w := rbWorld(t, "m1", "m2", "m3", "m4")
	for _, m := range []string{"m1", "m2", "m3"} {
		w.s.MemberCtl(m).Fields[FieldFacts] = "linux,go"
	}
	w.s.MemberCtl("m4").Fields[FieldFacts] = "darwin,go"
	// m1 over its width: one working and the linux card ready, so the linux card is its
	// excess; m2 and m3 are at their width (no free lane), m4 is idle
	rbPlace(w, "s1-w1", "pro", "m1", Working)
	rbPlace(w, "s1-2", "pro", "m2", Working)
	rbPlace(w, "s1-3", "pro", "m3", Working)
	rbPlace(w, "s1-linux", "pro", "m1", Ready, "pr.brief", "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n")
	w.must(Rebalance(w.s, nil, "machine"))
	wc := w.s.Fleet.Card("s1-linux.w1")
	require.NotNil(t, wc)
	assert.Equal(t, "m1", wc.Row, "a GATE-NEEDS linux card is never rebalanced to the darwin member")
}

// TestALevelKeepsAGateNeedsCardWhereTheNeedIs: the level moves a card off a member over
// its width only to a member whose facts carry the card's gate needs; it never moves the
// card to the darwin member.
// tla/GateFacts.tla Move.
func TestALevelKeepsAGateNeedsCardWhereTheNeedIs(t *testing.T) {
	t.Parallel()
	w := rbWorld(t, "m1", "m2", "m3", "m4")
	for _, m := range []string{"m1", "m2", "m3"} {
		w.s.MemberCtl(m).Fields[FieldFacts] = "linux,go"
	}
	w.s.MemberCtl("m4").Fields[FieldFacts] = "darwin,go"
	// m1 over its width (one working and the linux card ready), m2 and m3 at their width,
	// m4 with a free lane
	rbPlace(w, "s1-w1", "pro", "m1", Working)
	rbPlace(w, "s1-2", "pro", "m2", Working)
	rbPlace(w, "s1-3", "pro", "m3", Working)
	rbPlace(w, "s1-linux", "pro", "m1", Ready, "pr.brief", "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n")
	p := Plan{}
	level(w.s, &p, w.s.UpMembers(), dealRound(w.s), roundMoves{}, nil)
	for _, u := range p.Units {
		if u.Key == "s1-linux.w1" {
			assert.NotContains(t, u.Moved, "-> m4:", "the level never moves a GATE-NEEDS linux card to the darwin member")
		}
	}
}

// TestARedealOfADownMembersCardKeepsItsGateNeeds: a working GATE-NEEDS linux card on a
// member that goes down is never redealt to the darwin member; when no member whose facts
// carry linux is up to take it, the redeal raises the gate-host judgment as the deal does.
// tla/GateFacts.tla Move.
func TestARedealOfADownMembersCardKeepsItsGateNeeds(t *testing.T) {
	t.Parallel()
	w := rbWorld(t, "m1", "m2", "m3", "m4")
	for _, m := range []string{"m1", "m2", "m3"} {
		w.s.MemberCtl(m).Fields[FieldFacts] = "linux,go"
	}
	w.s.MemberCtl("m4").Fields[FieldFacts] = "darwin,go"
	// m2 and m3 go down (held), so the only member up to take m1's card is m4 (darwin)
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m3"}))
	rbPlace(w, "s1-linux", "pro", "m1", Working, "pr.brief", "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n")
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	if wc := w.s.Fleet.Card("s1-linux.w1"); wc != nil {
		assert.NotEqual(t, "m4", wc.Row, "a down member's GATE-NEEDS linux card is never redealt to the darwin member")
	}
	notes := w.notesOf(NGateNeeds)
	require.Len(t, notes, 1, "no member whose facts carry linux is up to take it: one gate-host judgment")
}
