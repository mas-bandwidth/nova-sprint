package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTierFromBriefLine1Heavy stores the heavy tier from line 1 when admitting a card.
func TestTierFromBriefLine1Heavy(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	// Admit a card with heavy tier on line 1
	plan := Add(w.s, AddReq{
		Stream: "s1",
		Cards: []CardAdd{{
			ID:    "c1",
			Brief: "c1: work (s1) tier: heavy\n\nthe task",
		}},
	})
	assert.Empty(t, plan.Refused)
	w.do(plan)
	c := w.s.Primary("c1")
	require.NotNil(t, c)
	assert.Equal(t, "heavy", c.F(FieldTier), "the tier from line 1 should be stored")
}

// TestTierFromBriefLine1Pro stores the pro tier from line 1 when admitting a card.
func TestTierFromBriefLine1Pro(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	plan := Add(w.s, AddReq{
		Stream: "s1",
		Cards: []CardAdd{{
			ID:    "c1",
			Brief: "c1: work (s1) tier: pro\n\nthe task",
		}},
	})
	assert.Empty(t, plan.Refused)
	w.do(plan)
	c := w.s.Primary("c1")
	require.NotNil(t, c)
	assert.Equal(t, "pro", c.F(FieldTier))
}

// TestTierFromBriefLine1Flash stores the flash tier from line 1 when admitting a card.
func TestTierFromBriefLine1Flash(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	plan := Add(w.s, AddReq{
		Stream: "s1",
		Cards: []CardAdd{{
			ID:    "c1",
			Brief: "c1: work (s1) tier: flash\n\nthe task",
		}},
	})
	assert.Empty(t, plan.Refused)
	w.do(plan)
	c := w.s.Primary("c1")
	require.NotNil(t, c)
	assert.Equal(t, "flash", c.F(FieldTier))
}

// TestTierFromBriefLine1Frontier stores the frontier tier from line 1 when admitting a card.
func TestTierFromBriefLine1Frontier(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	plan := Add(w.s, AddReq{
		Stream: "s1",
		Cards: []CardAdd{{
			ID:    "c1",
			Brief: "c1: work (s1) tier: frontier\n\nthe task",
		}},
	})
	assert.Empty(t, plan.Refused)
	w.do(plan)
	c := w.s.Primary("c1")
	require.NotNil(t, c)
	assert.Equal(t, "frontier", c.F(FieldTier))
}

// TestTierFromBriefLine1Missing stores empty tier when line 1 has no tier.
func TestTierFromBriefLine1Missing(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	plan := Add(w.s, AddReq{
		Stream: "s1",
		Cards: []CardAdd{{
			ID:    "c1",
			Brief: "c1: work (s1)\n\nthe task",
		}},
	})
	assert.Empty(t, plan.Refused)
	w.do(plan)
	c := w.s.Primary("c1")
	require.NotNil(t, c)
	assert.Equal(t, "", c.F(FieldTier), "no tier on line 1 means no FieldTier")
}
