package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The seat's generation and a friend's health through the command (docs/SPEC-SPRINT.md
// section 1, "A friend's health"): `seat` is the small read the daemons call every
// second; `friend health` is the coordinator's observation, fenced by the seat; the
// table shows up, held or down and nothing else, with the reason when there is one.

// seatView is what seat --json carries.
type seatJSON struct {
	Holder     string `json:"holder"`
	Epoch      uint64 `json:"epoch"`
	Generation uint64 `json:"generation"`
}

func (ta *testApp) seatState() seatJSON {
	ta.t.Helper()
	var s seatJSON
	ta.json("seat", &s)
	return s
}

// The seat read: holder, epoch and generation, from the first init with no seat
// record; a handover takes the next generation; a clear keeps it.
func TestSeatReadsHolderEpochAndGeneration(t *testing.T) {
	t.Parallel()
	ta := seatSprint(t)
	assert.Equal(t, "SEAT holder=coordinator epoch=0 generation=1\n", ta.ok("seat"))
	assert.Equal(t, seatJSON{Holder: "coordinator", Epoch: 0, Generation: 1}, ta.seatState())
	ta.ok("coordinator rowan --reason 'handing over'")
	assert.Equal(t, seatJSON{Holder: "rowan", Epoch: 0, Generation: 2}, ta.seatState())
	var h handoverView
	ta.json("handover", &h)
	assert.Equal(t, uint64(2), h.Seat.Generation, "handover carries it too")
	ta.ok("clear --confirm sprint --actor rowan")
	assert.Equal(t, seatJSON{Holder: "rowan", Epoch: 1, Generation: 2}, ta.seatState(), "a clear moves the epoch, never the seat's generation")
}

// friend health --clear removes the coordinator's observation of a friend, so her status is
// her session's evidence alone: with the observation gone she is up only on a card of hers
// finished, and her beat never brings her up. --dry-run says what stood and writes nothing;
// the seat's holder alone clears; an observation's flag beside --clear is refused.
