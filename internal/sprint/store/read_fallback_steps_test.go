package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestAReaderProviderFaultReturnReasksOnTheNextRoute pins the real-store read path
// behind the Ultramarine stall: ReadStep's readers-table branch loads the fleet and
// the routes, so a reader's provider-fault return is the fallback of fallback.go
// (tla/ProviderFallback.tla Fallback, FaultSpendsNothing; docs/SPEC-SPRINT.md
// section 5, provider fallback), never a read: the same read card is asked again at
// once on the next route in line, of the same reader, with no FieldReasked.
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
	assert.Equal(t, sprint.Asked, back.Col, "asked again at once: not returned, not spent")
	assert.NotEqual(t, first, back.F(sprint.FieldRoute), "the next route in line")
	assert.Contains(t, []string{"flash-a", "flash-b"}, back.F(sprint.FieldRoute))
	assert.Empty(t, back.F(sprint.FieldReasked), "the reader was never spent")
	assert.Empty(t, back.F(sprint.FieldReturned), "not a returned read")
	_, _, cooling := s.TierCooling("flash")
	assert.False(t, cooling, "the other route is in line: the tier is not all-cooling")
}
