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
// sentence (coordinator finding 1; tla/ProviderBudget.tla, OnePerCause).
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
// grouping every card that shares it (coordinator finding 2; tla/ProviderBudget.tla, OnePerCause).
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

func TestAReadyCardBlockedOnlyOnRoomRaisesFleetUp(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 1}))
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
