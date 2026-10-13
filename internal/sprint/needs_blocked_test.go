package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestABoundCardRaisesOneBlockerJudgmentListingItsDependents(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "tools-v1217", IDs: []string{"a-route-may-be-frontier"}}))
	w.note(judgment(NBound, "tools-v1217", w.s.Now, 0, "a-route-may-be-frontier"))
	w.must(Add(w.s, AddReq{Stream: "tools-v1217", IDs: []string{"muse-is-a-fleet-harness"}, Needs: []string{"a-route-may-be-frontier"}}))
	w.must(Add(w.s, AddReq{Stream: "tools-v1217", IDs: []string{"a-funded-provider-is-a-config-row-and-always-shown"}, Needs: []string{"a-route-may-be-frontier"}}))
	w.must(Add(w.s, AddReq{Stream: "tools-v1217", IDs: []string{"the-server-unit-carries-the-bus-address"}, Needs: []string{"muse-is-a-fleet-harness"}}))

	// Run TickNeeds, Resolve, TickDeadlines and TickOverdue at s.Now + 3 h:
	w.s.Now = w.s.Now.Add(3 * time.Hour)
	req := TickReq{}
	pNeeds, _ := TickNeeds(w.s, req)
	pResolve := Resolve(w.s, ResolveReq{})
	pDeadlines, _ := TickDeadlines(w.s, req)
	pOverdue, _ := TickOverdue(w.s, req)
	pBlocked, _ := TickNeedBlocked(w.s, req)

	allNotes := append([]Note(nil), pNeeds.Notes...)
	allNotes = append(allNotes, pResolve.Notes...)
	allNotes = append(allNotes, pDeadlines.Notes...)
	allNotes = append(allNotes, pOverdue.Notes...)
	allNotes = append(allNotes, pBlocked.Notes...)

	var blockerNotes []Note
	for _, n := range allNotes {
		if n.Type == NNeedBlocked {
			blockerNotes = append(blockerNotes, n)
		}
	}
	require.Len(t, blockerNotes, 1, "exactly one NNeedBlocked on a-route-may-be-frontier; allNotes: %+v", allNotes)
	bn := blockerNotes[0]
	assert.Contains(t, bn.Primaries, "a-route-may-be-frontier")
	assert.Contains(t, bn.What, "muse-is-a-fleet-harness")
	assert.Contains(t, bn.What, "a-funded-provider-is-a-config-row-and-always-shown")
	assert.Contains(t, bn.What, "the-server-unit-carries-the-bus-address")
	assert.Contains(t, bn.What, "tools-v1217")

	// no NBlocked, NMissingNeed, NNeedsGone or NMergeLate for them
	depIDs := []string{"muse-is-a-fleet-harness", "a-funded-provider-is-a-config-row-and-always-shown", "the-server-unit-carries-the-bus-address"}
	for _, n := range allNotes {
		if n.Type == NBlocked || n.Type == NMissingNeed || n.Type == NNeedsGone || n.Type == NMergeLate {
			for _, dep := range depIDs {
				assert.NotContains(t, n.Primaries, dep, "no %s for dependent %s", n.Type, dep)
			}
		}
		if n.Type == NRaisedAgain {
			assert.NotContains(t, n.Primaries, "a-route-may-be-frontier", "no NRaisedAgain for NBound")
		}
	}
}

func TestADroppedNeedIsOneBlockerNotOnePerDependent(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dropped-card"}}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep-1"}, Needs: []string{"dropped-card"}}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep-2"}, Needs: []string{"dropped-card"}}))
	w.seedDroppedNeed("dropped-card")

	pResolve := Resolve(w.s, ResolveReq{})
	pBlocked, _ := TickNeedBlocked(w.s, TickReq{})
	notes := append(pResolve.Notes, pBlocked.Notes...)

	var blockedNotes, blockerNotes []Note
	for _, n := range notes {
		if n.Type == NBlocked {
			blockedNotes = append(blockedNotes, n)
		}
		if n.Type == NNeedBlocked {
			blockerNotes = append(blockerNotes, n)
		}
	}
	require.Len(t, blockerNotes, 1, "exactly one NNeedBlocked; notes: %+v", notes)
	require.Empty(t, blockedNotes, "zero NBlocked (not one per dependent); got: %+v", blockedNotes)
}

func TestTheBlockerJudgmentRetiresWhenTheHoldIsDecided(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"blocker"}}))
	w.note(judgment(NBound, "s1", w.s.Now, 0, "blocker"))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Needs: []string{"blocker"}}))

	p1, _ := TickNeedBlocked(w.s, TickReq{})
	require.NotEmpty(t, p1.Notes, "first tick raises NNeedBlocked")
	w.note(p1.Notes...)

	// A rework closes the NBound:
	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"blocker"}}, Tier: "pro"}))

	// The next tick closes NNeedBlocked:
	p2, _ := TickNeedBlocked(w.s, TickReq{})
	require.NotEmpty(t, p2.Closes, "next tick closes NNeedBlocked")
}

func TestDeferRefusesACardWithLiveDependents(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"card-a"}}))
	// card-a is waiting on the table:
	w.s.Work.Card("card-a").Col = Waiting
	w.s.Work.cells = nil
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"card-b"}, Needs: []string{"card-a"}}))

	p := Defer(w.s, DeferReq{Sel: Sel{IDs: []string{"card-a"}}, Reason: "defer to roadmap"})
	require.Len(t, p.Refused, 1, "defer refuses a card with live dependents: %+v", p)
	require.Contains(t, p.Refused[0].Why, "card-a is needed by card-b")
	require.Empty(t, p.Units, "table unchanged: nothing moved")
	require.NotNil(t, w.s.Work.Placed("card-a"), "card-a remains on the table")
}


func TestRelinkToAHeldCardIsRefused(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"held-card"}}))
	w.note(judgment(NBound, "s1", w.s.Now, 0, "held-card"))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"drop-card"}}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Needs: []string{"drop-card"}}))

	p := Drop(w.s, DropReq{Sel: Sel{IDs: []string{"drop-card"}}, Reason: "replaced", Relink: "held-card"})
	require.Len(t, p.Refused, 1, "relink to a held card is refused: %+v", p)
	require.Empty(t, p.Units, "nothing moved")
}

func TestDropWithLiveDependentsIsRefusedUnlessEachIsNamed(t *testing.T) {
	t.Parallel()
	// Guard test keeping BASE's refusal and its three flags green:
	// (a) drop without flags is refused:
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"card-a"}}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Needs: []string{"card-a"}}))
	p := Drop(w.s, DropReq{Sel: Sel{IDs: []string{"card-a"}}, Reason: "replaced"})
	require.Len(t, p.Refused, 1, "refused without flag")
	require.Empty(t, p.Units)

	// (b) drop with --cascade:
	pCascade := Drop(w.s, DropReq{Sel: Sel{IDs: []string{"card-a"}}, Reason: "replaced", Cascade: true})
	require.Empty(t, pCascade.Refused)
	require.NotEmpty(t, pCascade.Units)

	// (c) drop with --waive:
	pWaive := Drop(w.s, DropReq{Sel: Sel{IDs: []string{"card-a"}}, Reason: "replaced", Waive: true})
	require.Empty(t, pWaive.Refused)
	require.NotEmpty(t, pWaive.Units)

	// (d) drop with --relink to unheld card:
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"target"}}))
	pRelink := Drop(w.s, DropReq{Sel: Sel{IDs: []string{"card-a"}}, Reason: "replaced", Relink: "target"})
	require.Empty(t, pRelink.Refused)
	require.NotEmpty(t, pRelink.Units)
}
