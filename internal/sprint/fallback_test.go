package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// The provider fallback (fallback.go; tla/ProviderFallback.tla; docs/SPEC-SPRINT.md section
// 5, provider fallback): one provider failing never stalls the fleet, it falls back to the
// next route in line. Every test runs on the fake store with time an argument starting at
// 1000: no real time, sockets or network.

// fallback429 is a provider's rate limit as native and the member write it.
const fallback429 = "provider failure: provider: class=rate-limited status=429 msg=Too many requests"

// fallbackWorld is two members up, one pro primary s1-1, and the pro routes given as
// name/provider pairs, the tier's array in that order, at time 1000.
func fallbackWorld(t *testing.T, routes ...[2]string) *world {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b", "reader-c")
	w.s.Now = time.Unix(1000, 0).UTC()
	w.readersTable()
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 1}))
	w.clean("setup")
	var arr []string
	for _, r := range routes {
		w.s.Routes = append(w.s.Routes, Route{Name: r[0], Tier: cardhdr.RoutePro, Provider: r[1], Model: "m-" + r[0], Enabled: true, Deadline: 600})
		arr = append(arr, r[0])
	}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: arr}
	return w
}

// abc is the three pro routes on providers A, B and C, in that order.
var abc = [][2]string{{"pro-a", "A"}, {"pro-b", "B"}, {"pro-c", "C"}}

// seam turns a reversed witness on for this world alone (fallbackSeams).
func seam(t *testing.T, w *world, names ...string) {
	t.Helper()
	fallbackSeams.Store(w.s.Fleet, names)
	t.Cleanup(func() { fallbackSeams.Delete(w.s.Fleet) })
}

// dealAndTake deals s1-1 and takes its work card, returning the card, its member and the
// attempt the take cut.
func dealAndTake(t *testing.T, w *world) (wc, member string, attempt int) {
	t.Helper()
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc = w.s.Work.Card("s1-1").F("work")
	require.NotEmpty(t, wc, "s1-1 dealt")
	member = w.s.Fleet.Card(wc).Row
	w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	return wc, member, w.s.Work.Card("s1-1").Int("attempt")
}

// fallsBackOnTheSameAttempt is what the fallback must leave after a take on pro-a ended
// 429, in the same step: the card dealt on want with the same attempt, no take ended and no
// redeal spent, its primary still working on it. It returns each rule broken.
func fallsBackOnTheSameAttempt(w *world, wc string, attempt int, want string) []string {
	var bad []string
	c := w.s.Fleet.Card(wc)
	if c == nil || c.Col != Ready || c.F(FieldRoute) != want {
		bad = append(bad, "not dealt on "+want+" in the same step")
	}
	if c != nil && (c.F(FieldTakeEnded) != "" || c.Int("redeals") != 0) {
		bad = append(bad, "a redeal or an ended take spent")
	}
	if c != nil && c.Int(FieldRateReturns) != 0 {
		bad = append(bad, "MaxRateReturns spent")
	}
	pr := w.s.Work.Card("s1-1")
	if pr.Int("attempt") != attempt || pr.F("work") != wc || w.state("s1-1") != Working {
		bad = append(bad, "the attempt changed or the primary left working")
	}
	return bad
}

// TestProviderFaultFallsBackOnTheSameAttempt: tier pro with routes on providers A, B, C in
// that order; a take on A ends 429; in the same step the card is dealt on B with the same
// attempt number and no redeal spent (tla/ProviderFallback.tla Fallback,
// FaultSpendsNothing). Red at BASE: the card went back to ready to wait for the next deal.
// The reversed witness counts the fault as an attempt (the "spend" seam,
// MCProviderFallbackBrokenSpend) and the check fails.
func TestProviderFaultFallsBackOnTheSameAttempt(t *testing.T) {
	t.Parallel()
	w := fallbackWorld(t, abc...)
	wc, member, attempt := dealAndTake(t, w)
	require.Equal(t, "pro-a", w.s.Fleet.Card(wc).F(FieldRoute), "the line's first route")
	p := w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: fallback429}))
	require.Len(t, p.Units, 1)
	assert.Contains(t, p.Units[0].Moved, "fallen back at once to pro-b")
	assert.Empty(t, fallsBackOnTheSameAttempt(w, wc, attempt, "pro-b"))
	assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")
	rates, _ := RateTakes(w.s.Fleet.Card(wc))
	require.Len(t, rates, 1, "the 429 keeps its record, so pro-a backs off")
	assert.Equal(t, "pro-a", rates[0].Route)

	t.Run("reversed witness: the fault counted as an attempt", func(t *testing.T) {
		t.Parallel()
		w := fallbackWorld(t, abc...)
		seam(t, w, "spend")
		wc, member, attempt := dealAndTake(t, w)
		w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: fallback429}))
		assert.Contains(t, fallsBackOnTheSameAttempt(w, wc, attempt, "pro-b"), "a redeal or an ended take spent")
	})
}

// TestReadProviderFaultSpendsNoReader: a read on A returned with `provider:
// class=rate-limited status=429`: the read is asked again at once, of the same reader, on
// B, its reader's counts unchanged (no re-ask counted, not stamped returned), and A cools
// (tla/ProviderFallback.tla Fallback, FaultSpendsNothing). Red at BASE: the return was
// counted (FieldReasked) and the read waited for the ask, which could draw A again.
func TestReadProviderFaultSpendsNoReader(t *testing.T) {
	t.Parallel()
	w := fallbackWorld(t, abc...)
	wc, member, _ := dealAndTake(t, w)
	w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Report: "done"}))
	// the reads draw from the tier's index: put it on pro-a, so a read runs there
	w.s.Fleet.SetProp(PropRouteIndex(cardhdr.RoutePro), "3")
	w.must(Ask(w.s, AskReq{}))
	var onA *Card
	for _, rc := range readsAt(w.s, w.s.Work.Card("s1-1"), 1) {
		if rc.F(FieldRoute) == "pro-a" {
			onA = rc
		}
	}
	require.NotNil(t, onA, "a read on pro-a")
	reader, id := onA.Row, onA.ID
	p := w.must(Read(w.s, ReadReq{As: reader, Sel: Sel{IDs: []string{id}}, Return: true,
		Reason: "no verdict: provider: class=rate-limited status=429 msg=Too many requests"}))
	require.Len(t, p.Units, 1)
	rc := w.s.Readers.Placed(id)
	require.NotNil(t, rc, "the read stays placed")
	assert.Equal(t, reader, rc.Row, "asked again of the same reader")
	assert.Equal(t, Asked, rc.Col)
	assert.Equal(t, "pro-b", rc.F(FieldRoute), "on the next route in line")
	assert.Zero(t, rc.Int(FieldReasked), "no re-ask counted: the reader is not spent")
	assert.Empty(t, rc.F(FieldReturned), "not a returned read")
	_, cools := w.s.providerCooling("A")
	assert.True(t, cools, "A cools")
}

// TestEveryRouteCoolingWaitsWithADeadline: A rate-limited until 1060, B and C unfunded: the
// card waits with deadline 1060 and one judgment naming all three; at 1060 it is dealt on A
// (tla/ProviderFallback.tla WaitHasDeadline, Wake, NeverLeftUnworked). Red at BASE: the deal
// read no funded flag and dealt the card on B at once.
func TestEveryRouteCoolingWaitsWithADeadline(t *testing.T) {
	t.Parallel()
	everyRouteCoolingWaitsWithADeadline(t)
}

// TestFallbackEveryRouteCoolingWaitsWithADeadline is the same check under the name the
// DONE-WHEN command's Fallback|ProviderFault run reaches.
func TestFallbackEveryRouteCoolingWaitsWithADeadline(t *testing.T) {
	t.Parallel()
	everyRouteCoolingWaitsWithADeadline(t)
}

func everyRouteCoolingWaitsWithADeadline(t *testing.T) {
	w := fallbackWorld(t, abc...)
	w.s.Providers = map[string]Provider{"A": {Name: "A", Funded: true}, "B": {Name: "B"}, "C": {Name: "C"}}
	wc, member, attempt := dealAndTake(t, w)
	require.Equal(t, "pro-a", w.s.Fleet.Card(wc).F(FieldRoute), "the one funded provider")
	w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: fallback429}))
	require.Equal(t, Ready, w.state("s1-1"), "no route in line: the card waits ready, never failed")
	assert.Equal(t, attempt, w.s.Work.Card("s1-1").Int("attempt"))

	settled, _ := w.s.withRests()
	why, deadline, ok := settled.TierCooling(cardhdr.RoutePro)
	require.True(t, ok, "every route of pro cools")
	assert.Equal(t, time.Unix(1060, 0).UTC(), deadline, "the deadline is A's backoff end")
	for _, want := range []string{"A rate-limited until " + stamp(time.Unix(1060, 0)), "B unfunded", "C unfunded"} {
		assert.Contains(t, why, want)
	}
	p, _ := TickDeal(w.s, TickReq{})
	w.must(p)
	var judged []Note
	for _, n := range w.notes {
		if n.Type == NNoRoute && n.Stream == TierSubject(cardhdr.RoutePro) {
			judged = append(judged, n)
		}
	}
	require.Len(t, judged, 1, "one judgment for the tier: %+v", w.notes)
	for _, want := range []string{"A rate-limited", "B unfunded", "C unfunded"} {
		assert.Contains(t, judged[0].What, want)
	}
	assert.Equal(t, Ready, w.state("s1-1"), "not dealt while every route cools")

	w.s.Now = deadline
	p, _ = TickDeal(w.s, TickReq{})
	w.must(p)
	assert.Equal(t, Working, w.state("s1-1"), "dealt at the deadline")
	assert.Equal(t, "pro-a", w.s.Fleet.Card(wc).F(FieldRoute), "on A, its backoff over")
	assert.Equal(t, attempt, w.s.Work.Card("s1-1").Int("attempt"), "the same attempt")
}

// TestUnfundedProviderNeverInLine: A applied with funded=false is out of line: the deal
// draws B, the next in line, and the check of the tier is no judgment while B and C serve
// (tla/ProviderFallback.tla NoDealToCooling). Red at BASE: the draw never read funded.
func TestUnfundedProviderNeverInLine(t *testing.T) {
	t.Parallel()
	unfundedProviderNeverInLine(t)
}

// TestFallbackUnfundedProviderNeverInLine is the same check under the name the DONE-WHEN
// command's Fallback|ProviderFault run reaches.
func TestFallbackUnfundedProviderNeverInLine(t *testing.T) {
	t.Parallel()
	unfundedProviderNeverInLine(t)
}

func unfundedProviderNeverInLine(t *testing.T) {
	w := fallbackWorld(t, abc...)
	w.s.Providers = map[string]Provider{"A": {Name: "A"}, "B": {Name: "B", Funded: true}, "C": {Name: "C", Funded: true}}
	wc, _, _ := dealAndTake(t, w)
	assert.Equal(t, "pro-b", w.s.Fleet.Card(wc).F(FieldRoute), "A is unfunded: never in line")
	_, _, ok := w.s.TierCooling(cardhdr.RoutePro)
	assert.False(t, ok, "B and C serve: no wait")
	_, why, _ := w.s.routeServed(w.s.Work.Card("s1-1"), nil, "")
	assert.Empty(t, why)
}

// coolsPerProvider runs the per-provider case: routes pro-a1 and pro-a2 on A and pro-b on
// B; a take on pro-a1 ends 429; the route it falls back to.
func coolsPerProvider(t *testing.T, seams ...string) string {
	t.Helper()
	w := fallbackWorld(t, [2]string{"pro-a1", "A"}, [2]string{"pro-a2", "A"}, [2]string{"pro-b", "B"})
	if len(seams) > 0 {
		seam(t, w, seams...)
	}
	wc, member, _ := dealAndTake(t, w)
	require.Equal(t, "pro-a1", w.s.Fleet.Card(wc).F(FieldRoute))
	w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Report: fallback429}))
	return w.s.Fleet.Card(wc).F(FieldRoute)
}

// TestCoolingIsPerProvider: two routes of A: one fault takes both out of line, so the card
// falls back to B, never to A's other route (tla/ProviderFallback.tla: a provider cools,
// every route of it out). Red at BASE: a rate limit backed off the one route, and the card
// waited for the next deal. The reversed witness leaves cooling out of the line (the
// "nocooling" seam, MCProviderFallbackBrokenDealCooling): the card falls back onto pro-a2.
func TestCoolingIsPerProvider(t *testing.T) {
	t.Parallel()
	coolingIsPerProvider(t)
}

// TestFallbackCoolingIsPerProvider is the same check under the name the DONE-WHEN
// command's Fallback|ProviderFault run reaches.
func TestFallbackCoolingIsPerProvider(t *testing.T) {
	t.Parallel()
	coolingIsPerProvider(t)
}

func coolingIsPerProvider(t *testing.T) {
	assert.Equal(t, "pro-b", coolsPerProvider(t), "both of A's routes cool")
	t.Run("reversed witness: the line ignores cooling", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "pro-a2", coolsPerProvider(t, "nocooling"), "the witness draws A's other route")
	})
}

// TestProviderFaultClassAndRetryAfter pins the fault classes (2) reads and the windows the
// provider's words name (1).
func TestProviderFaultClassAndRetryAfter(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]string{
		fallback429: classRateLimited,
		"provider failure: provider: class=provider-5xx status=502 msg=bad gateway": class5xx,
		"provider failure: provider: class=timeout status=- msg=no answer":          classTimeout,
		"provider failure: provider: class=out-of-credit status=402 msg=pay up":     RestCredit,
		"provider failure: provider: class=auth status=401 msg=bad key":             "",
		"no result: no RESULT.md shape":                                             "",
		"tests red":                                                                 "",
	} {
		assert.Equal(t, want, providerFaultClass(line), line)
	}
	for line, want := range map[string]time.Duration{
		"429: please retry after 30 s":   30 * time.Second,
		"Rate limited, try again in 12s": 12 * time.Second,
		"Retry-After: 20":                20 * time.Second,
		"retry after 2 minutes":          2 * time.Minute,
		"Retry-After: 99999":             RateBackoffMax,
		"try again in 250ms":             time.Second,
	} {
		d, ok := retryAfter(line)
		assert.True(t, ok, line)
		assert.Equal(t, want, d, line)
	}
	_, ok := retryAfter("Too many requests")
	assert.False(t, ok)
	assert.True(t, strings.HasPrefix(PropProviderCool("A"), "provider_cool_"))
}
