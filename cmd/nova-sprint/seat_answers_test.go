package main

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// openTypesOn is the types of the judgments open on the primary, from the inbox.
func (ta *testApp) openTypesOn(id string) []string {
	ta.t.Helper()
	var out []string
	for _, g := range ta.inboxGroups() {
		if g.Kind != sprint.Judgment {
			continue
		}
		for _, p := range g.Primaries {
			if p == id {
				out = append(out, g.Type)
			}
		}
	}
	return out
}

// returnedForARead (its stream set to seat-accept before the last read, when asked) is a pro card s1-1 accepted on two reads, one of them lost, returned by
// repair (rule 6) for its missing read, and that read asked and passed: both reads it
// needs stand at its head again.
func (ta *testApp) returnedForARead(seatAccept bool) {
	ta.t.Helper()
	ta.ok("init --readers reader-a,reader-b,reader-c --members m1")
	ta.ok("add --stream s1 --count 1 --one --brief-file " + proBriefFile(ta.t))
	ta.deal(1)
	ta.ok("take --as m1 --limit 100")
	var q struct{ Cards []queueCard }
	ta.json("queue --as m1", &q)
	require.NotEmpty(ta.t, q.Cards)
	ta.ok("finish --as m1 " + q.Cards[0].ID + "@" + strconv.Itoa(q.Cards[0].Gen))
	ta.ok("ask --limit 100")
	ta.ok("read --as reader-a --ok --limit 100")
	ta.ok("read --as reader-b --ok --limit 100")
	ta.ok("accept s1-1")
	// a third read needed: accepted short of its reads, through the verb, never an outside
	// write (start refuses while check fails)
	ta.ok("set --reads 3")
	ta.ok("repair")
	require.Equal(ta.t, sprint.Review, ta.primary("s1-1").Col, "repair returned it for its missing read")
	ta.ok("ask --limit 100")
	if seatAccept {
		ta.ok("stream set s1 --seat-accept on")
	}
	ta.ok("read --as reader-c --ok --limit 100")
}

// A card whose two reads have both passed is merging after one tick, with no seat verb,
// also when it was returned to review for a missing read (repair's rule 6): the return's one
// cause is answered by the read, so no "ready to accept" judgment holds it for the seat (the
// owner, 2026-10-12: four cards whose two reads had passed sat 45 minutes in "ready to
// accept").
func TestATwoReadsPassedCardIsMergingAfterOneTickWithNoSeatVerb(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.returnedForARead(false)
	ta.ok("start")
	ta.ok("tick")
	assert.Equal(t, sprint.Merging, ta.primary("s1-1").Col, "accepted by the tick")
	assert.NotContains(t, ta.openTypesOn("s1-1"), sprint.NReadyToAccept, "no ready to accept for the seat")
	assert.Empty(t, ta.openTypesOn("s1-1"), "nothing holds it for the seat")
}

// A stream set to seat-accept keeps the hand step: the same card is "ready to accept", in
// review, until the seat accepts it.
func TestASeatAcceptStreamKeepsItsCardForTheSeat(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.returnedForARead(true)
	ta.ok("start")
	ta.ok("tick")
	assert.Equal(t, sprint.Review, ta.primary("s1-1").Col, "a seat-accept stream's card waits for the seat")
	assert.Contains(t, ta.openTypesOn("s1-1"), sprint.NReadyToAccept)
	ta.ok("stream set s1 --seat-accept off")
	ta.ok("tick")
	assert.Equal(t, sprint.Merging, ta.primary("s1-1").Col, "off again, the tick accepts it")
	code, _, errs := ta.do("stream set s1 --seat-accept maybe")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errs, "--seat-accept wants on or off")
}

// A card returned to review only so its needs could be set goes back to merging at the next
// tick on the reads that stand: the needs set answers the return (the owner, 2026-10-12:
// "returned to review" held cards 45 minutes after their needs were repaired).
func TestACardReturnedForItsNeedsIsMergingAfterOneTick(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.proToMerging()
	ta.ok("return s1-1 --reason 'its needs name a dropped card'")
	require.Equal(t, sprint.Review, ta.primary("s1-1").Col)
	require.Contains(t, ta.openTypesOn("s1-1"), sprint.NReturned, "returned to review: the seat's, until its needs are set")
	ta.ok("needs set s1-1 --clear")
	ta.ok("start")
	ta.ok("tick")
	assert.Equal(t, sprint.Merging, ta.primary("s1-1").Col, "back to merging with no seat verb")
	assert.Empty(t, ta.openTypesOn("s1-1"), "the returned judgment closed with the accept")
}

// A return that is a mind's (no needs set after it) still holds the card for the seat.
func TestACardReturnedByTheSeatStaysForTheSeat(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.proToMerging()
	ta.ok("return s1-1 --reason 'look again'")
	ta.ok("start")
	ta.ok("tick")
	assert.Equal(t, sprint.Review, ta.primary("s1-1").Col, "a mind's return is the seat's")
	assert.Contains(t, ta.openTypesOn("s1-1"), sprint.NReturned)
}
