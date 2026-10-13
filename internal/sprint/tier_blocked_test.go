package sprint

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnUnfundedTierIsOneJudgmentAndQuietsItsCards(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)

	// Tier pro's array has two routes, one resting for credit and one for auth.
	routes := []Route{
		{Name: "pro-credit", Tier: "pro", Provider: "prov-credit", Model: "deepseek-v3", Enabled: true},
		{Name: "pro-auth", Tier: "pro", Provider: "prov-auth", Model: "claude-3-5-sonnet", Enabled: true},
	}
	tiers := map[string][]string{
		"pro": {"pro-credit", "pro-auth"},
	}

	f := NewTable(Fleet)
	f.SetRows([]string{"m1"})
	ctl := &Card{ID: CtlID("m1"), Row: "m1", Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up}}
	f.Put(ctl)

	// Both providers rest at t0 and through t0 + 4h.
	f.SetProp(PropProviderRest("prov-credit"), RouteRest{
		At:    t0,
		Until: t0.Add(4 * time.Hour),
		Cards: []string{"s1-1"},
		Cause: RestCredit,
		Why:   "out of credit",
	}.value())
	f.SetProp(PropProviderRest("prov-auth"), RouteRest{
		At:    t0,
		Until: t0.Add(4 * time.Hour),
		Cause: RestAuth,
		Why:   "provider refused auth (401)",
	}.value())

	// Three pro primaries are ready in two streams: s1-1, s1-2 in s1; s2-1 in s2.
	w := NewTable(Work)
	w.SetProp(PropDealtMax, "2h")
	w.SetRows([]string{"s1", "s2"})
	w.Put(&Card{ID: "s1-1", Row: "s1", Col: Ready, Fields: map[string]string{FieldTierNow: "pro"}})
	w.Put(&Card{ID: "s1-2", Row: "s1", Col: Ready, Fields: map[string]string{FieldTierNow: "pro"}})
	w.Put(&Card{ID: "s2-1", Row: "s2", Col: Ready, Fields: map[string]string{FieldTierNow: "pro"}})

	// One more pro primary is at its bound with its NBound judgment open for 3h.
	w.Put(&Card{ID: "s1-3", Row: "s1", Col: Review, Fields: map[string]string{FieldTierNow: "pro", "attempt": "3"}})
	open := []Open{
		{
			Key: OpenKey("j-bound-s1-3", "s1-3"),
			Note: Note{
				ID:        "j-bound-s1-3",
				Kind:      Judgment,
				Type:      NBound,
				Stream:    "s1",
				Card:      "s1-3",
				Primaries: []string{"s1-3"},
				At:        t0,
				What:      "attempt 3 reached its bound on pro",
			},
		},
	}

	// The dealt work card of one is fleet Ready past the dealt bound.
	wc := &Card{
		ID:  "s1-1.w1",
		Row: "m1",
		Col: Ready,
		Fields: map[string]string{
			"primary":     "s1-1",
			"stream":      "s1",
			"attempt":     "1",
			FieldRoute:    "pro-credit",
			"dealt":       stamp(t0),
			"first_dealt": stamp(t0),
		},
	}
	f.Put(wc)

	s := &Snapshot{
		Now:     t0.Add(3 * time.Hour),
		Fleet:   f,
		Work:    w,
		Readers: NewTable(Readers),
		Merge:   NewTable(Merge),
		Routes:  routes,
		Tiers:   tiers,
		Open:    open,
	}

	req := TickReq{}
	pDeal, _ := TickDeal(s, req)
	pDeadlines, _ := TickDeadlines(s, req)
	pOverdue, _ := TickOverdue(s, req)

	// Assert exactly one NNoRoute for pro whose What opens "tier pro cannot be dealt: every route rests ("
	var proNoRoutes []Note
	for _, n := range pDeal.Notes {
		if n.Type == NNoRoute {
			proNoRoutes = append(proNoRoutes, n)
		}
	}
	require.Len(t, proNoRoutes, 1, "exactly one NNoRoute for pro")
	assert.True(t, strings.HasPrefix(proNoRoutes[0].What, "tier pro cannot be dealt: every route rests ("),
		"NNoRoute What must open with 'tier pro cannot be dealt: every route rests (', got: %s", proNoRoutes[0].What)

	// Zero NWorkLate
	var workLates []Note
	for _, n := range pDeadlines.Notes {
		if n.Type == NWorkLate {
			workLates = append(workLates, n)
		}
	}
	assert.Empty(t, workLates, "zero NWorkLate for tier-blocked card")

	// Zero NRaisedAgain for the bound judgment
	var raisedAgain []Note
	for _, n := range pOverdue.Notes {
		if n.Type == NRaisedAgain {
			raisedAgain = append(raisedAgain, n)
		}
	}
	assert.Empty(t, raisedAgain, "zero NRaisedAgain for the bound judgment")

	// NSeatSilent not raised for it
	var seatSilent []Note
	for _, n := range pOverdue.Notes {
		if n.Type == NSeatSilent {
			seatSilent = append(seatSilent, n)
		}
	}
	assert.Empty(t, seatSilent, "NSeatSilent not raised for tier-blocked judgments")
}

func TestAStreamBlockedOnlyByAnUnfundedTierIsNotStale(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)

	// Inbox with a StreamClock whose TierBlocked field is set shows no NStreamStale group
	clockBlocked := StreamClock{
		Stream:      "s1",
		State:       "ready",
		Progress:    t0.Add(-2 * time.Hour),
		TierBlocked: "pro",
	}
	reqBlocked := InboxReq{
		Now:     t0,
		Streams: []StreamClock{clockBlocked},
		Stale:   time.Hour,
	}
	groupsBlocked := Inbox(reqBlocked)
	for _, g := range groupsBlocked {
		assert.NotEqual(t, NStreamStale, g.Type, "a stream blocked only by an unfunded tier shows no NStreamStale")
	}

	// The same clock with TierBlocked empty shows one
	clockUnblocked := StreamClock{
		Stream:   "s1",
		State:    "ready",
		Progress: t0.Add(-2 * time.Hour),
	}
	reqUnblocked := InboxReq{
		Now:     t0,
		Streams: []StreamClock{clockUnblocked},
		Stale:   time.Hour,
	}
	groupsUnblocked := Inbox(reqUnblocked)
	foundStale := false
	for _, g := range groupsUnblocked {
		if g.Type == NStreamStale && g.Stream == "s1" {
			foundStale = true
			break
		}
	}
	assert.True(t, foundStale, "stream without TierBlocked shows NStreamStale")
}

func TestAWokenRouteResumesTheChecks(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)

	// The first state with one pro route's rest ended
	routes := []Route{
		{Name: "pro-credit", Tier: "pro", Provider: "prov-credit", Model: "deepseek-v3", Enabled: true},
		{Name: "pro-auth", Tier: "pro", Provider: "prov-auth", Model: "claude-3-5-sonnet", Enabled: true},
	}
	tiers := map[string][]string{
		"pro": {"pro-credit", "pro-auth"},
	}

	f := NewTable(Fleet)
	f.SetRows([]string{"m1"})
	ctl := &Card{ID: CtlID("m1"), Row: "m1", Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up}}
	f.Put(ctl)

	// prov-credit rest ENDED at t0 + 1h (now is t0 + 3h)
	f.SetProp(PropProviderRest("prov-credit"), RouteRest{
		At:    t0,
		Until: t0.Add(1 * time.Hour),
		Cards: []string{"s1-1"},
		Cause: RestCredit,
		Why:   "out of credit",
	}.value())
	// prov-auth still resting
	f.SetProp(PropProviderRest("prov-auth"), RouteRest{
		At:    t0,
		Until: t0.Add(4 * time.Hour),
		Cause: RestAuth,
		Why:   "provider refused auth (401)",
	}.value())

	w := NewTable(Work)
	w.SetRows([]string{"s1", "s2"})
	w.Put(&Card{ID: "s1-1", Row: "s1", Col: Ready, Fields: map[string]string{FieldTierNow: "pro"}})
	w.Put(&Card{ID: "s1-2", Row: "s1", Col: Ready, Fields: map[string]string{FieldTierNow: "pro"}})
	w.Put(&Card{ID: "s2-1", Row: "s2", Col: Ready, Fields: map[string]string{FieldTierNow: "pro"}})
	w.Put(&Card{ID: "s1-3", Row: "s1", Col: Review, Fields: map[string]string{FieldTierNow: "pro", "attempt": "3"}})

	// Previously open NNoRoute judgment for pro and NBound for s1-3
	open := []Open{
		{
			Key: OpenKey("j-noroute-pro", StreamSubject(TierSubject("pro"))),
			Note: Note{
				ID:     "j-noroute-pro",
				Kind:   Judgment,
				Type:   NNoRoute,
				Stream: TierSubject("pro"),
				At:     t0,
				What:   "tier pro cannot be dealt: every route rests",
			},
		},
		{
			Key: OpenKey("j-bound-s1-3", "s1-3"),
			Note: Note{
				ID:        "j-bound-s1-3",
				Kind:      Judgment,
				Type:      NBound,
				Stream:    "s1",
				Card:      "s1-3",
				Primaries: []string{"s1-3"},
				At:        t0,
				What:      "attempt 3 reached its bound on pro",
			},
		},
	}

	wc := &Card{
		ID:  "s1-1.w1",
		Row: "m1",
		Col: Ready,
		Fields: map[string]string{
			"primary":     "s1-1",
			"stream":      "s1",
			"attempt":     "1",
			FieldRoute:    "pro-credit",
			"dealt":       stamp(t0),
			"first_dealt": stamp(t0),
		},
	}
	f.Put(wc)

	s := &Snapshot{
		Now:     t0.Add(3 * time.Hour),
		Fleet:   f,
		Work:    w,
		Readers: NewTable(Readers),
		Merge:   NewTable(Merge),
		Routes:  routes,
		Tiers:   tiers,
		Open:    open,
	}

	req := TickReq{}
	pDeal, _ := TickDeal(s, req)

	// NNoRoute closes
	closedNoRoute := false
	for _, c := range pDeal.Closes {
		if c.Note.Type == NNoRoute && c.Note.Stream == TierSubject("pro") {
			closedNoRoute = true
			break
		}
	}
	assert.True(t, closedNoRoute, "NNoRoute closes when a route of the tier is live")

	// Bound judgment's raise is due again
	pOverdue, _ := TickOverdue(s, req)
	var raisedAgain []Note
	for _, n := range pOverdue.Notes {
		if n.Type == NRaisedAgain && (strings.HasPrefix(n.What, "j-bound-s1-3") || slices.Contains(n.Primaries, "s1-3")) {
			raisedAgain = append(raisedAgain, n)
		}
	}
	assert.NotEmpty(t, raisedAgain, "bound judgment's raise is due again once the tier is funded")
}
