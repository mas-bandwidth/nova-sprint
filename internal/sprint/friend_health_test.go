package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMoveSeatTakesTheNextGeneration(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Now: time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC), Coordinator: "rowan", SeatGeneration: FirstSeatGeneration}
	p := MoveSeat(s, SeatReq{To: "stella", Who: "rowan", Reason: "handing over"})
	if assert.NotNil(t, p.Seat) {
		assert.Equal(t, uint64(2), p.Seat.Generation)
	}
	s.Coordinator, s.SeatGeneration = "stella", 2
	p = MoveSeat(s, SeatReq{To: "rowan", Who: "stella", Reason: "back"})
	if assert.NotNil(t, p.Seat) {
		assert.Equal(t, uint64(3), p.Seat.Generation, "A->B->A: A's second seat is a new generation")
	}
}
