package store

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// A pinned card goes only to a worker that can serve its pin (internal/sprint/pin_serve.go;
// tla/PinServe.tla), on the mem twin. 2026-10-11: a brief pinned `model:
// anthropic/claude-fable-5-1`, the deal sent it to four members in 22 seconds, its whole
// redeal bound, each refused `class=auth` for want of an Anthropic key, and a judgment
// blamed the card.

// pinAuthLine is a member's report of a pin's take the provider refused for its key.
const pinAuthLine = cardhdr.EndProvider + ": provider: class=auth status=401 msg=invalid x-api-key (Anthropic API key)"

// pinned is a brief pinned to the model.
func pinned(model string) string {
	return briefOf("heavy", "model: "+model+"\ntokens: 5000\ndeadline: 600")
}

// The control: a pin whose provider has an enabled route (so the fleet holds its key) is
// dealt as usual, on the pin.
func TestAPinnedCardAMemberCanServeIsDealtAsUsual(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, pinned("prov-flash-a/model-big"))
	h.startMachine()
	h.machine()
	wc := h.workCards()["s1-1.w1"]
	require.NotNil(t, wc, "dealt")
	assert.Equal(t, sprint.RoutePin, wc.F(sprint.FieldRoute))
	assert.Equal(t, "prov-flash-a/model-big", wc.F(sprint.FieldModel))
	assert.Empty(t, h.openOf(sprint.NNoRoute), "no judgment")
}

// The deal never offers a pinned card to a member that cannot serve it: no member when no
// enabled route names the pin's provider; and, after one member's key was refused on a pin
// of the provider, only the other members.
func TestAPinnedCardIsNeverDealtToAMemberWithoutItsProvider(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, pinned("anthropic/claude-fable-5-1"))
	h.addReady("s2", 1, pinned("prov-flash-a/model-big"))
	h.startMachine()
	h.machine()
	cards := h.workCards()
	assert.NotContains(t, cards, "s1-1.w1", "no member holds a key for anthropic: never dealt")
	assert.Equal(t, sprint.Ready, h.snap().Work.Card("s1-1").Col)
	res := h.run(DealStep(sprint.DealReq{}))
	assert.Contains(t, refusedWhys(res), "no worker can serve pin anthropic/claude-fable-5-1", "the deal verb says why")

	wc := cards["s2-1.w1"]
	require.NotNil(t, wc)
	refusedBy := wc.Row
	h.failTake("s2-1.w1", pinAuthLine)
	h.machine()
	wc = h.snap().Fleet.Card("s2-1.w1")
	require.NotNil(t, wc)
	assert.NotEqual(t, refusedBy, wc.Row, "redealt to a member whose key was not refused")
	assert.Equal(t, sprint.Ready, wc.Col)

	// the rebalance and the level never move it back to the refused member
	for range 3 {
		h.machine()
		assert.NotEqual(t, refusedBy, h.snap().Fleet.Card("s2-1.w1").Row)
	}
}

// A pinned card no member up can serve waits ready, burns no redeal, and the tick raises
// ONE judgment naming the pin, never one a tick; it closes when a member can serve it.
func TestAPinnedCardNoOneCanServeWaitsWithOneJudgment(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 2, pinned("anthropic/claude-fable-5-1"))
	h.startMachine()
	for range 3 {
		h.machine()
	}
	for _, id := range []string{"s1-1", "s1-2"} {
		assert.Equal(t, sprint.Ready, h.snap().Work.Card(id).Col, "%s waits ready", id)
		assert.NotContains(t, h.workCards(), id+".w1", "%s is never dealt", id)
	}
	open := h.openOf(sprint.NNoRoute)
	require.Len(t, open, 1, "one judgment of the pin, however many cards wait")
	assert.Equal(t, "pin:anthropic/claude-fable-5-1", open[0].Note.Stream)
	assert.Contains(t, open[0].Note.What, "no worker can serve pin anthropic/claude-fable-5-1")
	assert.ElementsMatch(t, []string{"s1-1", "s1-2"}, open[0].Note.Primaries)
	assert.Equal(t, 1, h.written(sprint.NNoRoute), "written once, not every tick")
	assert.Empty(t, h.openOf(sprint.NBound), "never the card's bound")

	// a route of the provider (its key on the fleet) serves it: dealt, and the judgment closes
	r := route("heavy-a", "heavy")
	r.Provider = "anthropic"
	h.m.SetRoutes([]sprint.Route{route("flash-a", "flash"), r})
	h.machine()
	h.machine()
	assert.Contains(t, h.workCards(), "s1-1.w1", "dealt once served")
	assert.Empty(t, h.openOf(sprint.NNoRoute), "the judgment closes")
}

// A provider's refusal of a pin's take for its key is a coordination failure: the work card
// is withdrawn with no take ended, so no redeal is spent and no fail mark is written; the
// pin rests for that member; with every member refused it waits under the pin's judgment,
// never at the card's bound.
func TestAProviderAuthFailureOnAPinNeverSpendsTheCardsRedeals(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, pinned("prov-flash-a/model-big"))
	h.startMachine()
	h.machine()
	members := map[string]bool{}
	for i := range 2 {
		wc := h.snap().Fleet.Card("s1-1.w1")
		require.NotNil(t, wc, "take %d dealt", i+1)
		require.Equal(t, sprint.Ready, wc.Col, "take %d dealt", i+1)
		members[wc.Row] = true
		h.failTake("s1-1.w1", pinAuthLine)
		wc = h.snap().Fleet.Card("s1-1.w1")
		assert.Equal(t, sprint.Withdrawn, wc.Col)
		assert.Empty(t, wc.F(sprint.FieldTakeEnded), "no take ended: no redeal spent")
		assert.Empty(t, wc.F("ok"), "no fail mark on the member")
		assert.Equal(t, 0, wc.Int("redeals"))
		assert.NotEmpty(t, wc.F(fmt.Sprintf("pin_take_%d", i+1)), "the refusal's record rests the pin for that member")
		pr := h.snap().Work.Card("s1-1")
		assert.Equal(t, sprint.Ready, pr.Col)
		assert.Equal(t, 0, pr.Int("failed"))
		h.machine()
	}
	assert.Len(t, members, 2, "the second take went to the other member")
	assert.Equal(t, sprint.Withdrawn, h.snap().Fleet.Card("s1-1.w1").Col, "both members refused: not dealt again")
	assert.Equal(t, sprint.Ready, h.snap().Work.Card("s1-1").Col, "it waits ready")
	assert.Empty(t, h.openOf(sprint.NBound), "never the card's bound")
	open := h.openOf(sprint.NNoRoute)
	require.Len(t, open, 1)
	assert.Equal(t, "pin:prov-flash-a/model-big", open[0].Note.Stream)

	// the member's pin rest ends by itself: dealt again, still at no redeal spent
	h.tick(sprint.RouteRestFor)
	h.machine()
	wc := h.snap().Fleet.Card("s1-1.w1")
	assert.Equal(t, sprint.Ready, wc.Col, "dealt again once the rest ends")
	assert.Equal(t, 0, wc.Int("redeals"))
}

// refusedWhys is every refusal's why of a step's result, joined.
func refusedWhys(res Result) string {
	var out string
	for _, r := range res.Refused {
		out += r.Why + "\n"
	}
	return out
}
