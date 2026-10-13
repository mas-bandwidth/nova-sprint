package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestAFrontierCardIsReadAtHeavyOfTwoFleetReadersAndACoordinatorReadCounts is the
// test of a-frontier-card-can-be-read-and-accepted: a frontier card finished by
// a friend is read one tier down at heavy, asked of two fleet readers (no
// frontier reader is up), and the coordinator's own read (`read --as
// <coordinator> --ok <card>`) counts as one of the two reads the card needs.
//
// The store is private and named explicitly with --redis; the environment names
// no store (scrubbed env). It fails at the base: no read is asked at heavy and
// the coordinator has no read verb.
func TestAFrontierCardIsReadAtHeavyOfTwoFleetReadersAndACoordinatorReadCounts(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	// This fixture exercises an unpinned frontier card. The shared test fixture
	// declares only flash, pro and heavy routes, so give this test its own valid
	// funded frontier route rather than lowering or pinning the card's tier.
	routes, _, err := ta.m.Routes(context.Background())
	require.NoError(t, err)
	ta.m.SetRoutes(append(routes.Routes, sprint.Route{
		Name: "fixture-frontier", Tier: "frontier", Provider: "fixture", Model: "fixture", Tokens: 1000, Deadline: 600, Enabled: true,
	}))
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
	ta.ok("ask --redis mem:0 --limit 100")

	a, b := ta.askedOf("reader-a"), ta.askedOf("reader-b")
	require.NotEmpty(t, a, "reader-a is asked the frontier card's read one tier down at heavy")
	require.NotEmpty(t, b, "reader-b is asked the frontier card's read one tier down at heavy")
	var readView cardView
	ta.json("card --redis mem:0 s1-1", &readView)
	tierOf := map[string]string{}
	noteOf := map[string]string{}
	for _, rc := range readView.Reads {
		tierOf[rc.F("reader")] = rc.F(sprint.FieldTier)
		noteOf[rc.F("reader")] = rc.F(sprint.FieldReadFallbackNote)
	}
	require.Equal(t, "heavy", tierOf["reader-a"], "the frontier card is read one tier down at heavy, no frontier reader")
	require.Equal(t, "heavy", tierOf["reader-b"], "the frontier card is read one tier down at heavy, no frontier reader")
	require.Equal(t, "read at heavy: no frontier reader", noteOf["reader-a"], "the read card says why it is drawn one tier down")
	require.Equal(t, "read at heavy: no frontier reader", noteOf["reader-b"], "the read card says why it is drawn one tier down")

	// the coordinator's own read counts as one of the two
	ta.ok("read --redis mem:0 --as coordinator --ok s1-1 --finding 'read at heavy: no frontier reader'")
	ta.ok("read --redis mem:0 --as reader-a --ok --limit 1")
	ta.ok("accept --redis mem:0 --read-ok")
	var c cardView
	ta.json("card --redis mem:0 s1-1", &c)
	require.Equal(t, sprint.Merging, c.Column, "one reader's ok and the coordinator's read are the two the card needed")
}
