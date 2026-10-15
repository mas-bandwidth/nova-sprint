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

// `card <id>` shows the tier a card's next deal draws from and its ceiling, on its CARD OK
// line, its ATTEMPT lines and in --json: a pro card is on pro from its first deal, and a
// flash tier recorded on it (as one dealt before line 1 became the starting tier) never
// deals it below pro (the owner, 2026-10-16: a card is never worked below its tier).
func TestCardPrintsTheTierAndItsCeiling(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.m.SetRoutes(costRoutes())
	ta.ok("add --stream s1 --count 1 --one --brief-file " + writeBrief(t, "s1: the work (s1) tier: pro"))
	ta.tierNow("s1-1", "flash")
	ta.deal(1)
	var v struct{ Tier, Ceiling string }
	require.NoError(t, json.Unmarshal([]byte(ta.ok("card s1-1 --json")), &v))
	out := ta.ok("card s1-1")
	assert.Contains(t, out, " tier=pro ceiling=pro\n", "the CARD OK line")
	assert.Contains(t, out, "route=pro-a model=opencode/deepseek-v4-pro tier=pro member=m1", "dealt on pro, never flash")
	assert.NotContains(t, out, "route=flash-a")
	assert.Equal(t, []string{"pro", "pro"}, []string{v.Tier, v.Ceiling}, "--json")
}

// tierNow puts the primary id on a tier, as the machine's escalation leaves it (flash
// first: every card's first deal is on flash): a test of the two-reader machinery
// (cost rule 4) on a store with routes means a card on pro.
func (ta *testApp) tierNow(id, tier string) {
	ta.t.Helper()
	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(ta.t, err)
	snap, err := st.Load(ctx, []string{sprint.Work}, nil)
	require.NoError(ta.t, err)
	c := snap.Work.Card(id)
	require.NotNil(ta.t, c, id)
	_, err = ta.m.Apply(ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Work), Epoch: fmt.Sprint(st.PinnedEpoch()),
		ExpectedTableRevision: fmt.Sprint(snap.Work.Revision), OperationID: "tier-now-" + id,
		Members: []ntable.BatchMemberEntry{{ID: c.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)}, Set: map[string]string{sprint.FieldTierNow: tier}}}})
	require.NoError(ta.t, err)
}
