package sprint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// A tier is served by an enabled fleet route or by a friend up whose row lists it (the
// owner, 2026-10-04: "Fleet flash only; pro to friends"). On 2026-10-06 the fleet's pro
// routes were disabled on purpose and stella was up with pro on her row, and
// `rework --tier pro` refused: no enabled route serves tier pro.

// proOffWorld is s1-1 in review after a failed attempt, two machines up, a flash route
// enabled and the pro route disabled.
func proOffWorld(t *testing.T) *world {
	t.Helper()
	w := setup(t, 1)
	finished(w, "s1-1", true)
	require.Equal(t, Review, w.state("s1-1"))
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true},
		{Name: "pro-off", Tier: cardhdr.RoutePro, Provider: "p", Model: "pro", Enabled: false},
	}
	return w
}

func TestReworkAcceptsATierAFriendServes(t *testing.T) {
	t.Parallel()
	stella := func(status string) FriendSeat {
		return FriendSeat{Name: "stella", Width: 2, Status: status, Class: "heavy,pro"}
	}
	johnny := FriendSeat{Name: "johnny", Width: 2, Status: Down, Class: "pro"}
	rework := func(w *world) Plan {
		return Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "make it green", Tier: cardhdr.RoutePro})
	}

	t.Run("stella up serves pro: the rework moves, and the friends' deal deals it to her", func(t *testing.T) {
		t.Parallel()
		w := proOffWorld(t)
		w.s.Friends = []FriendSeat{stella(Up)}
		p := w.must(rework(w))
		require.Len(t, p.Units, 1)
		assert.Contains(t, p.Units[0].Moved, "review -> ready")
		assert.Contains(t, p.Units[0].Moved, "stella")
		assert.Equal(t, Ready, w.state("s1-1"))
		for _, m := range w.s.Members() {
			for _, c := range w.s.Fleet.Cell(m, Ready) {
				assert.NotEqual(t, "s1-1", c.F("primary"), "no machine is dealt a tier no route serves")
			}
		}
		// the tick: no judgment of the tier, and stella is dealt it
		dealWith(w, stella(Up))
		wc := w.s.Fleet.Card("s1-1.w2")
		require.NotNil(t, wc, "its next attempt is dealt")
		assert.Equal(t, FriendRow("stella"), wc.Row)
		for _, o := range w.s.Open {
			assert.NotEqual(t, NNoRoute, o.Note.Type, "a tier a friend up serves is no judgment: %s", o.Note.What)
		}
	})
	t.Run("stella held: refused naming her", func(t *testing.T) {
		t.Parallel()
		w := proOffWorld(t)
		w.s.Friends = []FriendSeat{stella(Held)}
		p := rework(w)
		require.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "no enabled route and no up friend serves tier pro")
		assert.Contains(t, p.Refused[0].Why, "bring up a friend whose row lists pro")
		assert.Contains(t, p.Refused[0].Why, "stella serves pro but is held/down")
		assert.Equal(t, Review, w.state("s1-1"))
	})
	t.Run("stella held and johnny down: refused naming both", func(t *testing.T) {
		t.Parallel()
		w := proOffWorld(t)
		w.s.Friends = []FriendSeat{stella(Held), johnny}
		p := rework(w)
		require.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "stella and johnny serve pro but are held/down")
	})
	t.Run("no friend: refused naming both ways", func(t *testing.T) {
		t.Parallel()
		w := proOffWorld(t)
		p := rework(w)
		require.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "no enabled route and no up friend serves tier pro: enable a route (nova-config route add")
	})
}

// Every verb that validates a tier goes through one check (tierServed): every string
// literal of the sprint's sources that refuses for want of an enabled route is inside it,
// and the deal's draw (routeOf), the tick's (noRoute) and the reads' (readRouteMissing)
// call it.
func TestATierCheckIsOneFunction(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	calls := map[string]bool{}
	found := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		file, err := parser.ParseFile(fset, f, src, 0)
		require.NoError(t, err)
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING && strings.Contains(x.Value, "no enabled route") {
						found++
						assert.Equal(t, "tierServed", fn.Name.Name, "%s: a refusal for want of a route outside the one check", fset.Position(x.Pos()))
					}
				case *ast.CallExpr:
					if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "tierServed" {
						calls[fn.Name.Name] = true
					}
				}
				return true
			})
		}
	}
	assert.Positive(t, found, "the one check's refusal is found")
	for _, caller := range []string{"routeOf", "readRouteMissing"} {
		assert.True(t, calls[caller], "%s goes through tierServed", caller)
	}
}

// --- A frontier card is the friends' by design (docs/SPEC-SPRINT.md, tiers and routes) ---
//
// The bug (2026-10-11): routeTier refused every frontier card before it consulted
// tierServed, so rework and accept read only the routes table, held.go and idle.go raised
// "no route serves tier frontier", readers.go and ready_over.go read the same words as its
// cause, and judgments_retire.go retired the judgment only when a route appeared, while the
// friends' deal already handed frontier cards to a friend row whose tiers include frontier.
// The coordinator moved 36 frontier cards only by twins ("I hate twins." / "Twinning is the
// last refuge of the scoundrel. Fix your tools."). A friend up whose row lists frontier
// serves it as a route would.

// frontierWorld is a world with one frontier primary in col (review, for a rework, or ready,
// for the tick's judgment), the two machines up, and one route of another tier so the store
// is not empty (the deal's own-model case is not in play).
func frontierWorld(t *testing.T, col string) *world {
	t.Helper()
	w := setup(t, 1)
	w.s.Work.SetRows([]string{"s1"})
	w.s.Work.Put(&Card{ID: "s1-1", Row: "s1", Col: col, Score: 1, Rev: 1, Fields: map[string]string{
		"kind": "primary", "attempt": "1", "stream": "s1", "head": "head",
		"brief": "c: the work (s1) tier: frontier\n\nThe task.\n"}})
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true},
	}
	return w
}

// frontierFay is a friend row that lists frontier.
func frontierFay(status string) FriendSeat {
	return FriendSeat{Name: "fay", Width: 2, Status: status, Tiers: []string{cardhdr.RouteFrontier}}
}

// openNoRoute is the tier's "no route serves the tier" judgment, if it is open.
func openNoRoute(w *world) (Open, bool) {
	for _, o := range w.s.Open {
		if o.Note.Type == NNoRoute {
			return o, true
		}
	}
	return Open{}, false
}

// (i) Rework of a frontier card in review moves it to ready when a friend up serves
// frontier, and the friends' deal deals it to her, never a machine.
func TestReworkAcceptsAFrontierTierAFriendServes(t *testing.T) {
	t.Parallel()
	w := frontierWorld(t, Review)
	w.s.Friends = []FriendSeat{frontierFay(Up)}
	p := w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "make it green", Tier: cardhdr.RouteFrontier}))
	require.Len(t, p.Units, 1)
	assert.Contains(t, p.Units[0].Moved, "review -> ready")
	assert.Equal(t, Ready, w.state("s1-1"))
	dealWith(w, frontierFay(Up))
	wc := w.s.Fleet.Card("s1-1.w2")
	require.NotNil(t, wc, "its next attempt is dealt to the friend")
	assert.Equal(t, FriendRow("fay"), wc.Row, "a frontier card is the friend's, not a machine's")
	for _, o := range w.s.Open {
		assert.NotEqual(t, NNoRoute, o.Note.Type, "a frontier tier a friend up serves is no judgment: %s", o.Note.What)
	}
}

// (ii) Accept sees the same served tier: the check it would read (routeServed) says a
// frontier tier a friend up serves is served, and accept's own refusal, if any, is the
// reads', never the tier. (Accept of a card in review never read the routes table at this
// base; this pins the check every tier-validating verb reads.)
func TestAcceptSeesAFrontierTierAFriendServes(t *testing.T) {
	t.Parallel()
	w := frontierWorld(t, Review)
	w.s.Friends = []FriendSeat{frontierFay(Up)}
	if _, why, byFriend := w.s.routeServed(w.s.Work.Card("s1-1"), nil, ""); !byFriend {
		t.Fatalf("accept's own check: a frontier tier a friend up serves is served, got %s", why)
	}
	for _, r := range Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"s1-1"}}}).Refused {
		assert.NotContains(t, r.Why, "frontier", "accept refuses for the reads, never the tier: %s", r.Why)
	}
}

// (iii) No "no route serves the tier" judgment of frontier while a friend up serves it,
// and one raised while no friend served retires within the tick.
func TestAFrontierTierAFriendServesRaisesNoNoRouteJudgment(t *testing.T) {
	t.Parallel()
	w := frontierWorld(t, Ready)
	td, _ := TickDeal(w.s, TickReq{})
	w.do(td)
	if _, ok := openNoRoute(w); !ok {
		t.Fatalf("frontier with no friend: the tier's judgment is raised: %v", w.s.Open)
	}
	// a friend up whose row lists frontier serves it, though her row has no room: the card
	// waits ready for her, as one waits on a full provider, never a judgment
	fay := FriendSeat{Name: "fay", Width: 0, Status: Up, Tiers: []string{cardhdr.RouteFrontier}}
	w.s.Friends = []FriendSeat{fay}
	td, _ = TickDeal(w.s, TickReq{Friends: []FriendSeat{fay}})
	w.do(td)
	if o, ok := openNoRoute(w); ok {
		t.Fatalf("a friend up serves frontier: no judgment, got %s", o.Note.What)
	}
}

// (iv) The idle root for the tier (idle.go's noRoute call) is not the no-route root while
// a friend up serves frontier.
func TestIdleRootForAFrontierTierAFriendServesIsNoNoRoute(t *testing.T) {
	t.Parallel()
	w := frontierWorld(t, Ready)
	c := w.s.Work.Card("s1-1")
	w.s.Friends = []FriendSeat{frontierFay(Up)}
	if _, why := w.s.noRoute(escalating(w.s, c)); why != "" {
		t.Fatalf("idle roots a frontier tier a friend up serves nowhere near rootNoRoute: %s", why)
	}
}

// (v) The same friend held, down, or with a row lacking frontier: the refusal and the
// judgment return, naming both the routes it looked at and the friends.
func TestAFrontierTierNoFriendServesIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		fays []FriendSeat
	}{
		{"held", []FriendSeat{frontierFay(Held)}},
		{"down", []FriendSeat{frontierFay(Down)}},
		{"row lacks frontier", []FriendSeat{{Name: "fay", Width: 2, Status: Up, Tiers: []string{cardhdr.RouteFlash}}}},
		{"no friend", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := frontierWorld(t, Review)
			w.s.Friends = tc.fays
			_, _, why, byFriend := w.s.routeOf(w.s.Work.Card("s1-1"), nil, nil, "")
			assert.False(t, byFriend)
			assert.Contains(t, why, "no route and no friend serves tier frontier")
			assert.Contains(t, why, "no up friend serves tier frontier")
			p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "make it green", Tier: cardhdr.RouteFrontier})
			require.Len(t, p.Refused, 1, "frontier with no friend up is refused")
			assert.Contains(t, p.Refused[0].Why, "frontier")
			assert.Equal(t, Review, w.state("s1-1"))
		})
	}
}
