package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAQuietedJudgmentIsNeverCountedOverdue verifies that a judgment quieted by
// an unfunded tier (an NBound on a pro card while every pro route rests, plus
// the tier's NNoRoute whose cause is rests) is never counted overdue:
// TickOverdue writes no NOverdue line after the first, and behindConds returns
// no NCoordinatorBehind.
// Red at BASE 65973f2bcf12.
func TestAQuietedJudgmentIsNeverCountedOverdue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	t0 := now.Add(-3 * time.Hour)

	workTable := NewTable(Work)
	workTable.SetRows([]string{"s1"})
	workTable.Put(&Card{
		ID:     "c1",
		Row:    "s1",
		Col:    Ready,
		Score:  100,
		Fields: map[string]string{PrimaryField: "c1", FieldTierNow: "pro"},
	})

	fleetTable := NewTable(Fleet)
	fleetTable.SetRows([]string{"m1"})

	boundJ := Note{
		ID:        "j-bound-pro",
		Kind:      Judgment,
		Type:      NBound,
		Stream:    "s1",
		Primaries: []string{"c1"},
		Count:     1,
		At:        t0,
		Decisions: []string{"rework with a fix", "drop"},
	}

	noRouteJ := Note{
		ID:          "j-noroute-pro",
		Kind:        Judgment,
		Type:        NNoRoute,
		Stream:      TierSubject("pro"),
		StreamLevel: true,
		Primaries:   []string{"c1"},
		Count:       1,
		At:          t0,
		What:        "tier pro cannot be dealt: every route rests (r1: credit; r2: auth)",
		Decisions:   []string{"route add", "look at the card", "drop"},
	}

	// First overdue reminder was written at t0 + DeadlineJudgment (2h ago):
	markAt := t0.Add(DeadlineJudgment)
	boundHold := Note{
		Kind:      Acknowledged,
		Type:      NOverdue,
		Stream:    "s1",
		What:      boundJ.ID,
		At:        markAt,
		Primaries: []string{"c1"},
		Count:     1,
	}
	noRouteHold := Note{
		Kind:        Acknowledged,
		Type:        NOverdue,
		Stream:      TierSubject("pro"),
		StreamLevel: true,
		What:        noRouteJ.ID,
		At:          markAt,
		Primaries:   []string{"c1"},
		Count:       1,
	}

	routes := []Route{
		{Name: "r1", Tier: "pro", Provider: "prov1", Enabled: true},
		{Name: "r2", Tier: "pro", Provider: "prov2", Enabled: true},
	}
	rests := map[string]RouteRest{
		"r1": {Route: "r1", Cause: RestCredit, Cards: []string{"c1"}, At: t0, Until: now.Add(time.Hour)},
		"r2": {Route: "r2", Cause: RestAuth, At: t0, Until: now.Add(time.Hour)},
	}

	s := &Snapshot{
		Now:         now,
		Coordinator: "coordinator",
		Work:        workTable,
		Fleet:       fleetTable,
		Routes:      routes,
		rests:       rests,
		Open: []Open{
			{Key: OpenKey(boundJ.ID, "c1"), Note: boundJ},
			{Key: OpenKey(noRouteJ.ID, StreamSubject(TierSubject("pro"))), Note: noRouteJ},
		},
		Acked: []Open{
			{Key: OpenKey(boundJ.ID, "c1"), Note: boundHold},
			{Key: OpenKey(noRouteJ.ID, StreamSubject(TierSubject("pro"))), Note: noRouteHold},
		},
	}
	r := TickReq{Who: MachineActor}

	// 1. behindConds returns no NCoordinatorBehind while quieted.
	conds := behindConds(s, r)
	assert.Empty(t, conds, "behindConds must return no NCoordinatorBehind for quieted judgments")

	// 2. TickOverdue writes no NOverdue line after the first.
	p, _ := TickOverdue(s, r)
	for _, n := range p.Notes {
		assert.NotEqual(t, NOverdue, n.Type, "TickOverdue wrote NOverdue line for quieted judgment")
		assert.NotEqual(t, NCoordinatorBehind, n.Type, "TickOverdue raised NCoordinatorBehind for quieted judgment")
	}

	// 3. The hold-close loop closes the overdue holds of quieted judgments.
	closedWhat := map[string]bool{}
	for _, o := range p.Closes {
		if o.Note.Type == NOverdue {
			closedWhat[o.Note.What] = true
		}
	}
	assert.True(t, closedWhat[boundJ.ID], "overdue hold for NBound was not closed")
	assert.True(t, closedWhat[noRouteJ.ID], "overdue hold for NNoRoute was not closed")
}

// TestAWaitedJudgmentIsNotCountedOverdue pins rule (a): a waited judgment with
// an unexpired review time is never counted overdue.
// Green at BASE 65973f2bcf12.
func TestAWaitedJudgmentIsNotCountedOverdue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	review := now.Add(30 * time.Minute)

	// A non-kept judgment (NRed) in s.Open with Review set:
	nonKeptJ := Note{
		ID:        "j-nonkept",
		Kind:      Judgment,
		Type:      NRed,
		Stream:    "s1",
		Primaries: []string{"c1"},
		Count:     1,
		At:        now.Add(-time.Hour),
		Review:    review,
		ReviewSet: now,
	}

	// A tick-kept judgment (NNoRoute) waited: closed into s.Acked with Review set:
	tickKeptHold := Note{
		ID:          "j-kept-hold",
		Kind:        Acknowledged,
		Type:        NNoRoute,
		Stream:      TierSubject("flash"),
		StreamLevel: true,
		Primaries:   []string{"c2"},
		Count:       1,
		At:          now.Add(-time.Hour),
		Review:      review,
		ReviewSet:   now,
	}

	s20 := &Snapshot{
		Now:         now.Add(20 * time.Minute),
		Coordinator: "coordinator",
		Open: []Open{
			{Key: OpenKey(nonKeptJ.ID, "c1"), Note: nonKeptJ},
		},
		Acked: []Open{
			{Key: OpenKey(tickKeptHold.ID, StreamSubject(TierSubject("flash"))), Note: tickKeptHold},
		},
	}
	r := TickReq{Who: MachineActor}

	// At s.Now + 20m (before review time): neither is counted by tickOverdue or behindConds.
	p20, _ := tickOverdue(s20, r)
	for _, n := range p20.Notes {
		assert.NotEqual(t, NOverdue, n.Type, "overdue line written before review time")
	}
	assert.Empty(t, behindConds(s20, r), "behindConds counted unexpired waited judgment")

	// At s.Now + 45m (15m after review time expired, exceeding PassEvery): both are counted.
	// The tick-kept judgment is re-raised into s.Open on review expiry:
	tickKeptRaised := Note{
		ID:          "j-kept-raised",
		Kind:        Judgment,
		Type:        NNoRoute,
		Stream:      TierSubject("flash"),
		StreamLevel: true,
		Primaries:   []string{"c2"},
		Count:       1,
		At:          now.Add(-time.Hour),
	}

	s45 := &Snapshot{
		Now:         now.Add(45 * time.Minute),
		Coordinator: "coordinator",
		Open: []Open{
			{Key: OpenKey(nonKeptJ.ID, "c1"), Note: nonKeptJ},
			{Key: OpenKey(tickKeptRaised.ID, StreamSubject(TierSubject("flash"))), Note: tickKeptRaised},
		},
		Acked: []Open{
			// NOverdue mark written for behindConds:
			{Key: OpenKey("ov1", "c1"), Note: Note{Kind: Acknowledged, Type: NOverdue, What: nonKeptJ.ID, At: now.Add(30 * time.Minute)}},
			{Key: OpenKey("ov2", StreamSubject(TierSubject("flash"))), Note: Note{Kind: Acknowledged, Type: NOverdue, What: tickKeptRaised.ID, At: now.Add(30 * time.Minute)}},
		},
	}

	assert.True(t, JudgmentOverdue(s45, r, nonKeptJ), "non-kept judgment overdue after review")
	assert.True(t, JudgmentOverdue(s45, r, tickKeptRaised), "tick-kept judgment overdue after review")

	conds45 := behindConds(s45, r)
	require.NotEmpty(t, conds45, "behindConds should count judgments past review time")
	assert.Equal(t, NCoordinatorBehind, conds45[0].typ)
}

// TestAQuietEndsAndTheJudgmentCountsAgain verifies that when a route comes back
// (rest ends), the judgment is counted again from its own stamps.
func TestAQuietEndsAndTheJudgmentCountsAgain(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	t0 := now.Add(-3 * time.Hour)

	workTable := NewTable(Work)
	workTable.SetRows([]string{"s1"})
	workTable.Put(&Card{
		ID:     "c1",
		Row:    "s1",
		Col:    Ready,
		Score:  100,
		Fields: map[string]string{PrimaryField: "c1", FieldTierNow: "pro"},
	})

	boundJ := Note{
		ID:        "j-bound-pro",
		Kind:      Judgment,
		Type:      NBound,
		Stream:    "s1",
		Primaries: []string{"c1"},
		Count:     1,
		At:        t0,
		Decisions: []string{"rework with a fix", "drop"},
	}

	markAt := t0.Add(DeadlineJudgment)
	boundHold := Note{
		Kind:      Acknowledged,
		Type:      NOverdue,
		Stream:    "s1",
		What:      boundJ.ID,
		At:        markAt,
		Primaries: []string{"c1"},
		Count:     1,
	}

	routes := []Route{
		{Name: "r1", Tier: "pro", Provider: "prov1", Enabled: true},
		{Name: "r2", Tier: "pro", Provider: "prov2", Enabled: true},
	}
	// Route r1's rest has ended (Until is in the past):
	rests := map[string]RouteRest{
		"r1": {Route: "r1", Cause: RestCredit, Cards: []string{"c1"}, At: t0, Until: now.Add(-10 * time.Minute)},
		"r2": {Route: "r2", Cause: RestAuth, At: t0, Until: now.Add(time.Hour)},
	}

	s := &Snapshot{
		Now:         now,
		Coordinator: "coordinator",
		Work:        workTable,
		Routes:      routes,
		rests:       rests,
		Open: []Open{
			{Key: OpenKey(boundJ.ID, "c1"), Note: boundJ},
		},
		Acked: []Open{
			{Key: OpenKey(boundJ.ID, "c1"), Note: boundHold},
		},
	}
	r := TickReq{Who: MachineActor}

	// With r1 live again, tier pro is served, so NBound is counted again:
	conds := behindConds(s, r)
	require.NotEmpty(t, conds, "behindConds must count NBound once pro route is live")
	assert.Equal(t, NCoordinatorBehind, conds[0].typ)
}
