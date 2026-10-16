package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realBackend is the in-memory store said to be a real one (Backend.InMemory false), as
// a Redis store is: the unit tier opens no socket, so the real store's kind is the one
// thing this changes.
type realBackend struct{ *Mem }

func (realBackend) InMemory() bool { return false }

// The store says which snapshots are the twin's (the owner, 2026-10-16, on the store with
// no route at all: "Fence it off."): only the in-memory backend, through the read-only
// wrapper or any wrapper of it, is a twin; Redis never is.
// tla/ReaderTiers.tla NoDealOnRealStoreWithoutRoutes.
func TestOnlyTheInMemoryBackendIsATwin(t *testing.T) {
	t.Parallel()
	m := NewMem()
	for _, tc := range []struct {
		name string
		b    Backend
		twin bool
	}{
		{"mem", m, true},
		{"mem pinned", m.AtEpoch(1, false), true},
		{"mem read-only", ReadOnly(m), true},
		{"a wrapper of mem", struct{ Backend }{m}, true},
		{"redis", &Redis{}, false},
		{"redis read-only", ReadOnly(&Redis{}), false},
		{"redis pinned", (&Redis{}).AtEpoch(1, false), false},
		{"a real store", realBackend{m}, false},
		{"none", nil, false},
	} {
		assert.Equal(t, tc.twin, (&Store{B: tc.b}).MemTwin(), tc.name)
	}
}

// A real store with no route deals no card on any tier, the refusal naming the
// configuration; the twin with none deals as before; one route enabled on the real store
// deals its tier on that route.
func TestARealStoreWithNoRoutesDealsNoCardUntilARouteIsEnabled(t *testing.T) {
	t.Parallel()
	deal := func(st *Store) Result {
		t.Helper()
		res, err := st.Run(t.Context(), DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1", "s1-2"}}}))
		require.NoError(t, err)
		return res
	}

	twin := newHarness(t)
	twin.setup(2)
	require.True(t, twin.snap().MemTwin, "the harness's store is the twin")
	res := deal(twin.st)
	assert.Len(t, res.Moved, 2, "the twin with no route deals every card on the member's own model: %+v", res.Refused)

	h := newHarness(t)
	h.setup(2)
	st := *h.st
	st.B = realBackend{h.m}
	res = deal(&st)
	assert.Empty(t, res.Moved, "a real store with no route deals nothing")
	require.Len(t, res.Refused, 2)
	for _, r := range res.Refused {
		assert.Contains(t, r.Why, "no enabled route and no up friend serves tier pro", r.Key)
		assert.Contains(t, r.Why, "nova-config route add", "the refusal names the configuration to set")
	}

	h.m.SetRoutes([]sprint.Route{route("pro-a", "pro")})
	res = deal(&st)
	require.Len(t, res.Moved, 2, "a route of the tier enabled: the real store deals it: %+v", res.Refused)
	for _, id := range []string{"s1-1", "s1-2"} {
		wc := h.snap().Fleet.Card(sprint.WorkCardID(id, 1))
		require.NotNil(t, wc, id)
		assert.Equal(t, "pro-a", wc.F(sprint.FieldRoute), "dealt on the route enabled")
	}
	h.clean("dealt on its route")
}
