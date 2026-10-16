package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eQuietNow is the clock of the quieted-judgment tests; their judgments were raised at
// eQuietNow less three hours, well past DeadlineJudgment (1h).
var eQuietNow = time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)

// eQuietWork is a work table holding the ready primary c1 at the tier, with the fields given.
func eQuietWork(tier string, fields map[string]string) *Table {
	f := map[string]string{PrimaryField: "c1", FieldTierNow: tier}
	for k, v := range fields {
		f[k] = v
	}
	wt := NewTable(Work)
	wt.SetRows([]string{"s1"})
	wt.Put(&Card{ID: "c1", Row: "s1", Col: Ready, Score: 100, Fields: f})
	return wt
}

// eQuietFleet is a fleet table of the one row m1.
func eQuietFleet() *Table {
	ft := NewTable(Fleet)
	ft.SetRows([]string{"m1"})
	return ft
}

// eBoundJ is the open NBound judgment j-bound-pro on c1, raised at.
func eBoundJ(at time.Time) Note {
	return Note{ID: "j-bound-pro", Kind: Judgment, Type: NBound, Stream: "s1", Primaries: []string{"c1"}, Count: 1, At: at,
		Decisions: []string{"rework with a fix", "drop"}}
}

// eNoRouteJ is the open NNoRoute judgment j-noroute-<tier> on c1, raised at, saying why.
func eNoRouteJ(tier string, at time.Time, why string) Note {
	return Note{ID: "j-noroute-" + tier, Kind: Judgment, Type: NNoRoute, Stream: TierSubject(tier), StreamLevel: true,
		Primaries: []string{"c1"}, Count: 1, At: at, What: "tier " + tier + " cannot be dealt: every route rests (" + why + ")",
		Decisions: []string{"route add", "look at the card", "drop"}}
}

// eOverdueMark is the first overdue reminder of the judgment j, written at.
func eOverdueMark(j Note, at time.Time) Note {
	return Note{Kind: Acknowledged, Type: NOverdue, Stream: j.Stream, StreamLevel: j.StreamLevel, What: j.ID, At: at,
		Primaries: []string{"c1"}, Count: 1}
}

// eTierKey is the open key of a tier-level judgment j.
func eTierKey(j Note, tier string) string { return OpenKey(j.ID, StreamSubject(TierSubject(tier))) }

// TestAQuietedJudgmentIsNeverCountedOverdue verifies that a judgment quieted by
// an unfunded tier (an NBound on a pro card while every pro route rests, plus
// the tier's NNoRoute whose cause is rests) is never counted overdue:
// TickOverdue writes no NOverdue line after the first, and behindConds returns
// no NCoordinatorBehind.
// Red at BASE 65973f2bcf12.
func TestAQuietedJudgmentIsNeverCountedOverdue(t *testing.T) {
	t.Parallel()
	now := eQuietNow
	t0 := now.Add(-3 * time.Hour)
	boundJ, noRouteJ := eBoundJ(t0), eNoRouteJ("pro", t0, "r1: credit; r2: auth")
	// First overdue reminder was written at t0 + DeadlineJudgment (2h ago):
	markAt := t0.Add(DeadlineJudgment)
	s := &Snapshot{
		Now:         now,
		Coordinator: "coordinator",
		Work:        eQuietWork("pro", nil),
		Fleet:       eQuietFleet(),
		Routes: []Route{
			{Name: "r1", Tier: "pro", Provider: "prov1", Enabled: true},
			{Name: "r2", Tier: "pro", Provider: "prov2", Enabled: true},
		},
		rests: map[string]RouteRest{
			"r1": {Route: "r1", Cause: RestCredit, Cards: []string{"c1"}, At: t0, Until: now.Add(time.Hour)},
			"r2": {Route: "r2", Cause: RestAuth, At: t0, Until: now.Add(time.Hour)},
		},
		Open:  []Open{{Key: OpenKey(boundJ.ID, "c1"), Note: boundJ}, {Key: eTierKey(noRouteJ, "pro"), Note: noRouteJ}},
		Acked: []Open{{Key: OpenKey(boundJ.ID, "c1"), Note: eOverdueMark(boundJ, markAt)}, {Key: eTierKey(noRouteJ, "pro"), Note: eOverdueMark(noRouteJ, markAt)}},
	}
	r := TickReq{Who: MachineActor}

	// 1. behindConds returns no NCoordinatorBehind while quieted.
	assert.Empty(t, behindConds(s, r), "behindConds must return no NCoordinatorBehind for quieted judgments")
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
	now := eQuietNow
	review := now.Add(30 * time.Minute)
	flash := StreamSubject(TierSubject("flash"))
	// A non-kept judgment (NRed) in s.Open with Review set:
	nonKeptJ := Note{ID: "j-nonkept", Kind: Judgment, Type: NRed, Stream: "s1", Primaries: []string{"c1"}, Count: 1,
		At: now.Add(-time.Hour), Review: review, ReviewSet: now}
	// A tick-kept judgment (NNoRoute) waited: closed into s.Acked with Review set; on review
	// expiry it is re-raised into s.Open:
	tickKeptRaised := Note{ID: "j-kept-raised", Kind: Judgment, Type: NNoRoute, Stream: TierSubject("flash"), StreamLevel: true,
		Primaries: []string{"c2"}, Count: 1, At: now.Add(-time.Hour)}
	tickKeptHold := tickKeptRaised
	tickKeptHold.ID, tickKeptHold.Kind, tickKeptHold.Review, tickKeptHold.ReviewSet = "j-kept-hold", Acknowledged, review, now

	s20 := &Snapshot{
		Now:         now.Add(20 * time.Minute),
		Coordinator: "coordinator",
		Open:        []Open{{Key: OpenKey(nonKeptJ.ID, "c1"), Note: nonKeptJ}},
		Acked:       []Open{{Key: OpenKey(tickKeptHold.ID, flash), Note: tickKeptHold}},
	}
	r := TickReq{Who: MachineActor}

	// At s.Now + 20m (before review time): neither is counted by tickOverdue or behindConds.
	p20, _ := tickOverdue(s20, r)
	for _, n := range p20.Notes {
		assert.NotEqual(t, NOverdue, n.Type, "overdue line written before review time")
	}
	assert.Empty(t, behindConds(s20, r), "behindConds counted unexpired waited judgment")

	// At s.Now + 45m (15m after review time expired, exceeding PassEvery): both are counted.
	s45 := &Snapshot{
		Now:         now.Add(45 * time.Minute),
		Coordinator: "coordinator",
		Open:        []Open{{Key: OpenKey(nonKeptJ.ID, "c1"), Note: nonKeptJ}, {Key: OpenKey(tickKeptRaised.ID, flash), Note: tickKeptRaised}},
		Acked: []Open{
			// NOverdue mark written for behindConds:
			{Key: OpenKey("ov1", "c1"), Note: Note{Kind: Acknowledged, Type: NOverdue, What: nonKeptJ.ID, At: now.Add(30 * time.Minute)}},
			{Key: OpenKey("ov2", flash), Note: Note{Kind: Acknowledged, Type: NOverdue, What: tickKeptRaised.ID, At: now.Add(30 * time.Minute)}},
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
	now := eQuietNow
	t0 := now.Add(-3 * time.Hour)
	boundJ := eBoundJ(t0)
	s := &Snapshot{
		Now:         now,
		Coordinator: "coordinator",
		Work:        eQuietWork("pro", nil),
		Routes: []Route{
			{Name: "r1", Tier: "pro", Provider: "prov1", Enabled: true},
			{Name: "r2", Tier: "pro", Provider: "prov2", Enabled: true},
		},
		// Route r1's rest has ended (Until is in the past):
		rests: map[string]RouteRest{
			"r1": {Route: "r1", Cause: RestCredit, Cards: []string{"c1"}, At: t0, Until: now.Add(-10 * time.Minute)},
			"r2": {Route: "r2", Cause: RestAuth, At: t0, Until: now.Add(time.Hour)},
		},
		Open:  []Open{{Key: OpenKey(boundJ.ID, "c1"), Note: boundJ}},
		Acked: []Open{{Key: OpenKey(boundJ.ID, "c1"), Note: eOverdueMark(boundJ, t0.Add(DeadlineJudgment))}},
	}
	// With r1 live again, tier pro is served, so NBound is counted again:
	conds := behindConds(s, TickReq{Who: MachineActor})
	require.NotEmpty(t, conds, "behindConds must count NBound once pro route is live")
	assert.Equal(t, NCoordinatorBehind, conds[0].typ)
}

// TestRestedTierNoRouteNotReRaisedByTickRuleNoRoute verifies that a rested tier's
// NNoRoute judgment is not re-raised/re-tiered by TickRuleNoRoute while the tier
// is not live, and that once the rest ends (TierNotLive false), TickRuleNoRoute resumes.
func TestRestedTierNoRouteNotReRaisedByTickRuleNoRoute(t *testing.T) {
	t.Parallel()
	now := eQuietNow
	t0 := now.Add(-3 * time.Hour) // well past DeadlineJudgment (1h)
	noRouteJ := eNoRouteJ("flash", t0, "r1: credit")
	s := &Snapshot{
		Now:         now,
		Coordinator: "coordinator",
		Work:        eQuietWork("flash", map[string]string{"brief": "RESULT: c1 sha=0123456789ab tier: flash\nPATHS: a.go\n"}),
		Fleet:       eQuietFleet(),
		Routes: []Route{
			{Name: "r1", Tier: "flash", Provider: "prov1", Enabled: true},
			{Name: "r2", Tier: "pro", Provider: "prov2", Enabled: true},
		},
		// r1 is resting until 1 hour from now:
		rests: map[string]RouteRest{"r1": {Route: "r1", Cause: RestCredit, Cards: []string{"c1"}, At: t0, Until: now.Add(time.Hour)}},
		Open:  []Open{{Key: eTierKey(noRouteJ, "flash"), Note: noRouteJ}},
	}
	r := TickReq{Who: MachineActor, AnswerRules: true}

	// 1. While flash is rested (TierNotLive true), TickRuleNoRoute does not re-raise or re-tier c1:
	p, _ := TickRuleNoRoute(s, r)
	assert.Empty(t, p.Units, "TickRuleNoRoute must not re-tier cards of a rested tier's NNoRoute")
	// 2. Once the rest ends (Until in past), TierNotLive is false; TickRuleNoRoute resumes and re-tiers c1 to pro:
	s.rests["r1"] = RouteRest{Route: "r1", Cause: RestCredit, Cards: []string{"c1"}, At: t0, Until: now.Add(-10 * time.Minute)}
	pResume, _ := TickRuleNoRoute(s, r)
	require.NotEmpty(t, pResume.Units, "TickRuleNoRoute must resume re-tiering once rest ends")
	assert.Equal(t, "c1", pResume.Units[0].Key)
	assert.Contains(t, pResume.Units[0].Moved, "re-tiered from flash to pro")
}

// TestRestedTierNoRouteNotInSeatWaitsOverdue verifies that a rested tier's
// NNoRoute judgment is not counted in SeatWaitsOf overdue tally while quieted,
// and that once the rest ends (TierNotLive false), SeatWaitsOf resumes counting it.
func TestRestedTierNoRouteNotInSeatWaitsOverdue(t *testing.T) {
	t.Parallel()
	now := eQuietNow
	t0 := now.Add(-3 * time.Hour) // past DeadlineJudgment (1h)
	noRouteJ := eNoRouteJ("flash", t0, "r1: credit")
	s := &Snapshot{
		Now:         now,
		Coordinator: "coordinator",
		Routes:      []Route{{Name: "r1", Tier: "flash", Provider: "prov1", Enabled: true}},
		rests:       map[string]RouteRest{"r1": {Route: "r1", Cause: RestCredit, Cards: []string{"c1"}, At: t0, Until: now.Add(time.Hour)}},
		Open:        []Open{{Key: eTierKey(noRouteJ, "flash"), Note: noRouteJ}},
	}
	r := TickReq{Who: MachineActor}

	// 1. While flash is rested (TierNotLive true), SeatWaitsOf counts the judgment but not as overdue:
	w := SeatWaitsOf(s, r)
	assert.Equal(t, 1, w.Judgments, "SeatWaitsOf must count the open judgment")
	assert.Equal(t, 0, w.Overdue, "SeatWaitsOf must not count quieted judgment overdue")
	// 2. Once the rest ends (Until in past), TierNotLive is false; SeatWaitsOf resumes counting it overdue:
	s.rests["r1"] = RouteRest{Route: "r1", Cause: RestCredit, Cards: []string{"c1"}, At: t0, Until: now.Add(-10 * time.Minute)}
	wResume := SeatWaitsOf(s, r)
	assert.Equal(t, 1, wResume.Judgments)
	assert.Equal(t, 1, wResume.Overdue, "SeatWaitsOf must count judgment overdue once rest ends")
}
