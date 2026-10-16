package sprint

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fT0 = time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)

// fTierBlockedSnap is tier pro with two routes, prov-credit resting until creditUntil and
// prov-auth until t0+4h; three pro primaries ready in s1 and s2 (s1-1, s1-2, s2-1), s1-3 in
// review at attempt 3, s1-1's work card dealt at t0 and fleet Ready, now t0+3h.
func fTierBlockedSnap(creditUntil time.Time, workProps map[string]string, open ...Open) *Snapshot {
	f := NewTable(Fleet)
	f.SetRows([]string{"m1"})
	f.Put(&Card{ID: CtlID("m1"), Row: "m1", Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up}})
	f.SetProp(PropProviderRest("prov-credit"), RouteRest{At: fT0, Until: creditUntil, Cards: []string{"s1-1"}, Cause: RestCredit, Why: "out of credit"}.value())
	f.SetProp(PropProviderRest("prov-auth"), RouteRest{At: fT0, Until: fT0.Add(4 * time.Hour), Cause: RestAuth, Why: "provider refused auth (401)"}.value())
	f.Put(&Card{ID: "s1-1.w1", Row: "m1", Col: Ready, Fields: map[string]string{
		"primary": "s1-1", "stream": "s1", "attempt": "1", FieldRoute: "pro-credit", "dealt": stamp(fT0), "first_dealt": stamp(fT0),
	}})

	w := NewTable(Work)
	for k, v := range workProps {
		w.SetProp(k, v)
	}
	w.SetRows([]string{"s1", "s2"})
	for _, id := range []string{"s1-1", "s1-2", "s2-1"} {
		w.Put(&Card{ID: id, Row: id[:2], Col: Ready, Fields: map[string]string{FieldTierNow: "pro"}})
	}
	w.Put(&Card{ID: "s1-3", Row: "s1", Col: Review, Fields: map[string]string{FieldTierNow: "pro", "attempt": "3"}})

	return &Snapshot{
		Now: fT0.Add(3 * time.Hour), Fleet: f, Work: w, Readers: NewTable(Readers), Merge: NewTable(Merge),
		Routes: []Route{
			{Name: "pro-credit", Tier: "pro", Provider: "prov-credit", Model: "deepseek-v3", Enabled: true},
			{Name: "pro-auth", Tier: "pro", Provider: "prov-auth", Model: "claude-3-5-sonnet", Enabled: true},
		},
		Tiers: map[string][]string{"pro": {"pro-credit", "pro-auth"}},
		Open:  open,
	}
}

// fBoundOpen is s1-3's NBound judgment, open since t0.
var fBoundOpen = Open{Key: OpenKey("j-bound-s1-3", "s1-3"), Note: Note{
	ID: "j-bound-s1-3", Kind: Judgment, Type: NBound, Stream: "s1", Card: "s1-3", Primaries: []string{"s1-3"},
	At: fT0, What: "attempt 3 reached its bound on pro",
}}

func fTyped(notes []Note, typ string) []Note {
	return slices.DeleteFunc(slices.Clone(notes), func(n Note) bool { return n.Type != typ })
}

func TestAnUnfundedTierIsOneJudgmentAndQuietsItsCards(t *testing.T) {
	t.Parallel()
	// Both providers rest through t0+4h; the bound judgment has been open 3h.
	s := fTierBlockedSnap(fT0.Add(4*time.Hour), map[string]string{PropDealtMax: "2h"}, fBoundOpen)
	pDeal, _ := TickDeal(s, TickReq{})
	pDeadlines, _ := TickDeadlines(s, TickReq{})
	pOverdue, _ := TickOverdue(s, TickReq{})

	proNoRoutes := fTyped(pDeal.Notes, NNoRoute)
	require.Len(t, proNoRoutes, 1, "exactly one NNoRoute for pro")
	assert.True(t, strings.HasPrefix(proNoRoutes[0].What, "tier pro cannot be dealt: every route rests ("),
		"NNoRoute What must open with 'tier pro cannot be dealt: every route rests (', got: %s", proNoRoutes[0].What)
	assert.Empty(t, fTyped(pDeadlines.Notes, NWorkLate), "zero NWorkLate for tier-blocked card")
	assert.Empty(t, fTyped(pOverdue.Notes, NRaisedAgain), "zero NRaisedAgain for the bound judgment")
	assert.Empty(t, fTyped(pOverdue.Notes, NSeatSilent), "NSeatSilent not raised for tier-blocked judgments")
}

func TestAStreamBlockedOnlyByAnUnfundedTierIsNotStale(t *testing.T) {
	t.Parallel()
	inbox := func(tierBlocked string) []Group {
		return Inbox(InboxReq{Now: fT0, Stale: time.Hour, Streams: []StreamClock{
			{Stream: "s1", State: "ready", Progress: fT0.Add(-2 * time.Hour), TierBlocked: tierBlocked},
		}})
	}
	for _, g := range inbox("pro") {
		assert.NotEqual(t, NStreamStale, g.Type, "a stream blocked only by an unfunded tier shows no NStreamStale")
	}
	foundStale := slices.ContainsFunc(inbox(""), func(g Group) bool { return g.Type == NStreamStale && g.Stream == "s1" })
	assert.True(t, foundStale, "stream without TierBlocked shows NStreamStale")
}

func TestAWokenRouteResumesTheChecks(t *testing.T) {
	t.Parallel()
	// prov-credit's rest ended at t0+1h (now is t0+3h); NNoRoute for pro and NBound for s1-3 are open.
	noRoute := Open{Key: OpenKey("j-noroute-pro", StreamSubject(TierSubject("pro"))), Note: Note{
		ID: "j-noroute-pro", Kind: Judgment, Type: NNoRoute, Stream: TierSubject("pro"), At: fT0,
		What: "tier pro cannot be dealt: every route rests",
	}}
	s := fTierBlockedSnap(fT0.Add(time.Hour), nil, noRoute, fBoundOpen)
	pDeal, _ := TickDeal(s, TickReq{})
	closedNoRoute := slices.ContainsFunc(pDeal.Closes, func(c Open) bool {
		return c.Note.Type == NNoRoute && c.Note.Stream == TierSubject("pro")
	})
	assert.True(t, closedNoRoute, "NNoRoute closes when a route of the tier is live")

	pOverdue, _ := TickOverdue(s, TickReq{})
	raisedAgain := slices.ContainsFunc(fTyped(pOverdue.Notes, NRaisedAgain), func(n Note) bool {
		return strings.HasPrefix(n.What, "j-bound-s1-3") || slices.Contains(n.Primaries, "s1-3")
	})
	assert.True(t, raisedAgain, "bound judgment's raise is due again once the tier is funded")
}
