package sprint_test

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The deal reads every width from nova-config alone (docs/SPEC-SPRINT.md
// section 5; tla/Level.tla, Width): a member's width is the width field of its
// machine:<m> hash as nova-config apply wrote it, read with the snapshot on
// every tick. Nothing is cached across ticks: a config change applies at the
// next tick with no restart and no sync, and a width lowered under a member's
// working cards never withdraws one.

type widthRig struct {
	t   *testing.T
	m   *store.Mem
	st  *store.Store
	mu  sync.Mutex
	now time.Time
	n   int
}

func newWidthRig(t *testing.T) *widthRig {
	t.Helper()
	m := store.NewMem()
	r := &widthRig{t: t, m: m, now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	n := 0
	r.st = &store.Store{B: m, Names: sprint.Names{Prefix: "w-"}, Actor: "coordinator",
		Now:   func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { r.mu.Lock(); defer r.mu.Unlock(); n++; return fmt.Sprint(n) },
		Sleep: func(time.Duration) {}}
	r.st.ShareTwin(store.NewTwin())
	require.NoError(t, r.st.Init(context.Background()))
	require.NoError(t, m.RowsAdd(context.Background(), "w-readers", []string{"reader-a"}))
	require.NoError(t, m.SetCoordinator(context.Background(), "coordinator"))
	_, _, _, err := r.st.SetMachine(context.Background(), true)
	require.NoError(t, err)
	return r
}

func (r *widthRig) up(name string) {
	r.t.Helper()
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: name}))
}

func (r *widthRig) must(step store.Step) store.Result {
	r.t.Helper()
	res, err := r.st.Run(context.Background(), step)
	require.NoError(r.t, err, step.Verb)
	require.Empty(r.t, res.Refused, "%s refused", step.Verb)
	return res
}

func (r *widthRig) add(count int) {
	r.t.Helper()
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", Count: count}))
}

func (r *widthRig) take(name string) {
	r.t.Helper()
	r.must(store.TakeStep(sprint.TakeReq{As: name, Sel: sprint.Sel{Limit: 100}, Who: name}))
}

func (r *widthRig) deal() {
	r.t.Helper()
	r.must(store.TickPartStep("deal", sprint.TickDeal, sprint.TickReq{}, nil, nil, nil))
}

func (r *widthRig) snap() *sprint.Snapshot {
	r.t.Helper()
	s, err := r.st.Load(context.Background(), store.All, nil)
	require.NoError(r.t, err)
	return s
}

func (r *widthRig) working(name string) int {
	return r.snap().Fleet.Count(name, sprint.Working)
}

func (r *widthRig) ready(name string) int {
	return r.snap().Fleet.Count(name, sprint.Ready)
}

func (r *widthRig) heldBy(name string) int {
	return r.ready(name) + r.working(name)
}

// TestTheDealUsesTheConfigWidthWithNoRestart: a member up whose control card
// names width 32 (the old copy) and whose machine:<m> width is 16 is dealt to
// 16, not 32; a config change with no restart moves the deal at the next tick.
func TestTheDealUsesTheConfigWidthWithNoRestart(t *testing.T) {
	t.Parallel()
	r := newWidthRig(t)
	r.up("m1")
	// the config width is 16 (the control card names nothing: a width is
	// nova-config's alone)
	r.m.SetMachineWidth("m1", "16")

	r.add(40)
	r.deal()
	require.Equal(t, 2*16, r.heldBy("m1"), "at config width 16 the deal holds 2*16, not the old copy's room")
	require.Equal(t, 2*16, r.ready("m1"), "the deal filled the 2x room at width 16")

	// the member takes up to its width: 16 working
	r.take("m1")
	require.Equal(t, 16, r.working("m1"), "the member works at most its width 16")

	// lower the width with no restart: nothing new is dealt while working >= 4,
	// and its working cards are not withdrawn
	r.m.SetMachineWidth("m1", "4")
	r.add(40)
	r.deal()
	require.Equal(t, 16, r.working("m1"), "lowering the width withdrew working cards")
	require.LessOrEqual(t, r.heldBy("m1"), 2*16, "lowering the width to 4 dealt new cards")

	// raise it: the next tick fills it to the raised width's room
	r.m.SetMachineWidth("m1", "24")
	r.deal()
	require.Equal(t, 2*24, r.heldBy("m1"), "raising the width to 24 fills its room")

	// width 0 drains: no new deal, working cards finish where they are
	r.m.SetMachineWidth("m1", "0")
	r.add(10)
	r.deal()
	require.Equal(t, 2*24, r.heldBy("m1"), "the drain deals nothing new")

	// delete the hash: dealt nothing, one judgment naming nova-config
	r.m.DeleteMachineWidth("m1")
	r.add(10)
	r.must(store.TickPartStep("deal", sprint.TickDeal, sprint.TickReq{}, nil, nil, nil))
	require.Equal(t, 2*24, r.heldBy("m1"), "a missing width deals nothing")
	named := false
	for _, o := range r.snap().Open {
		if strings.Contains(o.Note.What, "nova-config") || strings.Contains(o.Note.What, "width not in nova-config") {
			named = true
		}
	}
	require.True(t, named, "no judgment names nova-config for the missing width")

	// a friend's deal width is friend:<f>:desired, not the roster copy
	// (docs/SPEC-SPRINT.md section 5; tla/Level.tla Width)
	ctx := context.Background()
	_, _, _, err := r.st.SyncFriends(ctx, []store.FriendSpec{{Name: "f1", Width: 99, Class: "flash,pro"}})
	require.NoError(t, err)
	r.m.SetFriendDesiredWidth("f1", "8")
	var cards []sprint.CardAdd
	for i := 1; i <= 40; i++ {
		cards = append(cards, sprint.CardAdd{ID: fmt.Sprintf("s2-%d", i), Brief: "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend\n\nThe task."})
	}
	r.must(store.AddStep(sprint.AddReq{Stream: "s2", Cards: cards}))
	seats := r.friendSeatsUp("f1")
	require.Equal(t, 8, seats[0].Width, "desired 8 wins over the roster's 99")
	r.must(store.TickPartStep("deal", sprint.TickDeal, sprint.TickReq{Friends: seats}, nil, nil, nil))
	require.Equal(t, 2*8, r.heldFriend("f1"), "dealt to desired width 8, not the roster's 99")

	r.m.SetFriendDesiredWidth("f1", "2")
	seats = r.friendSeatsUp("f1")
	require.Equal(t, 2, seats[0].Width)
	r.must(store.TickPartStep("deal", sprint.TickDeal, sprint.TickReq{Friends: seats}, nil, nil, nil))
	require.Equal(t, 2*8, r.heldFriend("f1"), "lowering the desired width withdraws nothing")

	r.m.SetFriendDesiredWidth("f1", "8")
	seats = r.friendSeatsUp("f1")
	require.Equal(t, 8, seats[0].Width)
	r.m.SetFriendDesiredWidth("f1", "0")
	seats = r.friendSeatsUp("f1")
	require.Equal(t, 0, seats[0].Width, "desired 0 drains")
	r.must(store.TickPartStep("deal", sprint.TickDeal, sprint.TickReq{Friends: seats}, nil, nil, nil))
	require.Equal(t, 2*8, r.heldFriend("f1"), "the drain deals nothing new")
}

func (r *widthRig) friendSeatsUp(name string) []sprint.FriendSeat {
	r.t.Helper()
	seats, err := r.st.FriendSeats(context.Background(), r.now)
	require.NoError(r.t, err)
	for i := range seats {
		if seats[i].Name == name {
			seats[i].Status = sprint.Up
			return []sprint.FriendSeat{seats[i]}
		}
	}
	r.t.Fatalf("no friend %s", name)
	return nil
}

func (r *widthRig) heldFriend(name string) int {
	row := sprint.FriendRow(name)
	return r.snap().Fleet.Count(row, sprint.Ready) + r.snap().Fleet.Count(row, sprint.Working)
}

// TestNoPlanWritesAWidth is a class test over the package: no plan, verb or
// store write sets FieldWidth on a control card or a friend roster row. A
// writer added later fails it, naming the site.
func TestNoPlanWritesAWidth(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	var sites []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		af, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			continue
		}
		ast.Inspect(af, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			mt, ok := cl.Type.(*ast.MapType)
			if !ok {
				return true
			}
			if id, ok := mt.Key.(*ast.Ident); !ok || id.Name != "string" {
				return true
			}
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "FieldWidth" {
					continue
				}
				p := fset.Position(kv.Pos())
				sites = append(sites, fmt.Sprintf("%s:%d", f, p.Line))
			}
			return true
		})
	}
	require.Empty(t, sites, "a plan, verb or store writes FieldWidth: %v", sites)
}
