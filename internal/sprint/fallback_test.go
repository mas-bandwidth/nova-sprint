package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fallbackWorld is time 1000 with three pro routes on three providers, A, B and C, in
// that array order: the line tla/ProviderFallback.tla draws.
func fallbackWorld(t *testing.T) *world {
	t.Helper()
	w := setup(t, 1)
	w.s.Now = time.Unix(1000, 0).UTC()
	w.s.Routes = []Route{
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "pa", Model: "a", Deadline: 600, Enabled: true, First: true},
		{Name: "pro-b", Tier: cardhdr.RoutePro, Provider: "pb", Model: "b", Deadline: 600, Enabled: true},
		{Name: "pro-c", Tier: cardhdr.RoutePro, Provider: "pc", Model: "c", Deadline: 600, Enabled: true},
	}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-a", "pro-b", "pro-c"}}
	w.s.Providers = map[string]Provider{
		"pa": {Name: "pa", Funded: true},
		"pb": {Name: "pb", Funded: true},
		"pc": {Name: "pc", Funded: true},
	}
	return w
}

const fallback429 = "provider failure: provider: class=rate-limited status=429 msg=Too many requests; retry after 60 s"

// TestProviderFaultFallsBackOnTheSameAttempt: a take on A ends 429, and in the same
// tick, at the same attempt and generation, the same work card is dealt on B: no
// redeal and no attempt spent (tla/ProviderFallback.tla, Fallback).
func TestProviderFaultFallsBackOnTheSameAttempt(t *testing.T) {
	t.Parallel()
	w := fallbackWorld(t)
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wcID := w.s.Work.Card("s1-1").F("work")
	require.Equal(t, "pro-a", w.s.Fleet.Card(wcID).F(FieldRoute), "the first deal draws A")
	member := w.s.Fleet.Card(wcID).Row
	w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wcID}}, Gens: w.gens(wcID)}))
	require.Equal(t, 1, w.s.Work.Card("s1-1").Int("attempt"))
	p := w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wcID}}, Gens: w.gens(wcID), Failed: true, Report: fallback429}))
	require.Len(t, p.Units, 1, "%+v", p)

	c := w.s.Fleet.Card(wcID)
	require.NotNil(t, c)
	assert.Equal(t, Ready, c.Col, "the same work card is dealt again, not withdrawn")
	assert.Equal(t, "pro-b", c.F(FieldRoute), "it falls back to B")
	assert.Equal(t, "1", c.F("attempt"), "the same attempt")
	assert.Zero(t, c.Int("redeals"), "no redeal spent")
	assert.Empty(t, c.F(FieldTakeEnded), "no take ended")
	assert.Equal(t, Working, w.state("s1-1"), "the primary works on the same attempt")
	assert.Equal(t, 1, w.s.Work.Card("s1-1").Int("attempt"))
	assert.Equal(t, wcID, w.s.Work.Card("s1-1").F("work"), "the same work card")
	cools := w.s.providerCoolings()
	require.True(t, cools["pa"].cooling(w.s.Now), "A cools")
	assert.Contains(t, cools["pa"].said(), "rate-limited until")

	t.Run("reversed witness: a fault counted as an attempt", func(t *testing.T) {
		// with the test-only seam, the fault is not a same-attempt fallback: the card is
		// withdrawn and the primary waits ready, the expectation above fails
		w := fallbackWorld(t)
		w.s.Fleet.SetProp(seamFaultCountsAttempt, "1")
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		wcID := w.s.Work.Card("s1-1").F("work")
		member := w.s.Fleet.Card(wcID).Row
		w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wcID}}, Gens: w.gens(wcID)}))
		w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wcID}}, Gens: w.gens(wcID), Failed: true, Report: fallback429}))
		assert.Equal(t, Withdrawn, w.s.Fleet.Card(wcID).Col, "the seam counts the fault as an attempt: no same-attempt fallback")
		assert.Equal(t, Ready, w.state("s1-1"))
	})
}

// TestReadProviderFaultSpendsNoReader: a read on A returned with a provider's 429 cools
// A and is asked again on B; the reader's counts do not move (tla/ProviderFallback.tla,
// ProviderFault and NoDealToCooling).
func TestReadProviderFaultSpendsNoReader(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Now = time.Unix(1000, 0).UTC()
	w.s.Routes = []Route{
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "pa", Model: "a", Enabled: true, First: true},
		{Name: "pro-b", Tier: cardhdr.RoutePro, Provider: "pb", Model: "b", Enabled: true},
		{Name: "pro-c", Tier: cardhdr.RoutePro, Provider: "pc", Model: "c", Enabled: true},
	}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-a", "pro-b", "pro-c"}}
	w.s.Providers = map[string]Provider{"pa": {Funded: true}, "pb": {Funded: true}, "pc": {Funded: true}}
	putReview(w, "s1-1", proBrief, 1, 1, "head-s1-1")
	askReaders(t, w, nil)
	reads := readsAt(w.s, w.s.Work.Card("s1-1"), 1)
	require.NotEmpty(t, reads)
	for _, rc := range reads {
		require.Equal(t, "pro-a", rc.F(FieldRoute), "the read starts on A")
	}
	rc := reads[0]
	reader := rc.F("reader")
	w.must(Read(w.s, ReadReq{As: reader, Return: true, Reason: fallback429, Sel: Sel{IDs: []string{rc.ID}}}))
	restv, _ := w.s.Fleet.Prop(PropProviderRest("pa"))
	assert.NotEmpty(t, restv, "the provider rest is written")
	cools := w.s.providerCoolings()
	assert.True(t, cools["pa"].cooling(w.s.Now), "pa cools: %+v", cools)
	assert.NotNil(t, w.s.Readers.Placed(rc.ID), "the returned read stays on its reader")
	assert.Equal(t, 1, w.s.Work.Card("s1-1").Int("attempt"))

	w.must(Ask(w.s, AskReq{}))
	var onB int
	for _, c := range readsAt(w.s, w.s.Work.Card("s1-1"), 1) {
		if c.F(FieldRoute) == "pro-b" {
			onB++
		}
	}
	assert.GreaterOrEqual(t, onB, 1, "the read is asked on B, never on cooled A")
	assert.Nil(t, w.s.Readers.Placed(rc.ID), "the returned read is retired and re-asked at the same attempt")
	pr := w.s.Work.Card("s1-1")
	assert.Zero(t, pr.Int("broken_reads"), "no reader count moved")
	assert.Equal(t, Review, pr.Col)
	assert.Equal(t, 1, pr.Int("attempt"))
}

// TestEveryRouteCoolingWaitsWithADeadline: A is rate-limited until 1060, B and C are
// unfunded: the wait's deadline is 1060, one judgment names all three, and at 1060 the
// card is dealt on A (tla/ProviderFallback.tla, WaitHasDeadline).
func TestEveryRouteCoolingWaitsWithADeadline(t *testing.T) {
	t.Parallel()
	w := fallbackWorld(t)
	w.s.Providers["pb"] = Provider{Name: "pb", Funded: false}
	w.s.Providers["pc"] = Provider{Name: "pc", Funded: false}
	w.s.Fleet.Put(&Card{ID: "bk", Row: "m1", Col: Withdrawn, Fields: map[string]string{
		FieldRoute: "pro-a",
		FieldRateTake + "1": ProviderTake{Route: "pro-a", Finished: stamp(w.s.Now),
			Error: "provider: class=rate-limited status=429 msg=slow down; retry after 60 s"}.String(),
	}})

	wait, ok := w.s.ProviderWaitFor(cardhdr.RoutePro)
	require.True(t, ok, "every route of the tier is cooling")
	assert.Equal(t, time.Unix(1060, 0).UTC(), wait.Deadline, "the earliest cooling end")
	for _, want := range []string{"pa rate-limited until", "pb unfunded (the owner's to fund)", "pc unfunded (the owner's to fund)"} {
		assert.Contains(t, wait.Judgment, want)
	}
	assert.Equal(t, "tier pro has no route in line: ", wait.Judgment[:len("tier pro has no route in line: ")])

	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Len(t, p.Refused, 1, "the card waits, never a deal onto a cooling provider: %+v", p)
	assert.Equal(t, Ready, w.state("s1-1"))

	w.s.Now = time.Unix(1060, 0).UTC()
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wcID := w.s.Work.Card("s1-1").F("work")
	require.NotEmpty(t, wcID)
	assert.Equal(t, "pro-a", w.s.Fleet.Card(wcID).F(FieldRoute), "at its cooling end A serves again")
}

// TestUnfundedProviderNeverInLine: a provider whose row is funded=false is out of the
// line, and no deal draws its routes (tla/ProviderFallback.tla, NoDealToCooling).
func TestUnfundedProviderNeverInLine(t *testing.T) {
	t.Parallel()
	w := fallbackWorld(t)
	w.s.Providers["pb"] = Provider{Name: "pb", Funded: false}
	w.s.Providers["pc"] = Provider{Name: "pc", Funded: false}
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
	for _, c := range w.s.Fleet.Column(Ready) {
		if c.F("kind") == "work" {
			assert.Equal(t, "pro-a", c.F(FieldRoute), "an unfunded provider's route is never drawn")
		}
	}
	for _, name := range []string{"pro-b", "pro-c"} {
		_, ok := w.s.servedRoute(cardhdr.RoutePro, "", name, true)
		assert.False(t, ok, "%s is out of the line", name)
	}
}

// TestCoolingIsPerProvider: A has two routes in the tier; one fault on A takes both out
// of the line, and the next deal draws B (tla/ProviderFallback.tla, NoDealToCooling).
func TestCoolingIsPerProvider(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Now = time.Unix(1000, 0).UTC()
	w.s.Routes = []Route{
		{Name: "pro-a1", Tier: cardhdr.RoutePro, Provider: "pa", Model: "a", Deadline: 600, Enabled: true},
		{Name: "pro-a2", Tier: cardhdr.RoutePro, Provider: "pa", Model: "a2", Deadline: 600, Enabled: true},
		{Name: "pro-b", Tier: cardhdr.RoutePro, Provider: "pb", Model: "b", Deadline: 600, Enabled: true},
	}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-a1", "pro-a2", "pro-b"}}
	w.s.Providers = map[string]Provider{"pa": {Funded: true}, "pb": {Funded: true}}
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wcID := w.s.Work.Card("s1-1").F("work")
	require.Equal(t, "pro-a1", w.s.Fleet.Card(wcID).F(FieldRoute))
	member := w.s.Fleet.Card(wcID).Row
	w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wcID}}, Gens: w.gens(wcID)}))
	w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wcID}}, Gens: w.gens(wcID), Failed: true, Report: fallback429}))
	assert.Equal(t, "pro-b", w.s.Fleet.Card(wcID).F(FieldRoute), "the fault falls back past A's every route")
	_, a2 := w.s.servedRoute(cardhdr.RoutePro, "", "pro-a2", true)
	assert.False(t, a2, "one fault takes both routes of A out")
	_, b := w.s.servedRoute(cardhdr.RoutePro, "", "pro-b", true)
	assert.True(t, b)

	t.Run("reversed witness: the line ignoring cooling", func(t *testing.T) {
		w.s.Fleet.SetProp(seamIgnoreCooling, "1")
		_, a2 := w.s.servedRoute(cardhdr.RoutePro, "", "pro-a2", true)
		assert.True(t, a2, "with the line ignoring cooling A's second route would still be drawn")
	})
}

func TestProviderFaultKindAndRetryWords(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ProviderFaultRate, ProviderFaultKind("provider failure: provider: class=rate-limited status=429 msg=x"))
	assert.Equal(t, ProviderFault5xx, ProviderFaultKind("provider: class=provider-5xx status=503 msg=x"))
	assert.Equal(t, ProviderFaultTimeout, ProviderFaultKind("provider: class=timeout status=- msg=x"))
	assert.Equal(t, ProviderFaultCredit, ProviderFaultKind("provider: class=out-of-credit status=402 msg=x"))
	assert.Empty(t, ProviderFaultKind("tests red"))
	assert.Equal(t, 120*time.Second, retryAfter("provider: class=rate-limited status=429 msg=retry after 120 s"))
	assert.Equal(t, 90*time.Second, retryAfter("Retry-After: 90"))
	assert.Equal(t, 45*time.Second, retryAfter("try again in 45s"))
	assert.Zero(t, retryAfter("too many requests"))
}

func TestProviderWaitJudgmentIsOneLine(t *testing.T) {
	t.Parallel()
	w := fallbackWorld(t)
	w.s.Providers["pb"] = Provider{Name: "pb", Funded: false}
	w.s.Providers["pc"] = Provider{Name: "pc", Funded: false}
	w.s.Fleet.Put(&Card{ID: "bk", Row: "m1", Col: Withdrawn, Fields: map[string]string{
		FieldRoute:          "pro-a",
		FieldRateTake + "1": ProviderTake{Route: "pro-a", Finished: stamp(w.s.Now), Error: "provider: class=rate-limited status=429 msg=slow"}.String(),
	}})
	wait, ok := w.s.ProviderWaitFor(cardhdr.RoutePro)
	require.True(t, ok)
	assert.Equal(t, 1, strings.Count(wait.Judgment, "tier pro has no route in line:"))
}
