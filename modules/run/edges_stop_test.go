package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// gateGit is a repository where the card's head holds the base tip and is
// not on it (rung 1: the card's own PR gates), and main is the default
// branch.
type gateGit struct {
	git.Git
	base, head string
}

func (gateGit) Clone(context.Context, string, string) error    { return nil }
func (gateGit) Fetch(context.Context, string, ...string) error { return nil }
func (g gateGit) IsAncestor(_ context.Context, _, ancestor, descendant string) (bool, error) {
	return ancestor == g.base && descendant == g.head, nil
}
func (g gateGit) LsRemote(_ context.Context, _, ref string) (string, bool, error) {
	if ref == "refs/heads/main" {
		return g.base, true, nil
	}
	return g.head, true, nil
}
func (g gateGit) Main(context.Context, string) (string, string, error) { return "main", g.base, nil }

// gateForge is a forge with the card's PR open at its head. The test sets
// what CI says; the forge counts every ask and records, for each merge,
// whether the sprint was stopped in the store when it was made.
type gateForge struct {
	forge.Forge
	head    string
	state   forge.CheckState
	stopped func() bool
	asks    int
	merges  []bool
}

func (f *gateForge) PR(context.Context, string, string, string) (forge.PRInfo, bool, error) {
	f.asks++
	return forge.PRInfo{Number: 7, HeadSHA: f.head}, true, nil
}
func (f *gateForge) Checks(_ context.Context, _ string, shas []string) (map[string]forge.CheckRollup, error) {
	f.asks++
	return map[string]forge.CheckRollup{shas[0]: {SHA: shas[0], State: f.state}}, nil
}
func (f *gateForge) Merge(context.Context, string, int, string) (string, error) {
	f.merges = append(f.merges, f.stopped())
	return strings.Repeat("e", 40), nil
}

// landingPolicy is answering with a landing answer for stream s.
type landingPolicy struct{ answering }

func (p *landingPolicy) AnswerContext(ctx context.Context, vs policy.Versions, snap store.Snapshot, ro roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure) {
	out, fs := p.answering.AnswerContext(ctx, vs, snap, ro, q)
	out.Landing = map[string]policy.Landing{"s": {BackedUp: 3, Flowing: 1, CIWait: 600}}
	return out, fs
}

// A stopped sprint moves nothing, a landing in flight included: with CI
// pending the sprint is stopped, CI turns green, and for as many polls as the
// stop lasts the landing asks the forge nothing and merges nothing, said once.
// After the start the same request merges and the card lands; no second
// request is issued. Each Pause is one LandPoll of the injected clock, and
// the run ticks once in each.
func TestStopHoldsALandingInFlight(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base, head := strings.Repeat("b", 40), strings.Repeat("a", 40)
	f := started(t)
	must(t, f, "stream", store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"})
	must(t, f, "row", store.PutRow{Name: "alice", Session: "sess-1", Role: "worker"})
	must(t, f, "up", store.SetRowState{Row: "alice", State: "up"})
	must(t, f, "card", store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p1", Paths: []string{"a"}})
	must(t, f, "merging", store.SetCardState{Card: "p1", State: "merging"}, store.SetCardHead{Card: "p1", Head: head},
		store.SetCardAuthor{Card: "p1", Author: "alice"}, store.SetCardAttempt{Card: "p1", Attempt: 1}, store.SetCardReview{Card: "p1"})
	ro := roster.Roster{Workers: map[string]roster.Worker{"alice": {Name: "alice", Width: 1, Tiers: []string{"flash", "pro", "heavy", "frontier"}}}}
	tk, err := NewTicker(ctx, f, &healthy{}, &landingPolicy{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	fg := &gateForge{head: head, state: forge.CheckPending, stopped: func() bool {
		s, err := f.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return s.Stopped
	}}
	var held []string
	e := &Edges{Git: gateGit{base: base, head: head}, Forge: fg, ReposDir: t.TempDir(),
		Stopped: func() bool { return tk.Snapshot().Stopped }, Held: func(what string) { held = append(held, what) }}
	var asked []steps.EdgeRequest // every request any tick dispatched
	ticks := 0
	tick := func(results ...steps.InboxItem) {
		t.Helper()
		ticks++
		reqs, err := tk.Tick(ctx, Observed{Now: t0.Add(time.Duration(ticks) * time.Second), Present: []string{"alice"}, Roster: ro, Heads: map[string]string{"s": base}, Results: results})
		if err != nil {
			t.Fatal(err)
		}
		asked = append(asked, reqs...)
	}
	tick()
	if len(asked) != 1 || asked[0].Kind != "land" {
		t.Fatalf("the first tick asked %+v, want one land request", asked)
	}
	const holdPolls = 20
	polls, asksAtStop, heldAsks, heldMerges := 0, 0, -1, -1
	e.Pause = func(context.Context, time.Duration) error {
		polls++
		switch {
		case polls == 1: // CI is pending: the sprint is stopped, the run folds the stop, CI turns green
			must(t, f, "stop", store.SetStopped{Stopped: true, Reason: "look first"})
			tick()
			fg.state, asksAtStop = forge.CheckGreen, fg.asks
		case polls == 1+holdPolls: // many polls later: the start
			heldAsks, heldMerges = fg.asks-asksAtStop, len(fg.merges)
			must(t, f, "start again", store.SetStopped{Stopped: false, Reason: "go"}) // its own op id: started's is "start"
			tick()
		case polls > 2+holdPolls:
			return context.Canceled // a landing that never ends fails below, it does not hang
		default:
			tick()
		}
		return nil
	}
	e.land(ctx, asked[0], tk.Snapshot().Streams["s"])
	if len(fg.merges) != 1 || fg.merges[0] {
		t.Fatalf("after %d polls: merges %v (true: made while the sprint was stopped), want one, made after the start", polls, fg.merges)
	}
	if polls != 1+holdPolls || heldAsks != 0 || heldMerges != 0 {
		t.Fatalf("held for %d polls with %d asks of the forge and %d merges, want %d polls, 0 and 0", polls-1, heldAsks, heldMerges, holdPolls)
	}
	if len(held) != 1 || held[0] != "land "+asked[0].ID+" stream s" {
		t.Fatalf("the hold was said %q, want once for the request", held)
	}
	res := e.Results()
	if len(res) != 1 || res[0].Request != asked[0].ID || res[0].Outcome != "landed" || !res[0].Main {
		t.Fatalf("results %+v, want the one request %s landed on main", res, asked[0].ID)
	}
	tick(res...)
	tick()
	if got := tk.Snapshot().Cards["p1"].State; got != "landed" || len(asked) != 1 {
		t.Fatalf("p1 is %s after %d requests %+v, want landed through the one request", got, len(asked), asked)
	}
}

// The run tells its edges the stop as it last folded it, from the cold
// start on: a fresh store is stopped, the first start clears it, a stop sets
// it again; and a held landing is one LAND HELD line in the run log.
func TestRunTellsItsEdgesTheStop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	must(t, f, "policy", activePolicy()...)
	ro, err := NewConfigRoster(&fakeConfigSnapshot{}, 8)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	l, err := coldStart(ctx, Config{Store: f, Roster: ro, Bus: &fakeBus{who: map[string]bus.Presence{}}, Policy: &answering{}, Git: stillGit{},
		ReposDir: t.TempDir(), Clock: &beats{now: t0}, Log: &log}, &healthy{})
	if err != nil {
		t.Fatal(err)
	}
	if l.edges.Stopped == nil || l.edges.Held == nil {
		t.Fatal("the run's edges are not told the stop")
	}
	if !l.edges.Stopped() {
		t.Fatal("a run up on a fresh store: its edges read running, want stopped")
	}
	for i, want := range []bool{false, true} {
		must(t, f, []string{"start", "stop"}[i], store.SetStopped{Stopped: want, Reason: "test"})
		if err := l.tick(ctx, i+1); err != nil {
			t.Fatal(err)
		}
		if got := l.edges.Stopped(); got != want {
			t.Fatalf("after tick %d the edges read stopped %v, want %v", i+1, got, want)
		}
	}
	l.edges.Held("land land.s.x stream s")
	if !strings.Contains(log.String(), "LAND HELD land land.s.x stream s: the sprint is stopped\n") {
		t.Fatalf("log: %s", log.String())
	}
}
