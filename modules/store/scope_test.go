package store

import (
	"maps"
	"slices"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scopedReads: Read takes the cards its scope names, each whole, and the
// rest of the snapshot whole; the Fake and PostgreSQL agree on every scope.
func scopedReads(h *harness) {
	h.seed()
	h.ok(h.op("finish", nil, SetCardState{"u3", "done"}))
	all := h.snap()
	for _, c := range []struct {
		scope Scope
		want  []string
	}{
		{Scope{}, nil},
		{Scope{All: true}, []string{"c0", "c1", "c2", "u1", "u2", "u3"}},
		{Scope{IDs: []string{"u1"}}, []string{"c1", "u1", "u2"}},
		{Scope{IDs: []string{"c2"}}, []string{"c2", "u3"}},
		{Scope{IDs: []string{"nope"}}, nil},
		{Scope{Holder: "w1"}, []string{"c1", "u1", "u2"}},
		{Scope{Holder: "w1", Final: true}, []string{"c1", "c2", "u1", "u2", "u3"}},
		{Scope{Holder: "nobody", Final: true}, nil},
		{Scope{Producers: true}, []string{"c0", "c1", "c2"}},
		{Scope{IDs: []string{"u3"}, Holder: "w1"}, []string{"c1", "c2", "u1", "u2", "u3"}},
	} {
		s, err := h.s.Read(h.ctx, c.scope)
		require.NoError(h.t, err)
		assert.Equal(h.t, c.want, slices.Sorted(maps.Keys(s.Cards)), "%+v", c.scope)
		for id, got := range s.Cards {
			assert.Equal(h.t, all.Cards[id], got, "%+v: %s", c.scope, id)
		}
		s.Cards = all.Cards
		assert.Equal(h.t, all, s, "%+v: the rest of the snapshot is whole", c.scope)
	}
}
