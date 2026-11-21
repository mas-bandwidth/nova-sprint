package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// dealtTo is the working work consumers row holds in s's snapshot.
func dealtTo(tk *Ticker, row string) []string {
	var out []string
	for id, c := range tk.Snapshot().Cards {
		if c.Kind == "work" && c.Holder == row && c.State == "working" {
			out = append(out, id)
		}
	}
	return out
}

// The run follows nova-config's set of machines (Glenn, 2026-11-15): on the
// first tick whose roster no longer names alice (bob is still named, so the
// read is not empty) alice is retired, her dealt card goes back to the queue
// and is dealt to bob, and her row is in no read.
func TestTickRetiresARowTheRosterNoLongerNames(t *testing.T) {
	t.Parallel()
	f, ro := seeded(t)
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	tickN(t, tk, ro, 3)
	if got := dealtTo(tk, "alice"); len(got) != 1 {
		t.Fatalf("want one card dealt to alice first, got %v", got)
	}
	must(t, f, "bob", store.PutRow{Name: "bob", Session: "sess-3", Role: "worker"}, store.SetRowState{Row: "bob", State: "up"})
	gone := roster.Roster{Workers: map[string]roster.Worker{"bob": {Name: "bob", Width: 1, Tiers: ro.Workers["alice"].Tiers}}}
	for i := range 3 {
		now := t0.Add(time.Duration(10+i) * time.Second)
		if _, err := tk.Tick(context.Background(), Observed{Now: now, Present: []string{"alice", "bob"}, Roster: gone, Heads: map[string]string{"s": strings.Repeat("a", 40)}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, shown := tk.Snapshot().Rows["alice"]; shown {
		t.Fatalf("alice is still in the run's snapshot: %+v", tk.Snapshot().Rows["alice"])
	}
	if got := dealtTo(tk, "alice"); len(got) != 0 {
		t.Fatalf("alice still holds %v", got)
	}
	if got := dealtTo(tk, "bob"); len(got) != 1 {
		t.Fatalf("want p1 dealt again to bob, got %v; cards %+v", got, tk.Snapshot().Cards)
	}
}

// An empty roster (no worker, no kin) retires nothing: a read that returns
// nothing is a fault, never a fleet with no rows.
func TestTickRetiresNothingOnAnEmptyRoster(t *testing.T) {
	t.Parallel()
	f, ro := seeded(t)
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	tickN(t, tk, ro, 3)
	tickN(t, tk, roster.Roster{}, 3)
	if _, shown := tk.Snapshot().Rows["alice"]; !shown {
		t.Fatal("alice was retired on an empty roster")
	}
	if got := dealtTo(tk, "alice"); len(got) != 1 {
		t.Fatalf("alice's hand moved on an empty roster: %v", got)
	}
}

// Every roster read fails (180 such faults sat in the run's log on
// 2026-11-15: "nova-config's roster cannot be read (postgres: connect
// failed)"): no tick plans, so no row is retired and no hand moves.
func TestServeRosterFailureRetiresNothing(t *testing.T) {
	t.Parallel()
	f, _ := seeded(t)
	ro, err := NewConfigRoster(&fakeConfigSnapshot{err: errors.New("postgres: connect failed")}, noFriends{}, 8)
	if err != nil {
		t.Fatal(err)
	}
	clock := &watched{beats: &beats{now: t0, tick: make(chan time.Time)}, read: make(chan struct{})}
	b := &fakeBus{who: map[string]bus.Presence{"alice": {Name: "alice"}}}
	var log strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, Config{Store: f, Roster: ro, Bus: b, Policy: &answering{}, Git: stillGit{}, ReposDir: t.TempDir(), Clock: clock, Log: &log}, &healthy{})
	}()
	for range 4 {
		clock.tick <- t0
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	snap, err := f.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r, shown := snap.Rows["alice"]; !shown || r.Session != "sess-1" {
		t.Fatalf("alice after failed roster reads: %+v shown=%t", r, shown)
	}
}
