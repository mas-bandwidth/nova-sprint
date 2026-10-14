package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// A card the sprint gave a fallback tier shows its own brief tier and where it falls
// (docs/SPEC-SPRINT.md section 5, "An unserved tier falls to the strongest served tier"):
// `card` and `where --json --rows` print `tier=<brief> (falls to <t>)`.
func TestCardAndWhereShowAFallbackTier(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetRoutes(costRoutes())
	ta.ok("add --stream s1 --count 1 --one --brief-file " + writeBrief(t, "s1: the work (s1) tier: frontier\n\nThe task.\n"))
	// give the card a fallback tier, as the tick's part does (unserved_tier.go)
	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	snap, err := st.Load(ctx, []string{sprint.Work}, nil)
	require.NoError(t, err)
	c := snap.Work.Card("s1-1")
	require.NotNil(t, c, "s1-1")
	_, err = ta.m.Apply(ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Work), Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(snap.Work.Revision), OperationID: "fallback-tier-s1-1",
		Members: []ntable.BatchMemberEntry{{ID: c.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)}, Set: map[string]string{sprint.FieldFallbackTier: "pro"}}}})
	require.NoError(t, err)

	out := ta.ok("card s1-1")
	assert.Contains(t, out, " tier=frontier (falls to pro) ceiling=frontier\n", "the CARD OK line")

	var v struct{ Tier, Ceiling string }
	require.NoError(t, json.Unmarshal([]byte(ta.ok("card s1-1 --json")), &v))
	assert.Equal(t, "frontier (falls to pro)", v.Tier, "--json tier")
	assert.Equal(t, "frontier", v.Ceiling, "--json ceiling")

	rows := ta.ok("where --json --rows")
	var r struct {
		Rows []primaryRow `json:"rows"`
	}
	require.NoError(t, json.Unmarshal([]byte(rows), &r))
	found := false
	for _, row := range r.Rows {
		if row.ID == "s1-1" {
			found = true
			assert.Equal(t, "frontier (falls to pro)", row.TierShown, "where --rows tier_shown")
		}
	}
	assert.True(t, found, "s1-1 in where --rows")
}
