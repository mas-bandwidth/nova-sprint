package store

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A reader's read (the readers table, read-cards off) returned with a provider fault is
// not a spent reader: ReadStep loads the fleet table and the routes, so steps_review.go's
// Read falls the read back at once onto the next route in line, the same reader and the
// same read, and stamps no returned and counts no re-ask (internal/sprint/fallback.go;
// tla/ProviderFallback.tla Fallback, FaultSpendsNothing; docs/SPEC-SPRINT.md section 5,
// provider fallback).
func TestProviderFaultReadReturnFallsBackOnTheReadersTable(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-a", "pro"), route("pro-b", "pro"), route("pro-c", "pro"))
	h.addReady("s1", 1, briefOf("pro", ""))
	h.startMachine()
	h.machine()
	h.work("m1")
	h.work("m2")
	h.machine()
	var rc *sprint.Card
	for _, c := range h.snap().Readers.Of("s1-1") {
		if c.Col == sprint.Asked || c.Col == sprint.Reading {
			rc = c
		}
	}
	require.NotNil(t, rc, "the primary's first read is asked")
	line := []string{"pro-a", "pro-b", "pro-c"}
	from := rc.F(sprint.FieldRoute)
	i := slices.Index(line, from)
	require.GreaterOrEqual(t, i, 0, "the read draws a route of the tier's line")
	want := line[(i+1)%len(line)]
	reader, id := rc.Row, rc.ID
	h.must(ReadStep(sprint.ReadReq{As: reader, Sel: sprint.Sel{IDs: []string{id}}, Return: true,
		Reason: "no verdict: provider: class=rate-limited status=429 msg=Too many requests"}))
	back := h.snap().Readers.Placed(id)
	require.NotNil(t, back, "the read stays placed")
	assert.Equal(t, reader, back.Row, "asked again of the same reader")
	assert.Equal(t, sprint.Asked, back.Col)
	assert.Equal(t, want, back.F(sprint.FieldRoute), "on the next route in line")
	assert.Zero(t, back.Int(sprint.FieldReasked), "no re-ask counted: the reader is not spent")
	assert.Empty(t, back.F(sprint.FieldReturned), "not a returned read")
	h.clean("provider fault read fell back")
}
