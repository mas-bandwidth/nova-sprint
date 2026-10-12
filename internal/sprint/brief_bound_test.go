package sprint

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The attempt cap (docs/SPEC-SPRINT.md, the bounds; brief_bound.go; the owner, 2026-10-12): a
// card that has run its cap's attempts since the coordinator last decided on it is the
// coordinator's as one judgment, dealt to no machine and no friend, however much room a
// frontier or heavy friend has. The cap's default answer of 2026-10-04 (a friend's card past the
// cap, AttemptCapDeal) is retired: a friend tier is reached by the coordinator's re-tier, which
// is the decision that opens a fresh window.

// capWorld is a world with s1-1 a machine's card at the attempt cap: four attempts (the default
// cap) on the brief it was added with, no decision made, ready with its fourth work card
// withdrawn on a machine, every attempt's finding kept.
func capWorld(t *testing.T) *world {
	t.Helper()
	w := friendWorld(t, "c: a machine's card\nREPO: mas-bandwidth/nova-tools\n\nThe task.")
	pr := w.s.Work.Placed("s1-1")
	pr.Fields["attempt"] = "4"
	pr.Fields["findings"] = "attempt 1: one way\nattempt 2: another\nattempt 3: a third\nattempt 4: a fourth"
	w.s.Fleet.Put(&Card{ID: "s1-1.w4", Row: "m1", Col: Withdrawn, Score: pr.Score, Rev: 1, Fields: map[string]string{
		"kind": "work", "primary": "s1-1", "stream": "s1", "attempt": "4", "gen": "2", "member": "m1", "redeals": "3"}})
	_, atCap := AtAttemptCap(pr, w.s.AttemptsCap("s1"))
	require.True(t, atCap, "the card is at the attempt cap")
	return w
}

// capJudgmentsOn is the attempt-cap judgments open on the primary.
func capJudgmentsOn(s *Snapshot, id string) []Open {
	var out []Open
	for _, o := range s.Open {
		if o.Note.Type == NBriefWrong && (o.Subject() == id || slices.Contains(o.Note.Primaries, id)) {
			out = append(out, o)
		}
	}
	return out
}

// T4 of the card: a frontier friend up with room at the bound, and the card is not dealt to her
// (nor to a machine) until the coordinator re-tiers it; the cap's judgment is raised once and
// names every attempt's finding. Red before the fix: AttemptCapDeal dealt attempt 5 to her as
// the cap's default answer and reset the count as a replaced brief.
func TestACardAtTheCapIsDealtToNoOneUntilTheCoordinatorDecides(t *testing.T) {
	t.Parallel()
	w := capWorld(t)
	gus := FriendSeat{Name: "gus", Width: 8, Status: Up, Class: cardhdr.RouteFrontier}
	w.part(TickDeal, TickReq{Friends: []FriendSeat{gus}})
	assert.Equal(t, Ready, w.s.StateOf("s1-1"), "the card is left for the coordinator")
	assert.Nil(t, w.s.Fleet.Card("s1-1.w5"), "no attempt 5 is dealt to the friend")
	assert.Empty(t, w.s.Primary("s1-1").F(FieldWho), "its brief gains no WHO line")
	assert.Zero(t, w.s.Fleet.Count("m1", Ready)+w.s.Fleet.Count("m2", Ready), "no machine is dealt it either")
	open := capJudgmentsOn(w.s, "s1-1")
	require.Len(t, open, 1, "the cap's judgment, once")
	n := open[0].Note
	assert.Equal(t, Decisions[NBriefWrong], n.Decisions, "carry, brief, re-tier, drop")
	assert.Contains(t, n.What, "s1-1: brief defect after 4 attempts (max_attempts 4) without a coordinator decision")
	assert.Contains(t, n.What, "findings: attempt 1: one way; attempt 2: another; attempt 3: a third; attempt 4: a fourth", "every attempt's finding listed")
	assert.Contains(t, n.What, "nova-sprint rework s1-1 --fix <text> --tier <a higher tier>", "the re-tier is the way to a friend tier")

	// a second tick finds the judgment open and raises nothing; the card still waits
	w.part(TickDeal, TickReq{Friends: []FriendSeat{gus}})
	assert.Len(t, capJudgmentsOn(w.s, "s1-1"), 1, "raised once while it holds")
	assert.Nil(t, w.s.Fleet.Card("s1-1.w5"), "still no attempt 5")
	assert.Empty(t, Check(w.s, nil), "what is always true holds with the capped card waiting")

	// the coordinator re-tiers it: the decision, and the count starts again
	w.must(Brief(w.s, BriefReq{ID: "s1-1", Tier: cardhdr.RouteFrontier, Who: "coordinator", Reason: "four attempts on flash"}))
	pr := w.s.Primary("s1-1")
	assert.Equal(t, "4", pr.F(FieldDecidedAttempt), "decided at attempt 4")
	assert.Equal(t, 0, AttemptsSince(pr), "a fresh window")
	_, atCap := AtAttemptCap(pr, w.s.AttemptsCap("s1"))
	assert.False(t, atCap, "under the cap again")
	assert.Empty(t, capJudgmentsOn(w.s, "s1-1"), "the judgment is answered by the decision")
}

// A card at the cap whose withdrawn attempt is below its redeal bound is not dealt again to a
// machine either: a redeal is the machine's retry, and the owner's bound is on those. Red
// before the fix: the deal placed the withdrawn attempt 4 again.
func TestACardAtTheCapIsNotRedealtToAMachine(t *testing.T) {
	t.Parallel()
	w := capWorld(t)
	w.part(TickDeal, TickReq{})
	assert.Equal(t, Ready, w.s.StateOf("s1-1"))
	assert.Equal(t, Withdrawn, w.s.Fleet.Card("s1-1.w4").Col, "the withdrawn attempt stays withdrawn")
	assert.Zero(t, w.s.Fleet.Count("m1", Ready)+w.s.Fleet.Count("m2", Ready), "no machine is dealt it")
	require.Len(t, capJudgmentsOn(w.s, "s1-1"), 1, "the cap's judgment")
}

// A card at the attempt cap and at the environment-exit cap (Stella on #129) is held by one
// judgment at a time and dealt to no one: the attempt cap's judgment names it first, and no
// friend up with room is dealt attempt 5.
func TestACardAtBothCapsIsDealtToNoOne(t *testing.T) {
	t.Parallel()
	w := capWorld(t)
	wc := w.s.Fleet.Card("s1-1.w4")
	wc.Fields[FieldEnvExitHistory] = "m1=134,m2=137,m1=134"
	wc.Rev++
	w.s.Fleet.Put(wc)
	gus := FriendSeat{Name: "gus", Width: 8, Status: Up, Class: cardhdr.RouteFrontier}
	w.part(TickDeal, TickReq{Friends: []FriendSeat{gus}})
	assert.Equal(t, Ready, w.s.StateOf("s1-1"), "not dealt a next attempt")
	assert.Nil(t, w.s.Fleet.Card("s1-1.w5"), "no attempt 5")
	assert.NotNil(t, w.s.Fleet.Placed("s1-1.w4"), "the withdrawn history is not retired")
	assert.Empty(t, w.s.Primary("s1-1").F(FieldWho), "its brief gains no WHO line")
	require.Len(t, capJudgmentsOn(w.s, "s1-1"), 1, "one judgment holds it")
	w.part(TickDeal, TickReq{Friends: []FriendSeat{gus}})
	assert.Len(t, capJudgmentsOn(w.s, "s1-1"), 1, "raised once")
	assert.Nil(t, w.s.Fleet.Card("s1-1.w5"), "still no attempt 5")
}
