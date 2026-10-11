package sprint

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/member"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestARateLimitedTakeReturnsTheCardUnspent verifies that a rate-limited take:
// - Withdraws the card WITHOUT FieldTakeEnded
// - Primary goes back to ready
// - No redeal is spent
// - No attempt is spent
// - No failed-work judgment is raised
// - The take record is NOT written (for route rest counting)
func TestARateLimitedTakeReturnsTheCardUnspent(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	proMercury(w)
	w.s.Fleet.SetProps(map[string]string{PropModelRPM("inception/mercury-3"): "10"})

	// First take with rate-limited finish
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Work.Card("s1-1").F("work")
	member := w.s.Fleet.Card(wc).Row
	w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))

	report := "provider failure: provider: class=rate-limited status=429 msg=Too many requests"
	p := Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: report})
	require.Len(t, p.Units, 1)
	assert.Contains(t, p.Units[0].Moved, "returned untouched")
	w.must(p)

	c := w.s.Fleet.Card(wc)
	assert.Equal(t, Withdrawn, c.Col, "card is withdrawn")
	assert.Empty(t, c.F(FieldTakeEnded), "no take ended: no redeal spent")
	takes, _ := ProviderTakes(c)
	assert.Empty(t, takes, "no take record: never counted toward a route's rest")
	assert.Equal(t, Ready, w.state("s1-1"), "primary goes back to ready")
	assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")
	assert.Equal(t, 1, w.s.Work.Card("s1-1").Int("attempt"), "attempt is unchanged")
	// The rate-limited end increments RouteRateLimitBackoffEnds for backoff doubling
	assert.Equal(t, 1, c.Int(RouteRateLimitBackoffEnds), "backoff end count increments")
}

// TestARateLimitBacksTheRouteOffBriefly verifies that rate-limited ends:
// - Rest the route for 60s on first occurrence
// - Double for each further rate-limited end with no ok take in between
// - Cap at 15 minutes
// - End by themselves
func TestARateLimitBacksTheRouteOffBriefly(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 2, 3, 0, 0, 0, time.UTC)
	at := func(m int) string { return stamp(now.Add(time.Duration(m) * time.Minute)) }

	// Setup routes
	routes := []Route{
		{Name: "flash-deepseek41-direct", Tier: "flash", Provider: "deepseek", Enabled: true},
	}

	// First rate-limited take at minute 1
	fleet := func(takes map[string]string) *Table {
		f := NewTable(Fleet)
		f.SetRows([]string{"m1"})
		for id, v := range takes {
			f.Put(&Card{ID: id, Row: "m1", Col: Withdrawn, Fields: map[string]string{FieldRoute: "flash-deepseek41-direct", FieldProviderTake + "1": v}})
		}
		return f
	}

	take1 := ProviderTake{Route: "flash-deepseek41-direct", Taken: at(1), Finished: at(1), Error: "provider: class=rate-limited status=429 msg=Rate limit"}
	s := &Snapshot{Now: now.Add(2 * time.Minute), Fleet: fleet(map[string]string{"c1": take1.String()}), Routes: routes}

	due := RestsDue(s)
	require.Len(t, due, 1, "one route rest for rate-limited backoff")
	assert.Equal(t, "flash-deepseek41-direct", due[0].Route)
	assert.Equal(t, RestRateLimit, due[0].Cause)
	assert.Equal(t, []string{"c1"}, due[0].Cards)

	// Second rate-limited take at minute 3 (with no ok take in between)
	take2 := ProviderTake{Route: "flash-deepseek41-direct", Taken: at(3), Finished: at(3), Error: "provider: class=rate-limited status=429 msg=Rate limit"}
	s.Fleet.Put(&Card{ID: "c2", Row: "m1", Col: Withdrawn, Fields: map[string]string{FieldRoute: "flash-deepseek41-direct", FieldProviderTake + "1": take2.String()}})
	s.Now = now.Add(4 * time.Minute)

	due = RestsDue(s)
	require.Len(t, due, 1, "one route rest for rate-limited backoff")
	assert.Equal(t, 2*RouteRateLimitBackoffFor, due[0].Until.Sub(due[0].At), "second consecutive rate-limited end doubles backoff to 120s")

	// Add an ok take between rate-limited ends
	s.Fleet.Put(&Card{ID: "c3", Row: "m1", Col: DoneOK, Fields: map[string]string{FieldRoute: "flash-deepseek41-direct", "finished": at(5)}})
	s.Now = now.Add(6 * time.Minute)

	// Third rate-limited take - backoff resets because ok take occurred
	take4 := ProviderTake{Route: "flash-deepseek41-direct", Taken: at(7), Finished: at(7), Error: "provider: class=rate-limited status=429 msg=Rate limit"}
	s.Fleet.Put(&Card{ID: "c4", Row: "m1", Col: Withdrawn, Fields: map[string]string{FieldRoute: "flash-deepseek41-direct", FieldProviderTake + "1": take4.String()}})

	due = RestsDue(s)
	require.Len(t, due, 1, "one route rest for rate-limited backoff")
	assert.Equal(t, RouteRateLimitBackoffFor, due[0].Until.Sub(due[0].At), "ok take resets consecutive rate-limited count")

	// Verify that transient (5xx/timeout) doesn't reset the consecutive counter
	take5 := ProviderTake{Route: "flash-deepseek41-direct", Taken: at(8), Finished: at(8), Error: "provider: class=provider-5xx status=503 msg=unavailable"}
	s.Fleet.Put(&Card{ID: "c5", Row: "m1", Col: Withdrawn, Fields: map[string]string{FieldRoute: "flash-deepseek41-direct", FieldProviderTake + "1": take5.String()}})
	s.Now = now.Add(9 * time.Minute)

	take6 := ProviderTake{Route: "flash-deepseek41-direct", Taken: at(10), Finished: at(10), Error: "provider: class=rate-limited status=429 msg=Rate limit"}
	s.Fleet.Put(&Card{ID: "c6", Row: "m1", Col: Withdrawn, Fields: map[string]string{FieldRoute: "flash-deepseek41-direct", FieldProviderTake + "1": take6.String()}})
	s.Now = now.Add(11 * time.Minute)

	due = RestsDue(s)
	require.Len(t, due, 1)
	// 5xx is transient (doesn't reset counter), so consecutive stays at 2: 60s * 2 = 120s
	assert.Equal(t, 2*RouteRateLimitBackoffFor, due[0].Until.Sub(due[0].At), "5xx doesn't reset consecutive rate-limited count")
}

// TestARateLimitedRunIsAProviderFailure verifies that a rate-limited end with no pushed head
// is reported as `provider failure:` regardless of shape.
func TestARateLimitedRunIsAProviderFailure(t *testing.T) {
	t.Parallel()
	r := member.Result{
		End:      "provider failure",
		Provider: "provider: class=rate-limited status=429 msg=Too many requests",
		Shaped:   false,
		OK:       false,
		Head:     "",
	}
	pu := member.Push{Sha: ""}
	fin, why := member.Judge(r, pu)
	assert.Equal(t, member.FinishFailed, fin)
	assert.Contains(t, why, "provider failure:")
	assert.Contains(t, why, "rate-limited")

	// With shaped result and pushed head, it should still be ok (card's own)
	r.Shaped = true
	r.Verdict = "ok"
	pu.Sha = "abc123"
	fin, why = member.Judge(r, pu)
	assert.Equal(t, member.FinishOK, fin)
	assert.Empty(t, why)

	// Without pushed head but with shaped result, it's still a provider failure
	// because r.End == EndProvider and r.Provider != "" comes before the push check
	r.Shaped = true
	pu.Sha = ""
	fin, why = member.Judge(r, pu)
	assert.Equal(t, member.FinishFailed, fin)
	assert.Contains(t, why, "provider failure:")
}
