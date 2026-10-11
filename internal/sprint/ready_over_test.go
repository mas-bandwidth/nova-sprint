package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// A card that sits in ready past the ready bound (ReadyMax, default one minute) raises one
// judgment per tier, with the reason the deal leaves it (a-card-ready-over-a-minute-pushes-the-seat).

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
