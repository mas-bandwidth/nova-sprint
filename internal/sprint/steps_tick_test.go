package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The starving judgment carries its numbers (docs/SPEC-SPRINT.md section 5:
// a member holds up to DealAhead times its width, ready and working together):
// ready beside twice the width and working beside width, so "release a wave"
// is said only with the numbers, never as a bare command, and the judgment is
// updated in place as the dealt cards move to working.
func TestNStarvingCarriesWorkingAndWidth(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 2}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Width: 2}))
	// three cards ready, nothing working, and a wave held behind a sentinel
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 3}))
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"s2-gate"}, Sentinel: true, Held: true}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s2", Count: 4}))

	p, _ := TickDeal(w.s, TickReq{})
	var raised []Note
	for _, n := range p.Notes {
		if n.Type == NStarving {
			raised = append(raised, n)
		}
	}
	require.Len(t, raised, 1, "the starving judgment")
	n := raised[0]
	assert.Equal(t, "the fleet is starving: ready 3 is under twice the width 8, working 0 of width 4; release a wave: nova-sprint release s2-gate --reason '<why>'", n.What)
	assert.Equal(t, []string{"release", "wait"}, n.Decisions, "the wave's sentinel, never a single card")
	assert.Equal(t, []string{"s2-gate"}, n.Primaries)
	// "release a wave" appears only with the numbers, never as a decision
	for _, d := range n.Decisions {
		assert.NotContains(t, d, "release a wave")
	}

	// the same plan deals the three to the members, and a take starts one:
	// the open judgment is updated in place with the new numbers
	w.must(p)
	require.NotEmpty(t, w.s.Fleet.Cell("m1", Ready))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 1}}))
	require.Equal(t, 1, w.s.Fleet.Count("m1", Working)+w.s.Fleet.Count("m2", Working))

	p2, _ := TickDeal(w.s, TickReq{})
	var updated []Note
	for _, u := range p2.Updates {
		if u.Type == NStarving {
			updated = append(updated, u)
		}
	}
	require.Len(t, updated, 1, "the starving judgment updated in place")
	u := updated[0]
	assert.Equal(t, "the fleet is starving: ready 0 is under twice the width 8, working 1 of width 4; release a wave: nova-sprint release s2-gate --reason '<why>'", u.What)
	assert.Equal(t, []string{"release", "wait"}, u.Decisions)
}

// The all-dealt case has its own test: every ready card dealt and one taken
// into working on each member, ready is 0 while the wave is held, and the
// judgment still names its numbers — ready 0, the working count, the width —
// beside "release a wave".
func TestNStarvingAllDealt(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 2}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Width: 2}))
	// two cards, dealt round the fleet, then each member takes one into working
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 2}))
	p, _ := TickDeal(w.s, TickReq{})
	w.must(p)
	require.Equal(t, 2, w.s.Fleet.Count("m1", Ready)+w.s.Fleet.Count("m2", Ready), "the two dealt to the members")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 1}}))
	w.must(Take(w.s, TakeReq{As: "m2", Sel: Sel{Limit: 1}}))
	require.Equal(t, 2, w.s.Fleet.Count("m1", Working)+w.s.Fleet.Count("m2", Working))
	require.Empty(t, w.s.Work.Column(Ready), "all dealt")

	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"s2-gate"}, Sentinel: true, Held: true}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s2", Count: 4}))

	p2, _ := TickDeal(w.s, TickReq{})
	var raised []Note
	for _, n := range p2.Notes {
		if n.Type == NStarving {
			raised = append(raised, n)
		}
	}
	require.Len(t, raised, 1, "the starving judgment with everything dealt")
	n := raised[0]
	assert.Equal(t, "the fleet is starving: ready 0 is under twice the width 8, working 2 of width 4; release a wave: nova-sprint release s2-gate --reason '<why>'", n.What)
	assert.Equal(t, []string{"release", "wait"}, n.Decisions)
	assert.Equal(t, []string{"s2-gate"}, n.Primaries)
}
