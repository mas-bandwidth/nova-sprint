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
	h.ok(h.op("finish", nil, SetCardState{"u3", "done"}, SetCardReason{"c1", "why"}, SetCardReason{"u1", "because"}))
	all := h.snap()
	require.Equal(h.t, "do it", all.Cards["c1"].Brief)
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
		{Scope{Producers: true, Unworded: true}, []string{"c0", "c1", "c2"}},
		{Scope{Holder: "w1", Unworded: true}, []string{"c1", "u1", "u2"}},
	} {
		s, err := h.s.Read(h.ctx, c.scope)
		require.NoError(h.t, err)
		assert.Equal(h.t, c.want, slices.Sorted(maps.Keys(s.Cards)), "%+v", c.scope)
		for id, got := range s.Cards {
			want := all.Cards[id]
			if c.scope.Unworded {
				want.Brief, want.Reason, want.Prose, want.Finding = "", "", "", ""
			}
			assert.Equal(h.t, want, got, "%+v: %s", c.scope, id)
		}
		s.Cards = all.Cards
		assert.Equal(h.t, all, s, "%+v: the rest of the snapshot is whole", c.scope)
	}
}
