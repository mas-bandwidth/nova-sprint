package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestAReaderProviderFaultReturnReasksOnTheNextRoute pins the real-store read path
// behind the Ultramarine stall: ReadStep's readers-table branch loads the fleet and the
// routes, but prices with Step.Prices and so holds no tier array. A reader's
// provider-fault return therefore writes the provider's cooling and leaves the read
// asked, unspent and unrouted; the coordinator's ask (Routes loaded) re-asks it via
// readRouteOf on the next route in line, of the same reader, with no FieldReasked
// (tla/ProviderFallback.tla Fallback, FaultSpendsNothing; docs/SPEC-SPRINT.md section 5,
// provider fallback).
func TestAReaderProviderFaultReturnReasksOnTheNextRoute(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"), route("flash-b", "flash"))
	h.addReady("s1", 1, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	h.work("m1")
	h.machine()
	rcs := h.snap().Readers.Of("s1-1")
	require.Len(t, rcs, 1, "the flash card's one read")
	rc := rcs[0]
	first := rc.F(sprint.FieldRoute)
	require.Contains(t, []string{"flash-a", "flash-b"}, first, "a route of the tier")

	h.must(ReadStep(sprint.ReadReq{As: rc.Row, Begin: true, Sel: sprint.Sel{IDs: []string{rc.ID}}, Who: rc.Row}))
	h.must(ReadStep(sprint.ReadReq{As: rc.Row, Return: true,
		Reason: "no verdict: provider: class=rate-limited status=429 msg=Too many requests",
		Sel:    sprint.Sel{IDs: []string{rc.ID}}, Who: rc.Row}))

	s := h.snap()
	back := s.Readers.Card(rc.ID)
	require.NotNil(t, back, "the read card is still asked of its reader")
	assert.Equal(t, sprint.Asked, back.Col, "asked, not returned as a read and not spent")
	assert.Equal(t, first, back.F(sprint.FieldRoute), "the read step drew no route: the coordinator asks it")
	assert.NotEmpty(t, back.F(sprint.FieldReturned), "left for the ask to re-ask")
	assert.Empty(t, back.F(sprint.FieldReasked), "the reader was never spent")
	_, _, cooling := s.TierCooling("flash")
	assert.False(t, cooling, "the other route is in line: the tier is not all-cooling")

	// the ask (Routes loaded) draws the next route in line from readRouteOf: it asks it in
	// place, or retires the returned card and asks the read of another free reader
	h.machine()
	var asked *sprint.Card
	for _, c := range h.snap().Readers.Of("s1-1") {
		if c.Col == sprint.Asked {
			asked = c
		}
	}
	require.NotNil(t, asked, "the read is asked again")
	assert.NotEqual(t, first, asked.F(sprint.FieldRoute), "the next route in line")
	assert.Contains(t, []string{"flash-a", "flash-b"}, asked.F(sprint.FieldRoute))
	assert.Empty(t, asked.F(sprint.FieldReturned), "asked again, not returned")
	assert.Empty(t, asked.F(sprint.FieldReasked), "the reader was never spent")
}
