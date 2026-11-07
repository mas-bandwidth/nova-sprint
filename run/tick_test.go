package run

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Unit tests: store.Fake, a fake policy and a fake bus; no time, no socket.

type healthy struct{ err error }

func (h *healthy) Check(context.Context) error { return h.err }

// answering is a policy that deals every producer to every row of the
// roster, in id order.
type answering struct{ asks int }

func (a *answering) AnswerContext(_ context.Context, _ policy.Versions, snap store.Snapshot, ro roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure) {
	a.asks++
	names := slices.Sorted(maps.Keys(ro.Workers))
	out := policy.Answers{Order: map[string][]any{}, Rows: map[policy.RowsQuestion][]string{}, Width: map[string]policy.Width{},
		Limits:   policy.Limits{Bounces: 3, Faults: 3, SameFinding: 3, ReportsPerTick: 100, FleetWidthCap: 4, FlapLimit: 3, EscalateAfter: 2, FrontierAfter: 3},
		Timeouts: policy.Timeouts{RowSilentGone: 600, RowSilentWithdraw: 1800, LandingStall: 60, Judgement: 3600, Tick: 1, BaseRegate: []int{120}}}
	for _, id := range q.Order {
		out.Order[id] = []any{int64(0), id}
	}
	for _, r := range q.Rows {
		out.Rows[r] = names
	}
	for _, n := range names {
		out.Width[n] = policy.Width{Work: 1, Ahead: 1}
	}
	return out, nil
}

type sent struct{ to, subject, body string }

type fakeBus struct {
	sent []sent
	fail map[string]bool
	who  map[string]bus.Presence
}

func (b *fakeBus) Send(_ context.Context, to, subject, body string) (string, error) {
	if b.fail[to] {
		return "", errors.New("nova-bus exited 1")
	}
	b.sent = append(b.sent, sent{to, subject, body})
	return "1-1", nil
}
func (b *fakeBus) Who(context.Context) (map[string]bus.Presence, error) { return b.who, nil }

var t0 = time.Unix(1_800_000_000, 0).UTC()

func must(t *testing.T, st store.Store, id string, ws ...store.Write) {
	t.Helper()
	r, err := st.Apply(context.Background(), store.Op{ID: id, Epoch: 1, Actor: "test", Kind: "test", At: t0, Writes: ws})
	if err != nil || !r.Accepted {
		t.Fatalf("%s: %+v %v", id, r, err)
	}
}

// seeded is a started sprint with one stream, one producer and alice hooked
// in as a worker on the roster.
func seeded(t *testing.T) (*store.Fake, roster.Roster) {
	f := started(t)
	must(t, f, "stream", store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"})
	must(t, f, "row", store.PutRow{Name: "alice", Session: "sess-1", Role: "worker"})
	must(t, f, "up", store.SetRowState{Row: "alice", State: "up"})
	must(t, f, "card", store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p1", Paths: []string{"a"}})
	ro := roster.Roster{Workers: map[string]roster.Worker{"alice": {Name: "alice", Width: 1, Tiers: []string{"flash", "pro", "heavy", "frontier"}}}}
	return f, ro
}

func tickN(t *testing.T, tk *Ticker, ro roster.Roster, n int) {
	t.Helper()
	for i := range n {
		now := t0.Add(time.Duration(i+1) * time.Second)
		if _, err := tk.Tick(context.Background(), Observed{Now: now, Present: []string{"alice"}, Roster: ro, Heads: map[string]string{"s": strings.Repeat("a", 40)}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTickDealsAndPushesOneEnvelope(t *testing.T) {
	f, ro := seeded(t)
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	tickN(t, tk, ro, 3)
	snap := tk.Snapshot()
	var dealt []string
	for id, c := range snap.Cards {
		if c.Kind == "work" && c.Holder == "alice" && c.State == "working" {
			dealt = append(dealt, id)
		}
	}
	if len(dealt) != 1 {
		t.Fatalf("want one consumer dealt to alice, cards: %+v", snap.Cards)
	}
	b := &fakeBus{}
	if failed := tk.Push(context.Background(), b, t0); len(failed) != 0 {
		t.Fatalf("push failed for %v", failed)
	}
	if len(b.sent) != 1 || b.sent[0].to != "alice" || b.sent[0].subject != "DEAL" {
		t.Fatalf("envelopes: %+v", b.sent)
	}
	if err := tk.Fold(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := tk.Snapshot().Cursors["push:alice"]; got == 0 {
		t.Fatalf("cursor did not move after a confirmed send")
	}
	b.sent = nil
	if tk.Push(context.Background(), b, t0); len(b.sent) != 0 {
		t.Fatalf("a second push resent: %+v", b.sent)
	}
}

// TestTickRaisesSeenForAHookedRow is a row as the hook leaves it (PutRow
// inserts it down) and present on the bus: the first tick brings it up (the
// run loop's Seen) and the deal follows; a row down and absent from the bus
// stays down, and a row already up raises nothing.
func TestTickRaisesSeenForAHookedRow(t *testing.T) {
	f := started(t)
	must(t, f, "stream", store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"})
	must(t, f, "row", store.PutRow{Name: "alice", Session: "sess-1", Role: "worker"})
	must(t, f, "row2", store.PutRow{Name: "bob", Session: "sess-2", Role: "worker"})
	must(t, f, "card", store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p1", Paths: []string{"a"}})
	all := []string{"flash", "pro", "heavy", "frontier"}
	ro := roster.Roster{Workers: map[string]roster.Worker{"alice": {Name: "alice", Width: 1, Tiers: all}, "bob": {Name: "bob", Width: 1, Tiers: all}}}
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	tickN(t, tk, ro, 1)
	snap := tk.Snapshot()
	if a, b := snap.Rows["alice"].State, snap.Rows["bob"].State; a != "up" || b != "down" {
		t.Fatalf("after one tick: alice %s (want up), bob %s (want down)", a, b)
	}
	seq := snap.Seq
	tickN(t, tk, ro, 2)
	evs, err := f.Events(context.Background(), seq, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if !ev.Accepted {
			t.Fatalf("a later tick had a refused op: %+v", ev)
		}
	}
	var dealt []string
	for id, c := range tk.Snapshot().Cards {
		if c.Kind == "work" && c.State == "working" {
			dealt = append(dealt, id+" to "+c.Holder)
		}
	}
	if len(dealt) != 1 || !strings.HasSuffix(dealt[0], " to alice") {
		t.Fatalf("want p1 dealt to alice, got %v", dealt)
	}
}

func TestPushFailureLeavesTheCursor(t *testing.T) {
	f, ro := seeded(t)
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	tickN(t, tk, ro, 3)
	b := &fakeBus{fail: map[string]bool{"alice": true}}
	if failed := tk.Push(context.Background(), b, t0); len(failed) != 1 || failed[0].To != "alice" || failed[0].Err == nil {
		t.Fatalf("failed = %v", failed)
	}
	if err := tk.Fold(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := tk.Snapshot().Cursors["push:alice"]; got != 0 {
		t.Fatalf("cursor moved to %d after a failed send", got)
	}
	b.fail = nil
	if tk.Push(context.Background(), b, t0); len(b.sent) != 1 {
		t.Fatalf("the unmoved cursor is the resend: %+v", b.sent)
	}
}

func TestNoTickWhileStopped(t *testing.T) {
	f, ro := seeded(t)
	must(t, f, "stop", store.SetStopped{Stopped: true, Reason: "maintenance"})
	p := &answering{}
	tk, err := NewTicker(context.Background(), f, &healthy{}, p, t0)
	if err != nil {
		t.Fatal(err)
	}
	before := tk.Snapshot().Seq
	tickN(t, tk, ro, 3)
	if tk.Snapshot().Seq != before || p.asks != 0 {
		t.Fatalf("a stopped sprint ticked: seq %d -> %d, %d asks", before, tk.Snapshot().Seq, p.asks)
	}
}

func TestLostLockAppliesNothing(t *testing.T) {
	f, ro := seeded(t)
	h := &healthy{}
	tk, err := NewTicker(context.Background(), f, h, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	h.err = ErrLockLost
	before := tk.Snapshot().Seq
	if _, err := tk.Tick(context.Background(), Observed{Now: t0, Roster: ro}); !errors.Is(err, ErrLockLost) {
		t.Fatalf("err = %v", err)
	}
	if s, _ := f.Snapshot(context.Background()); s.Seq != before {
		t.Fatalf("applied after the lock was lost: %d -> %d", before, s.Seq)
	}
}

func TestEnvelopeIsBoundedWithNext(t *testing.T) {
	var q []pending
	for i := range 200 {
		q = append(q, pending{int64(i + 1), Notice{Subject: "DEAL", Line: strings.Repeat("x", 60)}})
	}
	subject, body := envelope("alice", q)
	if subject != "DEAL" || len(body) > bus.MaxMessageBytes || !strings.HasSuffix(body, "more: nova-sprint next --as alice") {
		t.Fatalf("subject %q, %d bytes, tail %q", subject, len(body), body[max(0, len(body)-60):])
	}
}

func TestLandResultMapping(t *testing.T) {
	cases := []struct {
		res  git.LandResult
		err  error
		want steps.InboxItem
	}{
		{git.LandResult{Outcome: git.OutcomeLanded, LandedHeads: []string{"h1"}}, nil, steps.InboxItem{Outcome: "landed", Landed: []string{"h1"}}},
		{git.LandResult{Outcome: git.OutcomeCIRed, BlamedHead: "h2", GreenHeads: []string{"h1"}}, nil, steps.InboxItem{Outcome: "red", Blamed: "h2"}},
		{git.LandResult{Outcome: git.OutcomeRung3Conflict}, nil, steps.InboxItem{Outcome: "conflict"}},
		{git.LandResult{Outcome: git.OutcomeLedgerFailed}, nil, steps.InboxItem{Outcome: "conflict"}},
		{git.LandResult{Outcome: git.OutcomePushRefused}, nil, steps.InboxItem{Outcome: "fault"}},
		{git.LandResult{Outcome: git.OutcomeMissingHead}, nil, steps.InboxItem{Outcome: "fault"}},
		{git.LandResult{}, errors.New("land refused"), steps.InboxItem{Outcome: "fault"}},
	}
	for _, c := range cases {
		got := landItem("land.s.x", c.res, c.err)
		c.want.Kind, c.want.Request = "land", "land.s.x"
		if got.Outcome != c.want.Outcome || got.Blamed != c.want.Blamed || !slices.Equal(got.Landed, c.want.Landed) || got.Request != c.want.Request {
			t.Fatalf("%v %v: got %+v want %+v", c.res.Outcome, c.err, got, c.want)
		}
	}
}

func TestStopIsOneMessageThenAck(t *testing.T) {
	b := &fakeBus{}
	e := &Edges{Bus: b}
	e.stop(context.Background(), steps.EdgeRequest{ID: "stop.1", Kind: "stop", Row: "alice", Consumer: "p1.w1"})
	got := e.Results()
	if len(b.sent) != 1 || b.sent[0].body != "STOP p1.w1" || len(got) != 1 || got[0].Outcome != "ack" || got[0].Request != "stop.1" {
		t.Fatalf("sent %+v results %+v", b.sent, got)
	}
	b.fail = map[string]bool{"alice": true}
	var said []string
	e.Fault = func(what string, err error) { said = append(said, what+": "+err.Error()) }
	e.stop(context.Background(), steps.EdgeRequest{ID: "stop.2", Kind: "stop", Row: "alice", Consumer: "p1.w1"})
	if got := e.Results(); len(got) != 0 {
		t.Fatalf("an unsent stop was acknowledged: %+v", got)
	}
	if len(said) != 1 || !strings.HasPrefix(said[0], "stop stop.2 to alice: ") {
		t.Fatalf("the unsent stop's cause was not said: %q", said)
	}
}

// beats is a clock whose beats the test sends.
type beats struct {
	now  time.Time
	tick chan time.Time
}
type beat struct{ c chan time.Time }

func (b beat) C() <-chan time.Time { return b.c }
func (beat) Stop()                 {}
func (c *beats) Now() time.Time    { return c.now }
func (c *beats) Every(d time.Duration) Beat {
	if d == TickEvery {
		return beat{c.tick}
	}
	return beat{make(chan time.Time)}
}

type stillGit struct{ git.Git }

func (stillGit) Clone(context.Context, string, string) error { return nil }
func (stillGit) LsRemote(context.Context, string, string) (string, bool, error) {
	return strings.Repeat("a", 40), true, nil
}

func TestServeTicksPushesAndLogs(t *testing.T) {
	f := started(t)
	must(t, f, "stream", store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"})
	must(t, f, "row", store.PutRow{Name: "bench", Session: "sess-1", Role: "worker"})
	must(t, f, "up", store.SetRowState{Row: "bench", State: "up"})
	must(t, f, "card", store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p1", Paths: []string{"a"}})
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
		done <- serve(ctx, Config{Store: f, Roster: ro, Bus: b, Policy: &answering{}, Git: stillGit{}, Clock: clock, Log: &log}, &healthy{})
	}()
	for range 3 {
		clock.tick <- t0
	}
	clock.tick <- t0 // the third tick has finished once the fourth beat is taken
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "TICK n=3") {
		t.Fatalf("log: %s", log.String())
	}
	if len(b.sent) == 0 || b.sent[0].to != "bench" {
		t.Fatalf("no envelope to bench: %+v\n%s", b.sent, log.String())
	}
}

// stopFirst commits a stop just before the first op it is asked to apply.
type stopFirst struct {
	*store.Fake
	done bool
}

func (s *stopFirst) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if !s.done && op.Actor != "test" {
		s.done = true
		if _, err := s.Fake.Apply(ctx, store.Op{ID: "midstop", Epoch: 1, Actor: "test", Kind: "test", At: t0, Writes: []store.Write{store.SetStopped{Stopped: true, Reason: "mid"}}}); err != nil {
			return store.Receipt{}, err
		}
	}
	return s.Fake.Apply(ctx, op)
}

func TestStopBetweenFoldAndApplyRefusesTheOp(t *testing.T) {
	f, ro := seeded(t)
	st := &stopFirst{Fake: f}
	tk, err := NewTicker(context.Background(), st, &healthy{}, &answering{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	tickN(t, tk, ro, 1)
	if !st.done || tk.Counts().Applied != 0 || tk.Counts().Refused == 0 {
		t.Fatalf("a tick op was accepted after the stop committed: %+v", tk.Counts())
	}
}
