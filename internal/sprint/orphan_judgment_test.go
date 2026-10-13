package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A judgment naming no card of the epoch retires on the tick: an open judgment
// whose card the work table has no record of once loaded is closed by the tick
// that sees it absent, answered by the machine with the fact of the table.
func TestAJudgmentNamingNoCardOfTheEpochRetires(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)

	bound := judgment(NBound, "s1", w.s.Now, 0, "absent-1")
	w.note(bound)
	require.Len(t, w.s.Open, 1)

	var p Plan
	RetireJudgments(w.s, &p, "coordinator")
	require.NotEmpty(t, p.Closes, "RetireJudgments must close the orphan judgment")
	require.Len(t, p.Notes, 1, "RetireJudgments must write one decided note")
	assert.Equal(t, MachineActor, p.Notes[0].Who)
	assert.Contains(t, p.Notes[0].What, "absent-1")
	assert.Contains(t, p.Notes[0].What, "not on the table")
}

// At an epoch >= 1, an orphan judgment naming an absent card retires with
// the exact epoch number in its decided note.
func TestAJudgmentNamingNoCardOfTheEpochRetiresAtEpochOne(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.s.Epoch = 1

	bound := judgment(NBound, "s1", w.s.Now, 0, "absent-1")
	w.note(bound)
	require.Len(t, w.s.Open, 1)

	var p Plan
	RetireJudgments(w.s, &p, "coordinator")
	require.NotEmpty(t, p.Closes, "RetireJudgments must close the orphan judgment")
	require.Len(t, p.Notes, 1, "RetireJudgments must write one decided note")
	assert.Equal(t, MachineActor, p.Notes[0].Who)
	assert.Contains(t, p.Notes[0].What, "absent-1")
	assert.Contains(t, p.Notes[0].What, "not on the table of epoch 1")
}

// An orphan judgment is not counted by the seat-silent alarm: retired by the
// tick, it holds no card and causes no NSeatSilent or NRaisedAgain even hours
// past its deadline.
func TestAnOrphanIsNotCountedSeatSilent(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)

	bound := judgment(NBound, "s1", w.s.Now, 0, "absent-1")
	w.note(bound)
	require.Len(t, w.s.Open, 1)

	w.tick(9 * time.Hour)

	var p Plan
	RetireJudgments(w.s, &p, "coordinator")
	w.closeAll(p.Closes)

	pOverdue, _ := TickOverdue(w.s, TickReq{})
	for _, n := range pOverdue.Notes {
		assert.NotEqual(t, NSeatSilent, n.Type, "an orphan must not trigger seat silent: %s", n.What)
		assert.NotEqual(t, NRaisedAgain, n.Type, "an orphan must not be raised again: %s", n.What)
	}
}

// Check reports each orphan judgment (a judgment naming an absent or off-table
// card or dead stream that the tick failed to retire), and reports none for a
// friend-row subject, a tier: subject, or a stream-level judgment on a live stream.
func TestCheckNamesEachOrphan(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)

	// Orphan judgment: names absent card
	orphanCard := judgment(NBound, "s1", w.s.Now, 0, "absent-1")
	w.note(orphanCard)

	// Non-orphans:
	// 1. Friend-row subject
	friendNote := judgment(NFriendDeaf, FriendRow("amy"), w.s.Now, 0, FriendRow("amy"))
	w.note(friendNote)

	// 2. tier: subject
	tierNote := judgment(NNoRoute, TierSubject("frontier"), w.s.Now, 0)
	tierNote.StreamLevel = true
	w.note(tierNote)

	// 3. stream-level judgment on live stream "s1"
	streamNote := judgment(NStarving, "s1", w.s.Now, 0)
	streamNote.StreamLevel = true
	w.note(streamNote)

	violations := Check(w.s, nil)
	require.Len(t, violations, 1, "Check must report exactly one violation for the orphan judgment: %+v", violations)
	assert.Contains(t, violations[0].Detail, orphanCard.ID)
	assert.Contains(t, violations[0].Detail, "absent-1")
}
