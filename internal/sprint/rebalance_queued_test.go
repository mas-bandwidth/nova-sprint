package sprint

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The rebalance (docs/SPEC-SPRINT.md section 1, the rebalance; the owner, 2026-10-06: "This
// should be a holistic rebalance, not just across friends, not just across tiers, but BOTH").

// rbWorld is a world with the members given up at width 1, a flash and a pro route, and
// stream s1.
func rbWorld(t *testing.T, members ...string) *world {
	t.Helper()
	w := newWorld(t)
	for _, m := range members {
		w.setWidth(m, 1)
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m}))
	}
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "pr", Enabled: true},
	}
	w.s.Work.SetRows([]string{"s1"})
	return w
}

// rbPlace places primary id of the tier working in s1, its work card on the row in col.
func rbPlace(w *world, id, tier, row, col string, fields ...string) *Card {
	if !w.s.Fleet.HasRow(row) {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
	}
	n := float64(len(w.s.Work.Column(Working)) + 1)
	pr := &Card{ID: id, Row: "s1", Col: Working, Score: n, Rev: 1, Fields: map[string]string{
		"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief(tier), "work": WorkCardID(id, 1), FieldTierNow: tier}}
	wc := &Card{ID: WorkCardID(id, 1), Row: row, Col: col, Score: n, Rev: 1, Fields: map[string]string{
		"kind": "work", "primary": id, "stream": "s1", "attempt": "1", "gen": "1", "member": row}}
	for i := 0; i+1 < len(fields); i += 2 {
		if strings.HasPrefix(fields[i], "pr.") {
			pr.Fields[strings.TrimPrefix(fields[i], "pr.")] = fields[i+1]
		} else {
			wc.Fields[fields[i]] = fields[i+1]
		}
	}
	w.s.Work.Put(pr)
	w.s.Fleet.Put(wc)
	return wc
}

func rebalanced(p Plan) []string {
	var out []string
	for _, u := range p.Units {
		if strings.HasPrefix(u.Moved, "rebalanced ") {
			out = append(out, u.Moved)
		}
	}
	return out
}

func TestRebalanceIsTheWorkTablesPartAfterTheDeal(t *testing.T) {
	t.Parallel()
	var names []string
	for _, p := range TickTables[0].Parts {
		names = append(names, p.Name)
	}
	i := slices.Index(names, PartRebalance)
	require.GreaterOrEqual(t, i, 1, "the work table's update has the rebalance part")
	assert.Equal(t, "deal", names[i-1], "it runs on the deal applied")
}

// A flash card queued on a full flash friend moves to a member with an idle lane when the
// fleet's set admits flash; not when the set is pro only, nor with the fleet off.
func TestRebalanceMovesAFlashCardOffAFullFriendToAnIdleMember(t *testing.T) {
	t.Parallel()
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}}
	setup := func() (*world, *Card) {
		w := rbWorld(t, "m1")
		rbPlace(w, "s1-1", cardhdr.RouteFlash, FriendRow("amy"), Working)
		return w, rbPlace(w, "s1-2", cardhdr.RouteFlash, FriendRow("amy"), Ready)
	}

	w, wc := setup()
	p := w.must(Rebalance(w.s, []FriendSeat{amy}, "machine"))
	require.Equal(t, []string{"rebalanced s1-2.w1 from friend.amy to m1 gen=2 (its excess; a free lane takes it)"}, rebalanced(p))
	got := w.s.Fleet.Card(wc.ID)
	assert.Equal(t, "m1", got.Row)
	assert.Equal(t, Ready, got.Col)
	assert.Equal(t, "2", got.F("gen"), "its next generation: her inbox job is stale")
	assert.Equal(t, "1", got.F("attempt"), "its attempt kept")
	assert.Equal(t, "flash-a", got.F(FieldRoute), "a machine draws a route of its tier")
	assert.Equal(t, FriendRow("amy"), got.F(FieldRebalancedFrom))
	assert.Equal(t, "amy", got.F(FieldFriendsLeft))
	require.Len(t, p.Units[0].Notes, 1, "a happened note, no judgment")
	n := p.Units[0].Notes[0]
	assert.Equal(t, NRebalanced, n.Type)
	assert.Equal(t, Happened, n.Kind)
	assert.Equal(t, "rebalanced s1-2.w1 from friend.amy to m1", n.What)
	assert.Empty(t, p.Notes)

	w, _ = setup()
	w.s.Work.SetProp(PropFleetTiers, cardhdr.RoutePro)
	assert.Empty(t, rebalanced(Rebalance(w.s, []FriendSeat{amy}, "machine")), "the fleet's set is pro only")

	w, _ = setup()
	w.s.Work.SetProp(PropFleet, SwitchOff)
	assert.Empty(t, rebalanced(Rebalance(w.s, []FriendSeat{amy}, "machine")), "the fleet is off")
}

// A pro card queued on a full heavy friend moves to a pro friend with an idle lane, the
// cheapest before a heavy one, and never to a member whose fleet set is flash.
func TestRebalanceMovesAProCardToAProFriendNeverAFlashMember(t *testing.T) {
	t.Parallel()
	hal := FriendSeat{Name: "hal", Width: 1, Status: Up, Tiers: []string{cardhdr.RoutePro, cardhdr.RouteHeavy}}
	hugh := FriendSeat{Name: "hugh", Width: 3, Status: Up, Tiers: []string{cardhdr.RoutePro, cardhdr.RouteHeavy}}
	pam := FriendSeat{Name: "pam", Width: 1, Status: Up, Tiers: []string{cardhdr.RoutePro}}
	setup := func() (*world, *Card) {
		w := rbWorld(t, "m1")
		w.s.Work.SetProp(PropFleetTiers, cardhdr.RouteFlash)
		rbPlace(w, "s1-1", cardhdr.RoutePro, FriendRow("hal"), Working)
		return w, rbPlace(w, "s1-2", cardhdr.RoutePro, FriendRow("hal"), Ready, FieldRoute, "pro-a")
	}

	w, wc := setup()
	w.must(Rebalance(w.s, []FriendSeat{hal, hugh, pam}, "machine"))
	got := w.s.Fleet.Card(wc.ID)
	assert.Equal(t, FriendRow("pam"), got.Row, "the pro friend first, though the heavy one has more idle lanes")
	assert.Empty(t, got.F(FieldRoute), "a friend runs her own model")

	w, wc = setup()
	assert.Empty(t, rebalanced(Rebalance(w.s, []FriendSeat{hal}, "machine")), "never a flash member")
	assert.Equal(t, FriendRow("hal"), w.s.Fleet.Card(wc.ID).Row)
}

// A started card never moves: working, or ready and named running by her beat.
func TestRebalanceNeverMovesAStartedCard(t *testing.T) {
	t.Parallel()
	w := rbWorld(t, "m1")
	busy := rbPlace(w, "s1-1", cardhdr.RouteFlash, FriendRow("amy"), Working)
	queued := rbPlace(w, "s1-2", cardhdr.RouteFlash, FriendRow("amy"), Ready)
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}, Running: []string{queued.ID}}
	assert.Empty(t, rebalanced(Rebalance(w.s, []FriendSeat{amy}, "machine")))
	assert.Equal(t, FriendRow("amy"), w.s.Fleet.Card(busy.ID).Row)
}

// A pinned card never moves: a hard pin, a pin to the friend it sits on, WHO: friend off the
// friends, a model pin.
func TestRebalanceNeverMovesAPinnedCard(t *testing.T) {
	t.Parallel()
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}}
	for _, pin := range [][]string{
		{"pr." + FieldWho, "only." + FriendRow("amy")},
		{"pr." + FieldWho, FriendRow("amy")},
	} {
		w := rbWorld(t, "m1")
		rbPlace(w, "s1-1", cardhdr.RouteFlash, FriendRow("amy"), Working)
		rbPlace(w, "s1-2", cardhdr.RouteFlash, FriendRow("amy"), Ready, pin...)
		assert.Empty(t, rebalanced(Rebalance(w.s, []FriendSeat{amy}, "machine")), "%v", pin)
	}
	w := rbWorld(t, "m1")
	rbPlace(w, "s1-1", cardhdr.RouteFlash, FriendRow("amy"), Working)
	rbPlace(w, "s1-2", cardhdr.RouteFlash, FriendRow("amy"), Ready, "pr."+FieldWho, WhoFriend)
	assert.Empty(t, rebalanced(Rebalance(w.s, []FriendSeat{amy}, "machine")), "WHO: friend stays with the friends")

	w = rbWorld(t, "m1", "m2")
	rbPlace(w, "s1-1", cardhdr.RouteFlash, "m1", Working)
	rbPlace(w, "s1-2", cardhdr.RouteFlash, "m1", Ready, FieldRoute, RoutePin)
	assert.Empty(t, rebalanced(Rebalance(w.s, nil, "machine")), "a model pin")
}

// A card taken back from a unit, or rebalanced off it, never moves to it.
func TestRebalanceNeverMovesACardToAUnitItLeft(t *testing.T) {
	t.Parallel()
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}}
	bob := FriendSeat{Name: "bob", Width: 1, Status: Up, Tiers: []string{cardhdr.RouteFlash}}
	w := rbWorld(t)
	rbPlace(w, "s1-1", cardhdr.RouteFlash, FriendRow("amy"), Working)
	rbPlace(w, "s1-2", cardhdr.RouteFlash, FriendRow("amy"), Ready, FieldFriendsLeft, "bob")
	assert.Empty(t, rebalanced(Rebalance(w.s, []FriendSeat{amy, bob}, "machine")), "taken back from bob")

	w = rbWorld(t, "m1", "m2")
	rbPlace(w, "s1-1", cardhdr.RouteFlash, "m1", Working)
	rbPlace(w, "s1-2", cardhdr.RouteFlash, "m1", Ready, FieldRebalancedFrom, "m2")
	assert.Empty(t, rebalanced(Rebalance(w.s, nil, "machine")), "rebalanced off m2")
}

// A partly-idle unit is a source too: a unit whose lanes do not all work keeps its own
// start, but its ready work beyond the halves its working cards leave free (its excess)
// moves off it, never the whole queue held back. This is the source rule the old
// rebalance.go:103 `g.working < g.width` gate skipped.
func TestTheRebalanceMovesTheExcessWorkOfAPartlyIdleUnit(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.setWidth("m1", 4)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.setWidth("m2", 4)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true}}
	w.s.Work.SetRows([]string{"s1"})
	for i := 1; i <= 2; i++ { // m1's two lanes work, two are free: its excess is two of the four
		rbPlace(w, fmt.Sprintf("s1-w%d", i), cardhdr.RouteFlash, "m1", Working)
	}
	for i := 1; i <= 4; i++ {
		rbPlace(w, fmt.Sprintf("s1-q%d", i), cardhdr.RouteFlash, "m1", Ready)
	}
	p := w.must(Rebalance(w.s, nil, "machine"))
	assert.Len(t, rebalanced(p), 2, "the two excess work cards move off the partly-idle unit")
	assert.Equal(t, 2, w.s.Fleet.Count("m1", Ready), "m1 keeps only its two free lanes' worth of ready work")
	assert.Equal(t, 2, w.s.Fleet.Count("m2", Ready), "m2, with free lanes, took the excess")
}

// A unit with work, read and fix cards ready beyond its free lanes moves each kind to a
// unit with a free lane, none held back: a fix card (a rework attempt carrying the fix
// field, or a primary of priority fix) moves as a work card does, its attempt and fix kept;
// a read whose only unit with a free lane is its worker stays.
func TestTheRebalanceMovesWorkReadsAndFixAlike(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for _, m := range []string{"m1", "m2", "m3"} {
		w.setWidth(m, 8)
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m}))
	}
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "pr", Enabled: true}}
	w.s.Work.SetRows([]string{"s1"})
	w.s.Readers.SetRows([]string{ReaderPrefix + "m2", ReaderPrefix + "m3"})
	for i := 1; i <= 8; i++ { // m1's eight lanes all work: free halves 0, every ready card excess
		rbPlace(w, fmt.Sprintf("s1-w%d", i), cardhdr.RouteFlash, "m1", Working)
	}
	for i := 1; i <= 4; i++ {
		rbPlace(w, fmt.Sprintf("s1-r%d", i), cardhdr.RouteFlash, "m1", Ready)
	}
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("s1-q%d", i)
		w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Review, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief(cardhdr.RouteFlash), FieldTierNow: cardhdr.RouteFlash}})
		w.s.Fleet.Put(&Card{ID: ReadCardID(id, 1, "m1"), Row: "m1", Col: Ready, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "read", "primary": id, "stream": "s1", "reader": "m1", "attempt": "1", FieldReadCard: "1", FieldTier: cardhdr.RouteFlash}})
	}
	w.s.Work.Put(&Card{ID: "s1-f1", Row: "s1", Col: Working, Score: 20, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "2", "stream": "s1", "brief": fleetBrief(cardhdr.RouteFlash), FieldTierNow: cardhdr.RouteFlash, "work": "s1-f1.w2"}})
	w.s.Fleet.Put(&Card{ID: "s1-f1.w2", Row: "m1", Col: Ready, Score: 20, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": "s1-f1", "stream": "s1", "attempt": "2", "gen": "1", "member": "m1", "fix": "rebase it"}})
	w.s.Work.Put(&Card{ID: "s1-f2", Row: "s1", Col: Working, Score: 21, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief(cardhdr.RouteFlash), FieldTierNow: cardhdr.RouteFlash, FieldPriority: PriorityFix, "work": "s1-f2.w1"}})
	w.s.Fleet.Put(&Card{ID: "s1-f2.w1", Row: "m1", Col: Ready, Score: 21, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": "s1-f2", "stream": "s1", "attempt": "1", "gen": "1", "member": "m1", FieldPriority: PriorityFix}})

	p := w.must(Rebalance(w.s, nil, "machine"))
	assert.NotEmpty(t, rebalanced(p), "work, reads and fix all move")
	fix := w.s.Fleet.Card("s1-f1.w2")
	require.NotNil(t, fix, "the rework fix card still stands")
	assert.NotEqual(t, "m1", fix.Row, "the rework fix card moved off m1")
	assert.Equal(t, "2", fix.F("attempt"), "the rework fix card keeps its attempt")
	assert.Equal(t, "rebase it", fix.F("fix"), "the rework fix card keeps its fix")
	prio := w.s.Fleet.Card("s1-f2.w1")
	require.NotNil(t, prio, "the priority-fix card still stands")
	assert.NotEqual(t, "m1", prio.Row, "the priority-fix card moved off m1")
	assert.Equal(t, PriorityFix, prio.F(FieldPriority), "the priority-fix card keeps its priority")

	// a read whose only unit with a free lane is its worker stays: m2 worked the attempt and
	// is the only unit with a free lane, so the read never moves (mayReadCard: never the
	// attempt's worker, attemptUnit).
	w = newWorld(t)
	for _, m := range []string{"m1", "m2"} {
		w.setWidth(m, 2)
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m}))
	}
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "pr", Enabled: true}}
	w.s.Work.SetRows([]string{"s1"})
	w.s.Readers.SetRows([]string{ReaderPrefix + "m2"})
	rbPlace(w, "s1-a1", cardhdr.RouteFlash, "m1", Working)
	rbPlace(w, "s1-a2", cardhdr.RouteFlash, "m1", Working) // m1's two lanes work: no free half
	stay := "s1-stay"
	w.s.Work.Put(&Card{ID: stay, Row: "s1", Col: Review, Score: 30, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief(cardhdr.RouteFlash), FieldTierNow: cardhdr.RouteFlash}})
	w.s.Fleet.Put(&Card{ID: WorkCardID(stay, 1), Row: "m2", Col: Working, Score: 30, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": stay, "stream": "s1", "attempt": "1", "gen": "1", "member": "m2"}})
	w.s.Fleet.Put(&Card{ID: ReadCardID(stay, 1, "m1"), Row: "m1", Col: Ready, Score: 30, Rev: 1,
		Fields: map[string]string{"kind": "read", "primary": stay, "stream": "s1", "reader": "m1", "attempt": "1", FieldReadCard: "1", FieldTier: cardhdr.RouteFlash}})
	assert.Empty(t, rebalanced(w.must(Rebalance(w.s, nil, "machine"))),
		"the read stays: m2, the only unit with a free lane, worked the attempt")
	require.True(t, w.s.Fleet.Card(ReadCardID(stay, 1, "m1")).Placed(), "the read stays on m1")
	require.Nil(t, w.s.Fleet.Card(ReadCardID(stay, 1, "m2")), "never to its worker")
}

// Eight excess cards on a full fifth member round four idle members: two land on each, never
// all on the first by name; the next tick's first move goes to the member after the last one
// used (the cursor's place), never the first by name.
func TestTheRebalanceRollsItsTargets(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for _, m := range []string{"m1", "m2", "m3", "m4"} {
		w.setWidth(m, 4)
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m}))
	}
	w.setWidth("m5", 8)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m5"}))
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true}}
	w.s.Work.SetRows([]string{"s1"})
	for i := 1; i <= 8; i++ {
		rbPlace(w, fmt.Sprintf("s1-%d", i), cardhdr.RouteFlash, "m5", Working)
	}
	for i := 9; i <= 16; i++ {
		rbPlace(w, fmt.Sprintf("s1-%d", i), cardhdr.RouteFlash, "m5", Ready)
	}
	// the cursor starts one past the first by name, so after the roll over the four equal
	// members the member after the last one used is m2, not the first by name.
	w.s.Fleet.SetProps(map[string]string{PropDealIndex: "1"})
	w.must(Rebalance(w.s, nil, "machine"))
	for _, m := range []string{"m1", "m2", "m3", "m4"} {
		assert.Equal(t, 2, w.s.Fleet.Count(m, Ready), "%s takes two, never all on the first by name", m)
	}
	assert.Equal(t, 0, w.s.Fleet.Count("m5", Ready), "the fifth holds no excess after the roll")

	rbPlace(w, "s1-17", cardhdr.RouteFlash, "m5", Ready)
	w.must(Rebalance(w.s, nil, "machine"))
	assert.Equal(t, 3, w.s.Fleet.Count("m2", Ready), "the next tick's first move goes to m2, the member after the last one used, never the first by name")
	assert.Equal(t, 2, w.s.Fleet.Count("m1", Ready), "m1, the first by name, is not hit again out of turn")
	assert.Equal(t, 0, w.s.Fleet.Count("m5", Ready), "the fifth holds no excess after the next tick")
}

// A read card in Working (taken) never moves, even beside queued excess.
func TestTheRebalanceNeverMovesAStartedRead(t *testing.T) {
	t.Parallel()
	w := readQueueWorld(t, 2)
	id := "s1-9"
	w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Review, Score: 9, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "brief": fleetBrief(cardhdr.RouteFlash), FieldTierNow: cardhdr.RouteFlash}})
	w.s.Fleet.Put(&Card{ID: ReadCardID(id, 1, "m1"), Row: "m1", Col: Working, Score: 9, Rev: 1,
		Fields: map[string]string{"kind": "read", "primary": id, "stream": "s1", "reader": "m1", "attempt": "1", FieldReadCard: "1", FieldTier: cardhdr.RouteFlash}})
	w.must(Rebalance(w.s, nil, "machine"))
	c := w.s.Fleet.Card(ReadCardID(id, 1, "m1"))
	require.True(t, c.Placed(), "the working read is never moved")
	assert.Equal(t, Working, c.Col)
	assert.Equal(t, "m1", c.Row)
}

// The rebalance runs every tick, with no setting that turns it off: the work table's update
// names it after the deal, unconditionally.
func TestTheRebalanceRunsEveryTickWithNoSetting(t *testing.T) {
	t.Parallel()
	var names []string
	for _, p := range TickTables[0].Parts {
		names = append(names, p.Name)
	}
	require.Contains(t, names, PartRebalance, "the rebalance is the work table's part every tick")
	require.Equal(t, "deal", names[slices.Index(names, PartRebalance)-1], "it runs after the deal, no setting in between")
}

// Two queued cards and one idle lane: one moves.
func TestRebalanceMovesOneCardPerIdleLane(t *testing.T) {
	t.Parallel()
	w := rbWorld(t, "m1", "m2")
	rbPlace(w, "s1-1", cardhdr.RouteFlash, "m1", Working)
	a := rbPlace(w, "s1-2", cardhdr.RouteFlash, "m1", Ready)
	b := rbPlace(w, "s1-3", cardhdr.RouteFlash, "m1", Ready)
	p := w.must(Rebalance(w.s, nil, "machine"))
	assert.Len(t, rebalanced(p), 1)
	assert.Equal(t, 1, w.s.Fleet.Count("m2", Ready))
	assert.Equal(t, "m2", w.s.Fleet.Card(a.ID).Row, "the first in the deal order")
	assert.Equal(t, "m1", w.s.Fleet.Card(b.ID).Row)
	assert.Empty(t, rebalanced(Rebalance(w.s, nil, "machine")), "no idle lane left")
}
