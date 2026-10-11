package store

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// A pinned card goes only to a worker that can serve its pin (internal/sprint/pin_serve.go;
// tla/RouteIndex.tla, THE PIN), on the mem twin. 2026-10-11: a brief pinned `model:
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
		row := wc.Row
		h.failTake("s1-1.w1", pinAuthLine)
		wc = h.snap().Fleet.Card("s1-1.w1")
		assert.Equal(t, sprint.Withdrawn, wc.Col)
		assert.Empty(t, wc.F(sprint.FieldTakeEnded), "no take ended: no redeal spent")
		assert.Empty(t, wc.F("ok"), "no fail mark on the member")
		assert.Equal(t, 0, wc.Int("redeals"))
		assert.NotEmpty(t, wc.F("pin_take_"+row), "the refusal's record rests the pin for that member")
		assert.Equal(t, "1", wc.F("pin_refusals_"+row))
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

// In a store with no route nothing is known of keys: a pin's refusal ends its take as any
// provider failure does, spending a redeal, so the redeal bound and its judgment end it.
// Never a loop of refusals that spend nothing.
func TestAPinRefusalInAStoreWithNoRouteSpendsARedealAndEnds(t *testing.T) {
	t.Parallel()
	h := routeHarness(t)
	h.addReady("s1", 1, pinned("anthropic/claude-fable-5-1"))
	h.startMachine()
	h.machine()
	refusals := 0
	for range 2 * sprint.MaxRedeals {
		wc := h.snap().Fleet.Card("s1-1.w1")
		require.NotNil(t, wc)
		if wc.Col != sprint.Ready {
			break
		}
		h.failTake("s1-1.w1", pinAuthLine)
		refusals++
		assert.NotEmpty(t, h.snap().Fleet.Card("s1-1.w1").F(sprint.FieldTakeEnded), "refusal %d ends the take: a redeal is spent", refusals)
		h.machine()
	}
	assert.Equal(t, sprint.MaxRedeals+1, refusals, "the redeal bound ends it")
	assert.Equal(t, sprint.Withdrawn, h.snap().Fleet.Card("s1-1.w1").Col, "not dealt again")
	assert.Len(t, h.openOf(sprint.NBound), 1, "the bound's one judgment names it")
}

// A pin refused on every member, again and again: each member keeps ONE record (latest
// wins, with its count), and after MaxPinRefusals rests a member's rest stands, so the card
// waits under one standing judgment that does not reopen while nothing changes. Bringing a
// member up again ends its rest and the card is dealt to it.
func TestAPinRefusedOnEveryMemberStandsUnderOneJudgment(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, pinned("prov-flash-a/model-big"))
	h.startMachine()
	h.machine()
	refusals := 0
	for range 40 {
		wc := h.snap().Fleet.Card("s1-1.w1")
		require.NotNil(t, wc)
		if wc.Col == sprint.Ready {
			h.failTake("s1-1.w1", pinAuthLine)
			refusals++
		} else {
			h.tick(sprint.RouteRestFor)
		}
		h.machine()
	}
	assert.Equal(t, 2*sprint.MaxPinRefusals, refusals, "each member is refused MaxPinRefusals times, then its rest stands")
	wc := h.snap().Fleet.Card("s1-1.w1")
	assert.Equal(t, sprint.Withdrawn, wc.Col)
	assert.Equal(t, 0, wc.Int("redeals"))
	records := 0
	for k := range wc.Fields {
		if strings.HasPrefix(k, sprint.FieldPinTake) {
			records++
		}
	}
	assert.Equal(t, 2, records, "one record per member, never one per refusal")
	assert.Equal(t, "3", wc.F("pin_refusals_m1"))
	open := h.openOf(sprint.NNoRoute)
	require.Len(t, open, 1, "one standing judgment")
	res := h.run(DealStep(sprint.DealReq{}))
	assert.Contains(t, refusedWhys(res), "until it is brought up again", "both members' rests stand")
	written := h.written(sprint.NNoRoute)
	for range 4 {
		h.tick(sprint.RouteRestFor)
		h.machine()
	}
	assert.Equal(t, written, h.written(sprint.NNoRoute), "the standing judgment does not reopen")
	assert.Len(t, h.openOf(sprint.NNoRoute), 1)

	// m1 brought up again (its key fixed): its rest ends and the card is dealt to it
	h.must(FleetStep(sprint.FleetReq{Op: "down", Member: "m1"}))
	h.tick(time.Minute)
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: sprint.MaxWidth}))
	h.machine()
	wc = h.snap().Fleet.Card("s1-1.w1")
	assert.Equal(t, sprint.Ready, wc.Col, "dealt again")
	assert.Equal(t, "m1", wc.Row)
}

// A member brought up again starts afresh, as tla/RouteIndex.tla's MemberUp resets its count:
// three refusals make its rest stand, it comes up, one more refusal rests it for a time only,
// never standing, so the card is dealt to it again once that rest ends.
func TestAPinRefusalCountRestartsWhenTheMemberComesUp(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, pinned("prov-flash-a/model-big"))
	h.startMachine()
	h.machine()
	for range 40 {
		if wc := h.snap().Fleet.Card("s1-1.w1"); wc.Col == sprint.Ready {
			h.failTake("s1-1.w1", pinAuthLine)
		} else {
			h.tick(sprint.RouteRestFor)
		}
		h.machine()
	}
	require.Equal(t, "3", h.snap().Fleet.Card("s1-1.w1").F("pin_refusals_m1"), "m1's rest stands")
	require.Contains(t, refusedWhys(h.run(DealStep(sprint.DealReq{}))), "until it is brought up again")

	h.must(FleetStep(sprint.FleetReq{Op: "down", Member: "m1"}))
	h.tick(time.Minute)
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: sprint.MaxWidth}))
	h.machine()
	wc := h.snap().Fleet.Card("s1-1.w1")
	require.Equal(t, "m1", wc.Row, "dealt to m1 once it is up")
	h.tick(time.Minute)
	h.failTake("s1-1.w1", pinAuthLine)
	assert.Equal(t, "1", h.snap().Fleet.Card("s1-1.w1").F("pin_refusals_m1"), "the count restarts after the member came up")
	h.machine()
	h.tick(sprint.RouteRestFor)
	h.machine()
	wc = h.snap().Fleet.Card("s1-1.w1")
	assert.Equal(t, sprint.Ready, wc.Col, "one refusal after the up rests m1 for a time, never standing")
	assert.Equal(t, "m1", wc.Row)
}

// A refusal and a member's coming up in the same second are still ordered (the member's count
// of comings up, never stamps in seconds, which tie): a refusal then a down and up in that
// second resets the member's rest and count; an up then a refusal in that second counts and
// rests it (Stella's third read of #107).
func TestARefusalAndAnUpInTheSameSecondAreOrdered(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m2", Who: "coordinator"})) // m1 alone serves it (a hold stays through beats)
	h.addReady("s1", 1, pinned("prov-flash-a/model-big"))
	h.startMachine()
	h.machine()
	require.Equal(t, "m1", h.snap().Fleet.Card("s1-1.w1").Row)
	h.tick(time.Minute) // m1 came up a minute ago; all that follows is in one second

	// refusal, then down and up, all in one second: the up resets m1's rest
	h.failTake("s1-1.w1", pinAuthLine)
	h.must(FleetStep(sprint.FleetReq{Op: "down", Member: "m1"}))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: sprint.MaxWidth}))
	h.machine()
	wc := h.snap().Fleet.Card("s1-1.w1")
	require.Equal(t, sprint.Ready, wc.Col, "the up after the refusal ends m1's rest, in the same second")
	require.Equal(t, "m1", wc.Row)

	// up, then a refusal in that same second: it counts afresh and rests m1
	h.failTake("s1-1.w1", pinAuthLine)
	assert.Equal(t, "1", h.snap().Fleet.Card("s1-1.w1").F("pin_refusals_m1"), "the count restarted at the up")
	h.machine()
	assert.NotEqual(t, sprint.Ready, h.snap().Fleet.Card("s1-1.w1").Col, "the refusal after the up rests m1")
	open := h.openOf(sprint.NNoRoute)
	require.Len(t, open, 1)
	assert.Equal(t, "pin:prov-flash-a/model-big", open[0].Note.Stream)
}

// Rests written while routes were held stay when every route is removed: the deal, the
// withdrawal and the take ask the same rests, so the pin is neither dealt to a member a take
// would refuse nor left there, and its one judgment names it (Stella's second read of #107).
func TestRetainedPinRestsAfterTheRoutesGoNeverStrandAPin(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, pinned("prov-flash-a/model-big"))
	h.startMachine()
	h.machine()
	for range 40 {
		if wc := h.snap().Fleet.Card("s1-1.w1"); wc.Col == sprint.Ready {
			h.failTake("s1-1.w1", pinAuthLine)
		} else {
			h.tick(sprint.RouteRestFor)
		}
		h.machine()
	}
	require.Contains(t, refusedWhys(h.run(DealStep(sprint.DealReq{}))), "until it is brought up again", "both members' rests stand")

	h.m.SetRoutes(nil) // every route removed: the rests stay in the fleet table
	h.machine()
	h.machine()
	wc := h.snap().Fleet.Card("s1-1.w1")
	assert.NotEqual(t, sprint.Ready, wc.Col, "never dealt to a member whose pins rest (a take would refuse it there for ever)")
	open := h.openOf(sprint.NNoRoute)
	require.Len(t, open, 1, "the pin's one judgment names it")
	assert.Equal(t, "pin:prov-flash-a/model-big", open[0].Note.Stream)

	// a member brought up again serves it, routes or none
	h.must(FleetStep(sprint.FleetReq{Op: "down", Member: "m2"}))
	h.tick(time.Minute)
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: sprint.MaxWidth}))
	h.machine()
	wc = h.snap().Fleet.Card("s1-1.w1")
	assert.Equal(t, sprint.Ready, wc.Col)
	assert.Equal(t, "m2", wc.Row)
	h.must(TakeStep(sprint.TakeReq{As: "m2", Sel: sprint.Sel{Limit: 10}, Who: "m2"}))
	assert.Equal(t, sprint.Working, h.snap().Fleet.Card("s1-1.w1").Col, "the take on m2 takes it")
}

// A pinned card already ready on a member whose pins of its provider now rest is never
// taken there (a take by id is refused naming the pin), and the tick withdraws it, with no
// redeal spent, for a member that serves it.
func TestAPinnedCardReadyOnARestingMemberIsNotTakenAndIsWithdrawn(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 4, pinned("prov-flash-a/model-big"))
	h.startMachine()
	h.machine()
	first := h.snap().Fleet.Card("s1-1.w1")
	require.NotNil(t, first)
	x := first.Row
	var other *sprint.Card
	for _, c := range h.snap().Fleet.Cell(x, sprint.Ready) {
		if c.ID != first.ID {
			other = c
		}
	}
	require.NotNil(t, other, "a second pinned card ready on %s", x)
	h.failTake(first.ID, pinAuthLine)
	res := h.run(TakeStep(sprint.TakeReq{As: x, Sel: sprint.Sel{IDs: []string{other.ID}}, Gens: map[string]int{other.ID: other.Int("gen")}, Who: x}))
	assert.Contains(t, refusedWhys(res), "its pin prov-flash-a/model-big is not served on "+x, "a take by id is refused")
	// a take by count passes over every pinned card of the provider ready on x
	h.run(TakeStep(sprint.TakeReq{As: x, Sel: sprint.Sel{Limit: 100}, Who: x}))
	for _, c := range h.snap().Fleet.Column(sprint.Working) {
		assert.NotEqual(t, x, c.Row, "a take by count never takes %s on %s", c.ID, x)
	}
	h.machine()
	h.machine()
	moved := h.snap().Fleet.Card(other.ID)
	assert.Equal(t, sprint.Ready, moved.Col)
	assert.NotEqual(t, x, moved.Row, "withdrawn from the resting member and dealt to one that serves it")
	assert.Equal(t, 0, moved.Int("redeals"))
}

// refusedWhys is every refusal's why of a step's result, joined.
func refusedWhys(res Result) string {
	var out string
	for _, r := range res.Refused {
		out += r.Why + "\n"
	}
	return out
}
