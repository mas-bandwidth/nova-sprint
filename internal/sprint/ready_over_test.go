package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// A card that sits in ready past the ready bound (ReadyMax, default one minute) raises one
// judgment per (reason, tier), with the reason the deal leaves it (a-card-ready-over-a-minute-pushes-the-seat).

func TestAReadyCardPastItsBoundWithNoRouteRaisesTheJudgment(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: false}}
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	require.NotEmpty(t, p.Notes, "a card ready past its bound with no route raises nothing")
	w.do(p)
	ns := w.notesOf(NReadyOver)
	require.Len(t, ns, 1, "one judgment, not one a tick: %v", ns)
	assert.Equal(t, []string{"s1-1"}, ns[0].Primaries)
	assert.Contains(t, ns[0].What, "tier pro")
	assert.Contains(t, ns[0].What, "pro-a: disabled", "the routes are listed with why")
}

// A card ready past its bound whose tier is served only by a resting route raises the
// judgment, and the route it names as resting is one the deal does not draw: the rests are
// the deal's settled rests (ready_over.go, withRests), the ones due this tick included
// (tla/ReadyOver.tla, OnePerCause: a card blocked only by rests is judged).
func TestAReadyCardBehindARestingRouteRaisesTheJudgment(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	w.s.Fleet.SetProp(PropProviderRest("p"), RouteRest{At: w.s.Now.Add(-time.Hour), Until: OpenUntil, Cause: RestAuth, Cards: []string{"x"}}.value())
	w.tick(61 * time.Second)
	// the deal does not draw the resting route: the card stays ready
	d, _ := TickDeal(w.s, TickReq{})
	w.do(d)
	require.Equal(t, Ready, w.state("s1-1"), "the deal drew a card whose only route rests")
	p, _ := TickReadyOver(w.s, TickReq{})
	require.NotEmpty(t, p.Notes, "a card ready past its bound behind a resting route raises nothing")
	w.do(p)
	ns := w.notesOf(NReadyOver)
	require.Len(t, ns, 1, "one judgment, not %d: %v", len(ns), ns)
	assert.Equal(t, []string{"s1-1"}, ns[0].Primaries)
	assert.Contains(t, ns[0].What, "pro-a: resting until woken", "the judgment names the rest the deal settled")
}

// A rest that ended before now is not the deal's: the route serves again and the deal deals
// the card, so the ready-over part names no route and raises nothing (ready_over.go,
// withRests: only the rests Resting at s.Now are settled).
func TestAReadyCardBehindARestThatEndedIsNotJudged(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	w.s.Fleet.SetProp(PropProviderRest("p"), RouteRest{At: w.s.Now.Add(-2 * time.Hour), Until: w.s.Now.Add(-time.Hour), Cause: RestAuth, Cards: []string{"x"}}.value())
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	assert.Empty(t, p.Notes, "a rest that ended before now is not the deal's: the route serves and the card is dealt")
}

func TestTwoCardsWithTheSameCauseRaiseOneJudgment(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: false}}
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	w.do(p)
	ns := w.notesOf(NReadyOver)
	require.Len(t, ns, 1, "two cards, one cause: one judgment, not %d", len(ns))
	assert.ElementsMatch(t, []string{"s1-1", "s1-2"}, ns[0].Primaries)
}

func TestACardDealtClearsTheJudgment(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: false}}
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	w.do(p)
	require.Len(t, w.notesOf(NReadyOver), 1)
	// the route is enabled and the card is dealt: the judgment clears itself the tick
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.Equal(t, Working, w.state("s1-1"))
	p2, _ := TickReadyOver(w.s, TickReq{})
	w.do(p2)
	for _, o := range w.s.Open {
		assert.NotEqual(t, NReadyOver, o.Note.Type, "the dealt card's judgment stays open: %s", o.Note.What)
	}
}

func TestAReadyCardUnderItsBoundRaisesNothing(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: false}}
	w.tick(59 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	assert.Empty(t, p.Notes, "a card ready for 59s raises a judgment")
}

func TestTwoCausesOfOneTierRaiseTwoJudgments(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: false}}
	// s1-2 is held, s1-1's tier is unserved: two different causes of one tier.
	w.s.Work.Card("s1-2").Fields[FieldHeld] = "coordinator"
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	require.NotEmpty(t, p.Notes, "two causes of one tier raise nothing")
	w.do(p)
	ns := w.notesOf(NReadyOver)
	require.Len(t, ns, 2, "two causes of one tier raise one judgment, not %d", len(ns))
	var held, unserved bool
	for _, n := range ns {
		if strings.Contains(n.What, "held by the coordinator") {
			held = true
			assert.Equal(t, []string{"s1-2"}, n.Primaries)
		}
		if strings.Contains(n.What, "no serving route") {
			unserved = true
			assert.Equal(t, []string{"s1-1"}, n.Primaries)
		}
	}
	assert.True(t, held && unserved, "the causes are not split: %v", ns)
}

// A brief whose model lines do not read is the card's own cause, never the tier's routes:
// it and an unserved tier of the same tier are two judgments, never merged under the route
// sentence (coordinator finding 1; tla/ReadyOver.tla, OnePerCause).
func TestAModelLinesCardAndAnUnservedTierRaiseTwoJudgments(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: false}}
	// s1-2's brief model lines do not read; s1-1's tier pro is unserved.
	w.s.Work.Card("s1-2").Fields["brief"] = "tier: pro\nmodel: xyz\n"
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	require.NotEmpty(t, p.Notes, "a model-lines card and an unserved tier raise nothing")
	w.do(p)
	ns := w.notesOf(NReadyOver)
	require.Len(t, ns, 2, "two distinct causes of one tier raise one judgment, not %d", len(ns))
	var modelLines, unserved bool
	for _, n := range ns {
		if strings.Contains(n.What, "its brief's model lines") {
			modelLines = true
			assert.Equal(t, []string{"s1-2"}, n.Primaries, "the model-lines card is the card's own cause")
		}
		if strings.Contains(n.What, "no serving route") {
			unserved = true
			assert.Equal(t, []string{"s1-1"}, n.Primaries)
		}
	}
	assert.True(t, modelLines && unserved, "the model-lines cause is merged into the tier's: %v", ns)
}

// A group that gains cards of the same cause is rewritten in place, so the judgment keeps
// grouping every card that shares it (coordinator finding 2; tla/ReadyOver.tla, OnePerCause).
func TestAGroupRefreshesAsCardsJoin(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: false}}
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	w.do(p)
	require.Len(t, w.notesOf(NReadyOver), 1)
	// two more cards of the same cause join ready and pass the bound
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 2}))
	w.tick(61 * time.Second)
	p2, _ := TickReadyOver(w.s, TickReq{})
	require.Empty(t, p2.Notes, "the cause is already judged: no new note")
	w.do(p2)
	var updated []Note
	for _, u := range p2.Updates {
		if u.Type == NReadyOver {
			updated = append(updated, u)
		}
	}
	require.Len(t, updated, 1, "the grouped judgment refreshes as cards join")
	assert.Contains(t, updated[0].What, "3 primaries of tier pro", "the refreshed judgment names all three")
	assert.ElementsMatch(t, []string{"s1-1", "s1-2", "s1-3"}, updated[0].Primaries)
	require.Len(t, w.notesOf(NReadyOver), 1, "one judgment per cause, never one per card")
}

// A ready-over judgment is keyed on the cause, never the figures the cause names: the
// balance moves while the card sits ready, one judgment, rewritten in place with the
// latest balance (tla/ReadyOver.tla, OnePerCause).
func TestAReadyOverJudgmentStaysOneAsTheBalanceMoves(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	// pro-a is enabled and not resting, but the tier's applied array leaves it out, so
	// the deal draws no route of the tier and routeWhys names it: out of budget.
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-b"}}
	w.s.Fleet.SetProp(PropProviderBalance("p"), ProviderBalance{Known: true, Balance: -1, At: w.s.Now}.value())
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	require.NotEmpty(t, p.Notes, "a ready card out of budget past its bound raises nothing")
	w.do(p)
	open := func() []Note {
		var out []Note
		for _, o := range w.s.Open {
			if o.Note.Type == NReadyOver {
				out = append(out, o.Note)
			}
		}
		return out
	}
	ns := open()
	require.Len(t, ns, 1, "one cause: one judgment, not %d", len(ns))
	require.Contains(t, ns[0].What, "-$1.00", "the reason names the balance")
	// the balance moves while the cause stands: one judgment, rewritten in place
	w.s.Fleet.SetProp(PropProviderBalance("p"), ProviderBalance{Known: true, Balance: -2, At: w.s.Now}.value())
	p2, _ := TickReadyOver(w.s, TickReq{})
	require.Empty(t, p2.Closes, "the balance's move closed the judgment")
	require.Empty(t, p2.Notes, "the balance's move wrote a second judgment")
	w.do(p2)
	ns = open()
	require.Len(t, ns, 1, "one open judgment, not %d: %v", len(ns), ns)
	require.Contains(t, ns[0].What, "-$2.00", "the judgment is rewritten with the latest balance")
}

// A rest whose end moves while the cause stands is one judgment, rewritten in place
// (tla/ReadyOver.tla, OnePerCause).
func TestAReadyOverJudgmentStaysOneAsTheRestEndMoves(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	until1 := w.s.Now.Add(30 * time.Minute)
	w.s.Fleet.SetProp(PropProviderRest("p"), RouteRest{At: w.s.Now, Until: until1, Cause: RestCoordinator, Why: "rested by boss"}.value())
	w.tick(61 * time.Second)
	w.s.rests = nil
	w.s, _ = w.s.withRests()
	p, _ := TickReadyOver(w.s, TickReq{})
	require.NotEmpty(t, p.Notes, "a ready card whose tier rests past its bound raises nothing")
	w.do(p)
	open := func() []Note {
		var out []Note
		for _, o := range w.s.Open {
			if o.Note.Type == NReadyOver {
				out = append(out, o.Note)
			}
		}
		return out
	}
	ns := open()
	require.Len(t, ns, 1, "one cause: one judgment, not %d", len(ns))
	require.Contains(t, ns[0].What, "resting until "+stamp(until1), "the reason names the rest's end")
	// the provider's rest is extended: the cause stands, the judgment is rewritten in place
	until2 := w.s.Now.Add(60 * time.Minute)
	w.s.Fleet.SetProp(PropProviderRest("p"), RouteRest{At: w.s.Now, Until: until2, Cause: RestCoordinator, Why: "rested by boss"}.value())
	w.tick(1 * time.Second)
	w.s.rests = nil
	w.s, _ = w.s.withRests()
	p2, _ := TickReadyOver(w.s, TickReq{})
	require.Empty(t, p2.Closes, "the rest end's move closed the judgment")
	require.Empty(t, p2.Notes, "the rest end's move wrote a second judgment")
	w.do(p2)
	ns = open()
	require.Len(t, ns, 1, "one open judgment, not %d: %v", len(ns), ns)
	require.Contains(t, ns[0].What, "resting until "+stamp(until2), "the judgment is rewritten with the latest rest end")
}

// Two different causes of one tier are two judgments, never merged (tla/ReadyOver.tla,
// OnePerCause): here an out-of-budget route and a held card.
func TestAnOutOfBudgetCauseAndAHeldCauseRaiseTwoJudgments(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	w.s.Tiers = map[string][]string{cardhdr.RoutePro: {"pro-b"}}
	w.s.Fleet.SetProp(PropProviderBalance("p"), ProviderBalance{Known: true, Balance: -1, At: w.s.Now}.value())
	// s1-2 is held: a second cause of the same tier.
	w.s.Work.Card("s1-2").Fields[FieldHeld] = "coordinator"
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	require.NotEmpty(t, p.Notes, "two causes of one tier raise nothing")
	w.do(p)
	ns := w.notesOf(NReadyOver)
	require.Len(t, ns, 2, "two causes of one tier raise one judgment, not %d", len(ns))
	var held, budget bool
	for _, n := range ns {
		if strings.Contains(n.What, "held by the coordinator") {
			held = true
			assert.Equal(t, []string{"s1-2"}, n.Primaries)
		}
		if strings.Contains(n.What, "out of budget") {
			budget = true
			assert.Equal(t, []string{"s1-1"}, n.Primaries)
		}
	}
	assert.True(t, held && budget, "the causes are not split: %v", ns)
}

// The ready-over key is the cause, never its figures: the balance and a rest's end are
// dropped, while distinct causes stay distinct (tla/ReadyOver.tla, OnePerCause).
func TestReadyOverWhyDropsTheFiguresButKeepsTheCause(t *testing.T) {
	t.Parallel()
	budget := "1 primaries of tier pro sit in ready past 1m0s: tier pro has no serving route (pro-a: out of budget (balance -$1.00)): enable a route"
	assert.Equal(t, readyOverWhy(strings.Replace(budget, "-$1.00", "-$2.00", 1)), readyOverWhy(budget),
		"the balance figure is in the key")
	rest := "1 primaries of tier pro sit in ready past 1m0s: tier pro has no serving route (pro-a: resting until 2030-01-02T03:34:05Z: rested by boss): enable a route"
	assert.Equal(t, readyOverWhy(strings.Replace(rest, "2030-01-02T03:34:05Z", "2030-01-02T04:04:05Z", 1)), readyOverWhy(rest),
		"the rest end is in the key")
	disabled := "1 primaries of tier pro sit in ready past 1m0s: tier pro has no serving route (pro-a: disabled): enable a route"
	assert.NotEqual(t, readyOverWhy(budget), readyOverWhy(disabled), "two causes of one tier were merged")
}

func TestAReadyCardBlockedOnlyOnRoomRaisesFleetUp(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.setWidth("m1", 1)
	w.up("m1")
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 3}))
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	// fill m1's room (DealAhead times width 1 = 2): one working, one ready behind it
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	takeCard(w, "s1-1.w1")
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-2"}}}))
	require.Equal(t, 2, widthHeld(w, "m1"), "m1 is at its room")
	// s1-3 sits ready, blocked only on m1's room: the deal would deal it to a member
	// that can launch it, but none has free width
	w.tick(61 * time.Second)
	p, _ := TickReadyOver(w.s, TickReq{})
	require.NotEmpty(t, p.Notes, "a ready card blocked only on a member's room raises nothing")
	w.do(p)
	ns := w.notesOf(NReadyOver)
	require.Len(t, ns, 1, "the room cause raises one judgment, not %d", len(ns))
	assert.Equal(t, []string{"s1-3"}, ns[0].Primaries)
	assert.Contains(t, ns[0].What, "free width", "the judgment names the room")
	assert.Contains(t, ns[0].What, "fleet up", "the judgment names the remedy verb")
}
