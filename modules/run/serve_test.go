package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Unit tests of the run's start: no time, no socket. The heads read and the
// tick beat are the test's: a gated or failing git, and the beats clock.

// gatedGit reads every base tip once gate is closed: a first heads read
// slower than the first beat (one ls-remote per repository, one after
// another, after every restart).
type gatedGit struct {
	stillGit
	gate chan struct{}
}

func (g gatedGit) LsRemote(ctx context.Context, dir, ref string) (string, bool, error) {
	<-g.gate
	return g.stillGit.LsRemote(ctx, dir, ref)
}

// unreachableGit is a repository whose base tip cannot be read: its
// ls-remote fails, as the git edge's does when its timeout ends it.
type unreachableGit struct{ stillGit }

func (unreachableGit) LsRemote(context.Context, string, string) (string, bool, error) {
	return "", false, errors.New("git ls-remote: signal: killed")
}

// dealable is a sprint as a restart finds it: one stream, bench up and one
// producer already ready, so the first tick deals, and a tick with no head
// for s judges s headless.
func dealable(t *testing.T) *store.Fake {
	f := started(t)
	must(t, f, "stream", store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"})
	must(t, f, "row", store.PutRow{Name: "bench", Session: "sess-1", Role: "worker"})
	must(t, f, "up", store.SetRowState{Row: "bench", State: "up"})
	must(t, f, "card", store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p1", Paths: []string{"a"}})
	must(t, f, "ready", store.SetCardState{Card: "p1", State: "ready"})
	return f
}

// headless is every engine judgement, open or cleared, that says stream s
// had no head in a tick's inputs.
func headless(t *testing.T, f *store.Fake) []string {
	t.Helper()
	snap, err := f.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for id, j := range snap.Judgements {
		if j.Kind == "engine" && strings.HasPrefix(j.Question, "stream s has no head in the tick's inputs") {
			out = append(out, id+" ("+j.State+")")
		}
	}
	return out
}

// TestServeFirstTickWaitsForHeads: after a restart the first heads read is
// still running when the first beat comes. The run takes no beat until that
// read is done, so no tick passes empty heads and s, which has a deal, is
// never judged headless; once the read is done the beat ticks and deals.
func TestServeFirstTickWaitsForHeads(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := dealable(t)
		ro, err := NewConfigRoster(&fakeConfigSnapshot{}, 8)
		if err != nil {
			t.Fatal(err)
		}
		gate := make(chan struct{})
		clock := &beats{now: t0, tick: make(chan time.Time)}
		b := &fakeBus{who: map[string]bus.Presence{"bench": {Name: "bench"}}}
		var log strings.Builder
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- serve(ctx, Config{Store: f, Roster: ro, Bus: b, Policy: &answering{}, Git: gatedGit{gate: gate}, ReposDir: t.TempDir(), Clock: clock, Log: &log}, &healthy{})
		}()
		beaten := make(chan struct{})
		// The first beat comes while the heads read runs; then every
		// goroutine is blocked, the heads read on its gate.
		go func() { clock.tick <- t0; close(beaten) }()
		synctest.Wait()
		if got := headless(t, f); len(got) != 0 {
			t.Errorf("a tick ran before the first heads read was done and judged s headless: %v\n%s", got, log.String())
		}
		close(gate)
		<-beaten
		clock.tick <- t0 // the first tick has finished once the second beat is taken
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := headless(t, f); len(got) != 0 {
			t.Errorf("s was judged headless at start: %v\n%s", got, log.String())
		}
		snap, err := f.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if c, ok := snap.Cards["p1.w1"]; !ok || c.Holder != "bench" {
			t.Errorf("the first tick after the heads read did not deal p1 to bench: %+v\n%s", snap.Cards, log.String())
		}
	})
}

// TestServeUnreadableHeadsStillTick: a repository whose base tip cannot be
// read at start does not hold the run. The first read ends with the failure
// (the git edge bounds each ls-remote and clone), said as a heads fault; the
// run then ticks as before, and s, with a deal and no head, is judged.
func TestServeUnreadableHeadsStillTick(t *testing.T) {
	t.Parallel()
	f := dealable(t)
	ro, err := NewConfigRoster(&fakeConfigSnapshot{}, 8)
	if err != nil {
		t.Fatal(err)
	}
	clock := &beats{now: t0, tick: make(chan time.Time)}
	b := &fakeBus{who: map[string]bus.Presence{"bench": {Name: "bench"}}}
	var log strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, Config{Store: f, Roster: ro, Bus: b, Policy: &answering{}, Git: unreachableGit{}, ReposDir: t.TempDir(), Clock: clock, Log: &log}, &healthy{})
	}()
	clock.tick <- t0
	clock.tick <- t0 // the first tick has finished once the second beat is taken
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	out := log.String()
	if !strings.Contains(out, "EDGE FAULT heads stream s") || !strings.Contains(out, "TICK n=1 ") {
		t.Fatalf("want the heads fault said and the run ticking: %s", out)
	}
	if got := headless(t, f); len(got) != 1 {
		t.Fatalf("s, unreadable, has %v no-head judgements, want one\n%s", got, out)
	}
}

// slowGit is repositories whose ls-remote each waits on gate, as one that
// runs to the git edge's timeout does; it counts how many are read at once.
type slowGit struct {
	stillGit
	gate      chan struct{}
	mu        sync.Mutex
	now, most int
}

func (g *slowGit) LsRemote(ctx context.Context, dir, ref string) (string, bool, error) {
	g.mu.Lock()
	g.now++
	g.most = max(g.most, g.now)
	gate := g.gate
	g.mu.Unlock()
	<-gate
	g.mu.Lock()
	g.now--
	g.mu.Unlock()
	return g.stillGit.LsRemote(ctx, dir, ref)
}

// TestFirstHeadsPassReadsRepositoriesAtOnce: the run waits for the first
// heads pass before its first tick, so that pass reads up to HeadsAtOnce
// repositories at once: ten slow repositories hold the first tick for two
// timeouts, not ten. Later passes read one repository at a time, as before.
func TestFirstHeadsPassReadsRepositoriesAtOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		streams := map[string]store.Stream{}
		for i := range 10 {
			name := fmt.Sprint("s", i)
			streams[name] = store.Stream{Name: name, Repo: fmt.Sprintf("https://example.invalid/r%d.git", i), Base: "main", Forge: "github"}
		}
		g := &slowGit{gate: make(chan struct{})}
		e := &Edges{Git: g, ReposDir: t.TempDir()}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		read := make(chan struct{})
		stopped := make(chan struct{})
		go func() {
			e.WatchHeads(ctx, &beats{now: t0}, func() map[string]store.Stream { return streams }, read)
			close(stopped)
		}()
		most := func() int {
			g.mu.Lock()
			defer g.mu.Unlock()
			return g.most
		}
		synctest.Wait() // the first pass's reads are all waiting on the gate
		if got := most(); got != HeadsAtOnce {
			t.Errorf("the first pass read %d repositories at once, want %d", got, HeadsAtOnce)
		}
		close(g.gate)
		<-read
		if got := len(e.Heads()); got != len(streams) {
			t.Fatalf("the first pass read %d heads, want %d", got, len(streams))
		}
		later := make(chan struct{})
		g.mu.Lock()
		g.most, g.gate = 0, later
		g.mu.Unlock()
		e.Stale()
		synctest.Wait() // the second pass's one read waits on the gate
		if got := most(); got != 1 {
			t.Errorf("a later pass read %d repositories at once, want 1", got)
		}
		close(later)
		cancel()
		<-stopped // the watch's lock files are in the test's directory
	})
}
