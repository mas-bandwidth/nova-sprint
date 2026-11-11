package run

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The sprint's hard requirements on a row (README "Hard requirements",
// tla/RowAlive.tla), for a friend and a bench alike:
//
//	R1 a row is up while its hook is on the bus and down when it has been
//	   off it for RowSilentGone: nothing manual (no pong, no command);
//	R2 every card a row holds when it goes down or is held is returned to
//	   the queue and dealt again, with nobody doing anything.
//
// Unit tests: a fake store, no clock, no sockets. Clocks start at 1000.

var rt0 = time.Unix(1000, 0).UTC()

// gone is the bus silence after which a row is down (RowSilentGone).
var gone = time.Duration(steps.Engine.Timeouts.RowSilentGone) * time.Second

func rowMust(t *testing.T, st store.Store, id string, ws ...store.Write) {
	t.Helper()
	r, err := st.Apply(context.Background(), store.Op{ID: id, Epoch: 1, Actor: "test", Kind: "test", At: rt0, Writes: ws})
	if err != nil || !r.Accepted {
		t.Fatalf("%s: %+v %v", id, r, err)
	}
}

// rowSeeded is a started sprint with one stream, two producers, coordinator
// coord, and rows bob (up) and amy (down, off the bus), width 2 each and both
// hooked in, of kind (roster.KindKin: friends; roster.KindWorker: benches).
// amy comes up by R1 when her hook is on the bus.
func rowSeeded(t *testing.T, kind string) (*store.Fake, roster.Roster, *Ticker) {
	t.Helper()
	f := store.NewFake()
	rowMust(t, f, "start", store.SetStopped{Stopped: false, Reason: "test"})
	rowMust(t, f, "policy", activePolicy()...)
	rowMust(t, f, "stream", store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"})
	rowMust(t, f, "coord", store.PutRow{Name: "coord", Session: "sess-c", Role: "coordinator"})
	tiers := []string{"flash", "pro", "heavy", "frontier"}
	ro := roster.Roster{Kin: map[string]roster.Kin{}, Workers: map[string]roster.Worker{}}
	for _, n := range []string{"bob", "amy"} {
		rowMust(t, f, "row-"+n, store.PutRow{Name: n, Session: "sess-" + n, Role: "worker"})
		if n == "bob" {
			rowMust(t, f, "up-"+n, store.SetRowState{Row: n, State: "up"})
		}
		if kind == roster.KindKin {
			ro.Kin[n] = roster.Kin{Name: n, Model: "kin/model", Width: 2, Tiers: tiers}
		} else {
			ro.Workers[n] = roster.Worker{Name: n, Width: 2, Tiers: tiers, Harness: roster.WorkerHarness}
		}
	}
	rowMust(t, f, "p1", store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p1", Paths: []string{"a"}})
	rowMust(t, f, "p2", store.PutCard{ID: "p2", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p2", Paths: []string{"b"}})
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, rt0)
	if err != nil {
		t.Fatal(err)
	}
	return f, ro, tk
}

func rowTick(t *testing.T, tk *Ticker, ro roster.Roster, now time.Time, present ...string) {
	t.Helper()
	if _, err := tk.Tick(context.Background(), Observed{Now: now, Present: present, Roster: ro, Heads: map[string]string{"s": strings.Repeat("a", 40)}}); err != nil {
		t.Fatal(err)
	}
}

// heldBy is the live consumers row holds, sorted.
func heldBy(tk *Ticker, row string) []string {
	var out []string
	for id, c := range tk.Snapshot().Cards {
		if c.Holder == row && (c.State == "working" || c.State == "ready") {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// producersOf is the producers of row's live consumers, sorted.
func producersOf(tk *Ticker, row string) []string {
	snap := tk.Snapshot()
	var out []string
	for _, id := range heldBy(tk, row) {
		out = append(out, snap.Cards[id].Of)
	}
	slices.Sort(out)
	return out
}

// dealBob ticks with bob alone on the bus (amy down, off it) until bob
// holds a card, and returns the time and the producers bob works for.
func dealBob(t *testing.T, tk *Ticker, ro roster.Roster) (time.Time, []string) {
	t.Helper()
	now := rt0
	for i := 0; i < 10; i++ {
		now = now.Add(time.Second)
		rowTick(t, tk, ro, now, "bob")
		if got := producersOf(tk, "bob"); len(got) > 0 {
			return now, got
		}
	}
	t.Fatalf("bob was dealt nothing in 10 ticks")
	return now, nil
}

// hold is the coordinator's hold of row, applied as the verb applies it.
func hold(t *testing.T, f store.Store, tk *Ticker, row string, now time.Time) {
	t.Helper()
	op, why := steps.Verb(steps.Build(tk.Snapshot(), now), steps.Inputs{Now: now},
		steps.Request{Actor: "coord", Coordinators: []string{"coord"}, Row: row, Event: card.Hold{Reason: "test hold"}})
	if why != nil {
		t.Fatalf("hold %s refused: %v", row, why)
	}
	op.Epoch, op.At = tk.Snapshot().Epoch, now
	if r, err := f.Apply(context.Background(), op); err != nil || !r.Accepted {
		t.Fatalf("hold %s: %+v %v", row, r, err)
	}
}

var bothKinds = []string{roster.KindKin, roster.KindWorker}

// R1: a down row whose hook is on the bus comes up on the next tick, with
// nothing done by the row (the 2026-11-11 defect: Stella's friend row stayed
// down for 3.5 minutes with her hook on the bus, until her model ran a pong).
func TestRowUpWhileHookOnBus(t *testing.T) {
	t.Parallel()
	for _, kind := range bothKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, ro, tk := rowSeeded(t, kind)
			rowMust(t, f, "down-bob", store.SetRowState{Row: "bob", State: "down"})
			if err := tk.Fold(context.Background()); err != nil {
				t.Fatal(err)
			}
			rowTick(t, tk, ro, rt0.Add(time.Second), "bob")
			if st := tk.Snapshot().Rows["bob"].State; st != "up" {
				t.Fatalf("%s row bob, hooked and on the bus, is %s after a tick; want up with nothing done by bob", kind, st)
			}
		})
	}
}

// R1: a row whose hook stays on the bus stays up and keeps its hand, however
// long (three RowSilentGone spans), with nothing done by the row.
func TestRowStaysUpWhileHookOnBus(t *testing.T) {
	t.Parallel()
	for _, kind := range bothKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			_, ro, tk := rowSeeded(t, kind)
			start, _ := dealBob(t, tk, ro)
			for at := start; at.Before(start.Add(3 * gone)); at = at.Add(30 * time.Second) {
				rowTick(t, tk, ro, at, "bob", "amy")
			}
			if r := tk.Snapshot().Rows["bob"]; r.State != "up" || r.Flaps != 0 {
				t.Fatalf("%s row bob, on the bus throughout, is %s with %d flaps; want up, 0", kind, r.State, r.Flaps)
			}
		})
	}
}

// R1 and R2: a row whose hook leaves the bus is down after RowSilentGone,
// holds nothing, and every card it held is dealt again to a row that is up.
func TestRowDownWhenHookGoneAndHandDealtAgain(t *testing.T) {
	t.Parallel()
	for _, kind := range bothKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			_, ro, tk := rowSeeded(t, kind)
			start, had := dealBob(t, tk, ro)
			at := start
			for ; at.Before(start.Add(gone - 30*time.Second)); at = at.Add(30 * time.Second) {
				rowTick(t, tk, ro, at, "amy")
			}
			if st := tk.Snapshot().Rows["bob"].State; st != "up" {
				t.Fatalf("%s row bob went %s before RowSilentGone", kind, st)
			}
			for i := 0; i < 6; i++ {
				at = at.Add(30 * time.Second)
				rowTick(t, tk, ro, at, "amy")
			}
			if st := tk.Snapshot().Rows["bob"].State; st != "down" {
				t.Fatalf("%s row bob, off the bus past RowSilentGone, is %s; want down", kind, st)
			}
			if got := heldBy(tk, "bob"); len(got) != 0 {
				t.Fatalf("%s row bob is down and still holds %v", kind, got)
			}
			if got := producersOf(tk, "amy"); !containsAll(got, had) {
				t.Fatalf("bob's cards were not dealt again: amy works for %v, want at least %v", got, had)
			}
		})
	}
}

// R2: a held row's whole hand is returned and dealt again to a row that is
// up, with nobody doing anything after the hold.
func TestHeldRowHandDealtAgain(t *testing.T) {
	t.Parallel()
	for _, kind := range bothKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, ro, tk := rowSeeded(t, kind)
			at, had := dealBob(t, tk, ro)
			hold(t, f, tk, "bob", at)
			for i := 0; i < 5; i++ {
				at = at.Add(time.Second)
				rowTick(t, tk, ro, at, "bob", "amy")
			}
			if got := heldBy(tk, "bob"); len(got) != 0 {
				t.Fatalf("%s row bob is held and still holds %v", kind, got)
			}
			if got := producersOf(tk, "amy"); !containsAll(got, had) {
				t.Fatalf("held bob's cards were not dealt again: amy works for %v, want at least %v", got, had)
			}
		})
	}
}

// containsAll says whether every one of want is in got.
func containsAll(got, want []string) bool {
	for _, w := range want {
		if !slices.Contains(got, w) {
			return false
		}
	}
	return true
}
