package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestAFrontierCardIsNeverReadAtHeavyByFleetReaders: a frontier card finished in review is
// read at frontier, never one tier down (the owner, 2026-10-16: a card is never read below
// its tier): two fleet readers that read heavy alone are asked nothing, and the card waits
// in review for a frontier reader.
//
// The store is private and named explicitly with --redis; the environment names
// no store (scrubbed env). It fails at the base, where the read was asked at heavy.
func TestAFrontierCardIsNeverReadAtHeavyByFleetReaders(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	// two fleet readers that read heavy and no other tier; the store named on the line
	ta.ok("init --redis mem:0 --readers reader-a,reader-b --members m1")
	ta.ok("reader set --redis mem:0 reader-a --tiers heavy")
	ta.ok("reader set --redis mem:0 reader-b --tiers heavy")
	// a frontier card, dealt and finished, in review
	brief := writeBrief(t, "tier: frontier\nA frontier card")
	ta.ok("add --redis mem:0 --stream s1 --count 1 --one --brief-file " + brief)
	ta.deal(1)
	ta.ok("take --redis mem:0 --as m1 --limit 1")
	ta.ok("finish --redis mem:0 --as m1 s1-1.w1@1 --report 'the work is done'")
	ta.raw("ask --redis mem:0 --limit 100") // no reader may read it: the ask refuses, and asks nothing

	require.Empty(t, ta.askedOf("reader-a"), "reader-a reads heavy only: never asked a frontier read")
	require.Empty(t, ta.askedOf("reader-b"), "reader-b reads heavy only: never asked a frontier read")
	var c cardView
	ta.json("card --redis mem:0 s1-1", &c)
	for _, rc := range c.Reads {
		require.NotEqual(t, "heavy", rc.F(sprint.FieldTier), "no read of the frontier card at heavy")
	}
	require.Equal(t, sprint.Review, c.Column, "the card waits in review for a frontier reader")
}
