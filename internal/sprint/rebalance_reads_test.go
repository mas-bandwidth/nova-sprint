package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The rebalance levels reads too (docs/SPEC-SPRINT.md section 1, the rebalance; the
// owner, 2026-10-11: "it seems like we could possibly rebalance this a bit better",
// "Not one card at a time per-tick", "This should NOT be manual."). A read card
// queued on a member whose lanes all work goes to a member up with an idle lane whose
// reader row serves the read's tier and which has not read the card, as many as the
// idle lanes take (a read is half a slot), in one tick.

// readQueueWorld is m1 with its one lane full (a working work card) and n read cards
// queued ready on it, their primaries in review; m2 is up with its reader row and an
// idle lane.
func readQueueWorld(t *testing.T, n int) *world {
	t.Helper()
	w := rbWorld(t, "m1", "m2")
	w.s.Readers.SetRows(append(w.s.Readers.Rows(), ReaderPrefix+"m2"))
	rbPlace(w, "s1-1", cardhdr.RouteFlash, "m1", Working)
	for i := 2; i <= n+1; i++ {
		id := fmt.Sprintf("s1-%d", i)
		w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Review, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief(cardhdr.RouteFlash), FieldTierNow: cardhdr.RouteFlash}})
		w.s.Fleet.Put(&Card{ID: ReadCardID(id, 1, "m1"), Row: "m1", Col: Ready, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "read", "primary": id, "stream": "s1", "reader": "m1", "attempt": "1", FieldReadCard: "1", FieldTier: cardhdr.RouteFlash}})
	}
	return w
}

func readsReady(s *Snapshot, row string) int {
	n := 0
	for _, c := range s.Fleet.Cell(row, Ready) {
		if isRead(c) {
			n++
		}
	}
	return n
}

// A member with every lane working and reads queued beside a member up with an idle
// lane whose reader row serves the read's tier sees the reads move on the next tick, as
// many as the idle lanes take (reads at half a slot), in one tick.
func TestTheRebalanceMovesQueuedReadsToAnIdleMemberInOneTick(t *testing.T) {
	t.Parallel()
	w := readQueueWorld(t, 6)
	require.Equal(t, 6, readsReady(w.s, "m1"), "six reads queue on the full member")
	before := ReadCardID("s1-2", 1, "m1")
	p := w.must(Rebalance(w.s, nil, "machine"))
	assert.NotEmpty(t, rebalanced(p), "the rebalance moved no read off the full member")
	// m2's one idle lane takes the reads at half a slot: two of them.
	assert.Equal(t, 2, readsReady(w.s, "m2"), "the idle member's lane takes the reads at half a slot")
	assert.Equal(t, 4, readsReady(w.s, "m1"))
	// a moved read keeps its attempt, and is a fresh card on the new reader.
	moved := w.s.Fleet.Card(ReadCardID("s1-2", 1, "m2"))
	require.NotNil(t, moved, "the read moved to m2 under its identity there")
	assert.True(t, moved.Placed())
	assert.Equal(t, "1", moved.F("attempt"), "a moved read keeps its attempt number")
	assert.NotNil(t, w.s.Fleet.Card(before))
	assert.False(t, w.s.Fleet.Card(before).Placed(), "the read on the row it left is retired")
	assert.Equal(t, "m1", moved.F(FieldRebalancedFrom))
}

// Work moved in the work pass consumes the same destination lane budget as the read pass.
func TestTheRebalanceDoesNotOverfillAReaderAfterMovingWork(t *testing.T) {
	t.Parallel()
	w := readQueueWorld(t, 2)
	rbPlace(w, "s1-work", cardhdr.RouteFlash, "m1", Ready)

	p := w.must(Rebalance(w.s, nil, "machine"))
	assert.NotEmpty(t, rebalanced(p), "the ready work card should move to m2 first")
	assert.Equal(t, "m2", w.s.Fleet.Card(WorkCardID("s1-work", 1)).Row)
	assert.Equal(t, 0, readsReady(w.s, "m2"), "m2's lane was consumed by moved work")
	assert.Equal(t, 2, readsReady(w.s, "m1"), "reads stay queued when no idle lane remains")
}

func TestTheRebalanceUsesTheRemainingHalfSlotAfterAQueuedRead(t *testing.T) {
	t.Parallel()
	w := readQueueWorld(t, 2)
	prior := w.s.Fleet.Card(ReadCardID("s1-2", 1, "m1"))
	prior.Col = DoneOK
	w.s.Fleet.Put(prior)
	w.s.Fleet.Put(&Card{ID: ReadCardID("s1-2", 1, "m2"), Row: "m2", Col: Ready, Score: 2, Rev: 1,
		Fields: map[string]string{"kind": "read", "primary": "s1-2", "stream": "s1", "reader": "m2", "attempt": "1", FieldReadCard: "1", FieldTier: cardhdr.RouteFlash}})
	p := w.must(Rebalance(w.s, nil, "machine"))
	assert.Len(t, rebalanced(p), 1, "one read half-slot remains beside the queued read")
	assert.Equal(t, 2, readsReady(w.s, "m2"), "the remaining half-slot takes exactly one read")
	assert.Equal(t, 0, readsReady(w.s, "m1"))
}

func TestTheRebalanceKeepsAReadQueueWhenItsDonorHasHalfALaneFree(t *testing.T) {
	t.Parallel()
	w := rbWorld(t, "m1", "m2")
	w.s.Readers.SetRows(append(w.s.Readers.Rows(), ReaderPrefix+"m2"))
	for i, col := range []string{Working, Ready} {
		id := fmt.Sprintf("s1-%d", i+1)
		w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Review, Score: float64(i + 1), Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief(cardhdr.RouteFlash), FieldTierNow: cardhdr.RouteFlash}})
		w.s.Fleet.Put(&Card{ID: ReadCardID(id, 1, "m1"), Row: "m1", Col: col, Score: float64(i + 1), Rev: 1,
			Fields: map[string]string{"kind": "read", "primary": id, "stream": "s1", "reader": "m1", "attempt": "1", FieldReadCard: "1", FieldTier: cardhdr.RouteFlash}})
	}

	p := w.must(Rebalance(w.s, nil, "machine"))
	assert.Empty(t, rebalanced(p), "one working read leaves half a lane, so the queue can remain on m1")
	assert.Equal(t, 1, readsReady(w.s, "m1"))
	assert.Equal(t, 0, readsReady(w.s, "m2"))
}

// A read never moves to a member whose reader row does not serve its tier, nor back to a
// row it was rebalanced off.
func TestTheRebalanceReadNeverMovesToAnIneligibleOrOldRow(t *testing.T) {
	t.Parallel()
	w := readQueueWorld(t, 2)
	// m2's reader row exists but serves no tier here: take the flash route away.
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "pr", Enabled: true}}
	assert.Empty(t, rebalanced(w.must(Rebalance(w.s, nil, "machine"))),
		"a member whose reader row does not serve the read's tier takes no read")
}

// A read never moves to a member that already read the card at this attempt (the deal's
// eligibility, mayReadCard's readSpent): the donor's read stays queued, the other reads
// still move.
func TestTheRebalanceReadNeverMovesToAReaderThatAlreadyReadIt(t *testing.T) {
	t.Parallel()
	w := readQueueWorld(t, 2)
	// m2 already holds a read of s1-2 at this attempt
	w.s.Fleet.Put(&Card{ID: ReadCardID("s1-2", 1, "m2"), Row: "m2", Col: Ready, Score: 2, Rev: 1,
		Fields: map[string]string{"kind": "read", "primary": "s1-2", "stream": "s1", "reader": "m2", "attempt": "1", FieldReadCard: "1", FieldTier: cardhdr.RouteFlash}})
	p := w.must(Rebalance(w.s, nil, "machine"))
	require.Len(t, rebalanced(p), 1, "only s1-3 moves: m2 already read s1-2 at this attempt")
	require.NotNil(t, w.s.Fleet.Card(ReadCardID("s1-3", 1, "m2")), "s1-3 still moves to m2")
	require.True(t, w.s.Fleet.Card(ReadCardID("s1-2", 1, "m1")).Placed(),
		"s1-2's read stays queued on m1, never to the member that already read it")
}

// A read never moves back to a row it was rebalanced off (FieldRebalancedFrom): it takes
// the other idle reader instead.
func TestTheRebalanceReadNeverMovesBackToARowItWasRebalancedOff(t *testing.T) {
	t.Parallel()
	w := readQueueWorld(t, 1)
	w.setWidth("m3", 1)
	w.up("m3")
	w.s.Readers.SetRows(append(w.s.Readers.Rows(), ReaderPrefix+"m3"))
	rc := w.s.Fleet.Card(ReadCardID("s1-2", 1, "m1"))
	rc.Fields[FieldRebalancedFrom] = "m2"
	w.s.Fleet.Put(rc)
	p := w.must(Rebalance(w.s, nil, "machine"))
	require.Len(t, rebalanced(p), 1)
	require.NotNil(t, w.s.Fleet.Card(ReadCardID("s1-2", 1, "m3")), "it moves to m3, never back to m2")
	require.Nil(t, w.s.Fleet.Card(ReadCardID("s1-2", 1, "m2")))
}
