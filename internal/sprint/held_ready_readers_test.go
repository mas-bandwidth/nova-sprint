package sprint

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holdInPlace gives ready card id the server-default hold in place, as
// TickDecisionDefaults writes it for a ready primary whose work card was withdrawn.
func holdInPlace(w *world, id string) {
	c := w.s.Work.Card(id)
	c.Fields[FieldHeld] = stamp(w.s.Now)
	c.Fields["held_by"] = ServerDefaultActor
	c.Fields["held_reason"] = "j1: a card reached its bound; unanswered at twice its deadline"
	c.Fields["held_judgment"] = "j1"
	c.Fields["decision_held_from"] = Ready
}

// The readers of the ready column that took every ready card as dealable (the cold read
// of PR 471): a server-default hold in place keeps a card ready and dealt to no one, so
// none of them counts it as work the machine can move.
func TestAHeldReadyCardIsNoDealableWork(t *testing.T) {
	w := fleetWorld(t, 3, 1, "m1")
	holdInPlace(w, "s1-1")
	holdInPlace(w, "s1-2")

	// MoveDueKeys: no move is due for a card only release moves
	for _, k := range MoveDueKeys(w.s) {
		assert.False(t, strings.HasPrefix(k, "s1-1@") || strings.HasPrefix(k, "s1-2@"), "a held ready card is no due move: %s", k)
	}
	// the ready alarm counts dealable ready primaries
	assert.Equal(t, 1, dealableReady(w.s), "the two held ready cards are no ready work for the alarm")
	// the ready-over reason names the holder
	_, why := w.s.readyWhy(w.s.Work.Card("s1-1"))
	assert.Contains(t, why, "held by server-default for judgment j1")
	// an idle friend is not told a held card waits for her in the pool
	var pool []string
	for _, iw := range cardsWaitingFor(w.s, workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "pro", Tiers: []string{cardTierOf(w.s.Work.Card("s1-3"))}}), nil) {
		pool = append(pool, iw.phrase)
	}
	assert.Equal(t, []string{"s1-3 ready in the pool"}, pool, "the unheld card is named, the held ones not")
	w.must(Drop(w.s, DropReq{Sel: Sel{IDs: []string{"s1-3"}}, Reason: "out of the starving count"}))
	// starving: ready 0 of twice the width 2 while a wave is held, never ready 2
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"wave"}, Sentinel: true, Held: true}))
	w.part(TickDeal, TickReq{})
	notes := w.notesOf(NStarving)
	require.Len(t, notes, 1, "the fleet is starving: both ready cards are held")
	assert.Contains(t, notes[0].What, "ready 0 is under twice the width 2")
	assert.False(t, slices.ContainsFunc(w.s.Fleet.Column(Ready, Working), func(c *Card) bool { return c.F("primary") == "s1-1" || c.F("primary") == "s1-2" }), "and neither is dealt")
}
