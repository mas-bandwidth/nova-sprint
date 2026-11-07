package run

// TestEmitRunTrace drives the real chooser (acquireChooser, Chooser.Check),
// the real cold start (coldStart: NewTicker, the policy seed, the pusher's
// catch-up), run.Ticker (Tick: health check, fold, the stopped gate, steps'
// real plan, apply in order, edge dispatch after acceptance) and run.Pusher
// (Push: one envelope, nova-bus send, then the cursor op) on store.Fake, a
// fake bus, a fake lock session and hook, and a fake policy, on a seeded
// random walk, and writes what it observed as a RunTraceLog module for
// tla/RunTrace.tla (tla/RunTrace-MAPPING.md, "The run.Ticker binding").
//
// Unit-test rules: no real time (the clock is a value), no socket, no
// network. The trace is written only when NOVA_RUN_TRACE_PATH is set.
//
// The fixture, and what it holds fixed so the walk is one the model can
// express (each restriction is listed in the mapping document):
//   - three tracked producers p1, p2, p3 (the model's jobs and cards 1..3),
//     ready at Init; p0 (flash) with its consumer w0 working on r1, the
//     preemption's victim; p9 (flash) with w9 working on r2, so the idle
//     alarm never fires; r2 serves only flash and has no room.
//   - the policy answers one deal of room on r1 per tick, so one tracked
//     op per tick: the model's Budget is 1.
//   - outside writes (a report, Stop, Start) land between ticks, or a report
//     lands between the fold and the apply on the pending op's own card
//     (refused by its gen fence). An outside write on another card or a
//     Stop between the fold and the apply is the NOVA_RUN_TRACE_MIDSTOP
//     case: the code accepts the op, and RunTrace refuses the trace there.
//   - p3 is raised to blocker only when it is the last job, so the deal
//     order stays the model's FIFO; it then preempts w0 and the tick
//     returns the stop request (DeliverEdge) only once that op is accepted.
//   - a push is attempted only when the last tracked event raised r1 a
//     notice (reports raise none), so the envelope covers the whole log.
import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// tracked are the model's jobs: job j is the deal of tracked[j-1], on card j.
var tracked = []string{"p1", "p2", "p3"}

func jobOf(card string) int { return slices.Index(tracked, card) + 1 }

type runTrace struct {
	t     *testing.T
	ctx   context.Context
	rnd   *rand.Rand
	base  *store.Fake
	lines []string
	at    time.Time

	proc    string // "p1" or "p2": the process holding (or last holding) the lock
	session *traceSession
	lease   *fakeLease
	chooser *Chooser
	ticker  *Ticker
	active  bool // the lock and hook are this process's (no loss injected)
	alive   bool // the process has not noticed a loss or crashed

	// the tick in flight
	inTick     bool
	folded     bool
	selected   bool
	queue      []int // the model's q at this tick's fold
	batch      []int
	putback    []int
	carry      []int
	applied    int
	expected   int64
	opJob      map[string]int // tick op id -> job
	midReport  bool           // report on the op's card between fold and apply
	midStop    bool           // the MIDSTOP case: Stop between fold and apply
	reports    int
	stops      int
	failures   int
	stopped    bool
	edgeDone   []int
	failSend   bool
	sentHi     int
	numberNext int // the next process name
}

func (r *runTrace) add(action, fields string) {
	r.t.Helper()
	s, err := r.base.Snapshot(r.ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	r.lines = append(r.lines, fmt.Sprintf("  [i |-> %d, action |-> %q, physicalSeq |-> %d%s]", len(r.lines)+1, action, s.Seq, fields))
}

func tlaBool(v bool) string {
	if v {
		return "TRUE"
	}
	return "FALSE"
}

func tlaSeq(v []int) string {
	a := make([]string, len(v))
	for i, n := range v {
		a[i] = strconv.Itoa(n)
	}
	return "<<" + strings.Join(a, ",") + ">>"
}

func tlaSet(v []int) string {
	s := slices.Clone(v)
	slices.Sort(s)
	return "{" + strings.Trim(tlaSeq(s), "<>") + "}"
}

// trackedEvent is whether an accepted event is one of the model's events: a
// tick op on a tracked card (the deal or the preemption) or a report.
func trackedEvent(e store.Event) (kind string, job int, ok bool) {
	if !e.Accepted {
		return "", 0, false
	}
	switch e.Kind {
	case "trace-report":
		return "report", 0, true
	case "tick":
		for _, g := range steps.OpGroups(store.Op{Note: e.Receipt.Note}) {
			for _, c := range g.Cards {
				if j := jobOf(c); j > 0 {
					return "tick", j, true
				}
			}
		}
	}
	return "", 0, false
}

func (r *runTrace) events() []store.Event {
	r.t.Helper()
	ev, err := r.base.Events(r.ctx, 0, 0)
	if err != nil {
		r.t.Fatal(err)
	}
	return ev
}

// rank is how many tracked events are at or before seq: the model's log
// index for a physical seq.
func (r *runTrace) rank(seq int64) int {
	n := 0
	for _, e := range r.events() {
		if _, _, ok := trackedEvent(e); ok && e.Seq <= seq {
			n++
		}
	}
	return n
}

func (r *runTrace) snap() store.Snapshot {
	r.t.Helper()
	s, err := r.base.Snapshot(r.ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

// gens are the tracked cards' gens; every tracked producer is added at gen 0
// in the fixture's setup and readied there, so the baseline is that gen.
func gensOf(s store.Snapshot, baseline map[string]int) []int {
	out := make([]int, len(tracked))
	for i, c := range tracked {
		out[i] = s.Cards[c].Gen - baseline[c]
	}
	return out
}

var traceBaseline map[string]int

// done is the jobs whose deal (or preemption) was accepted: the producer
// left ready.
func doneOf(s store.Snapshot) []int {
	var out []int
	for i, c := range tracked {
		if s.Cards[c].State != "ready" {
			out = append(out, i+1)
		}
	}
	return out
}

func (r *runTrace) global() string {
	s := r.snap()
	var recs []string
	reports := 0
	for _, e := range r.events() {
		kind, j, ok := trackedEvent(e)
		if !ok {
			continue
		}
		if kind == "report" {
			reports++
			recs = append(recs, fmt.Sprintf("[kind |-> \"report\", id |-> %d]", len(tracked)+reports))
		} else {
			recs = append(recs, fmt.Sprintf("[kind |-> \"tick\", id |-> %d]", j))
		}
	}
	return fmt.Sprintf(", gens |-> %s, done |-> %s, events |-> <<%s>>", tlaSeq(gensOf(s, traceBaseline)), tlaSet(doneOf(s)), strings.Join(recs, ","))
}

// pending is the model's q: the undone jobs in steps' deal order (the
// ladder's rung, then policy's order, which the fixture answers by id).
func pendingJobs(s store.Snapshot) []int {
	var out []int
	rungOf := func(c store.Card) int {
		if c.Priority == "blocker" {
			return 0
		}
		return 1
	}
	ids := slices.Clone(tracked)
	slices.SortStableFunc(ids, func(a, b string) int { return rungOf(s.Cards[a]) - rungOf(s.Cards[b]) })
	for _, c := range ids {
		if s.Cards[c].State == "ready" {
			out = append(out, jobOf(c))
		}
	}
	return out
}

// --- the lock session and the hook -------------------------------------

type traceSession struct {
	r *runTrace
	fakeSession
}

func (s *traceSession) Try(ctx context.Context) (bool, error) {
	ok, err := s.fakeSession.Try(ctx)
	if ok && err == nil {
		s.r.add("Take", fmt.Sprintf(`, p |-> %q, lock |-> %q, hook |-> "none", procPhase |-> "hook"`, s.r.proc, s.r.proc))
	}
	return ok, err
}

type traceHook struct {
	r    *runTrace
	fail bool
}

func (h *traceHook) Acquire(_ context.Context, name string) (HookLease, error) {
	if name != "sprint" {
		return nil, fmt.Errorf("unexpected hook %s", name)
	}
	if h.fail {
		return nil, errors.New("fixture: the name is held")
	}
	h.r.lease = &fakeLease{}
	return h.r.lease, nil
}

// --- the store: observes the tick's applies and injects mid-tick writes --

type traceStore struct {
	*store.Fake
	r *runTrace
}

func (s *traceStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	r := s.r
	switch op.Kind {
	case "tick":
		return r.applyTick(ctx, op)
	case "push-cursor":
		rec, err := s.Fake.Apply(ctx, op)
		if err == nil && rec.Accepted {
			hi := r.rank(op.Writes[0].(store.SetCursor).Seq)
			r.add("SaveCursor", fmt.Sprintf(`, p |-> %q, r |-> "r1", cursor |-> [x \in {"r1"} |-> %d], push |-> %s`, r.proc, hi, tracePush(r.proc, "idle", 0, 0)))
		}
		return rec, err
	}
	r.t.Fatalf("the ticker applied an op the model has no action for: %s (%s) %+v", op.ID, op.Kind, op.Writes)
	return store.Receipt{}, nil
}

func (r *runTrace) applyTick(ctx context.Context, op store.Op) (store.Receipt, error) {
	r.t.Helper()
	job := 0
	for _, g := range steps.OpGroups(op) {
		for _, c := range g.Cards {
			if j := jobOf(c); j > 0 {
				if job != 0 && job != j {
					r.t.Fatalf("op %s moves two tracked cards", op.ID)
				}
				job = j
			}
		}
	}
	if job == 0 || r.selected || len(r.queue) == 0 || r.queue[0] != job {
		r.t.Fatalf("op %s (job %d) is not the head of the model's queue %v (selected %v)", op.ID, job, r.queue, r.selected)
	}
	r.selected = true
	r.batch, r.carry = []int{job}, slices.Clone(r.queue[1:])
	r.add("Select", fmt.Sprintf(`, p |-> %q, procBatch |-> %s, procCarry |-> %s, procPhase |-> "apply"`, r.proc, tlaSeq(r.batch), tlaSeq(r.carry)))
	// The op is planned on one view; its id names that view's seq (steps
	// opID). The store does not check it (no AtSeq): see the mapping.
	parts := strings.SplitN(op.ID, ".", 3)
	seq, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || len(parts) != 3 {
		r.t.Fatalf("op id %s", op.ID)
	}
	r.expected = seq
	r.add("CaptureFence", fmt.Sprintf(", p |-> %q, expectedSeq |-> %d, procFenced |-> TRUE", r.proc, r.expected))
	if r.midReport {
		r.midReport = false
		r.report(tracked[job-1])
	}
	if r.midStop {
		r.midStop = false
		r.setStopped(true)
	}
	rec, err := r.base.Apply(ctx, op)
	if err != nil {
		return rec, err
	}
	r.batch = nil
	if rec.Accepted {
		r.applied++
		r.opJob[op.ID] = job
		r.add("Apply", fmt.Sprintf(`, p |-> %q%s, procBatch |-> <<>>, procApplied |-> %d, expectedSeq |-> %d, receiptSeq |-> %d, procFenced |-> FALSE`, r.proc, r.global(), r.applied, r.expected, rec.Seq))
	} else {
		r.putback = append(r.putback, job)
		r.add("Refuse", fmt.Sprintf(`, p |-> %q, procBatch |-> <<>>, procPutback |-> %s, expectedSeq |-> %d, receiptSeq |-> %d, procFenced |-> FALSE`, r.proc, tlaSeq(r.putback), r.expected, rec.Seq))
	}
	return rec, nil
}

// --- the policy: one deal of room on r1 per tick; observes the fold ------

type tracePolicy struct{ r *runTrace }

func (p tracePolicy) AnswerContext(_ context.Context, _ policy.Versions, snap store.Snapshot, _ roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure) {
	out := policy.Answers{Version: 1, Order: map[string][]any{}, Rows: map[policy.RowsQuestion][]string{}, Width: map[string]policy.Width{},
		Limits:   policy.Limits{Bounces: 3, Faults: 3, SameFinding: 2, ReportsPerTick: 100, RouteRestWindow: 10, RouteRestAfter: 3, FleetWidthCap: 4, FlapLimit: 3, EscalateAfter: 2, FrontierAfter: 3},
		Timeouts: policy.Timeouts{RowSilentGone: 600, RowSilentWithdraw: 1800, LandingStall: 60, Judgement: 3600, RouteRestFor: 1800, Tick: 1, ReadyTake: 180, BaseRegate: []int{120}}}
	for _, id := range q.Order {
		out.Order[id] = []any{int64(0), id}
	}
	for _, rq := range q.Rows {
		if snap.Cards[rq.Card].Tier == "flash" {
			out.Rows[rq] = []string{"r2"}
		} else {
			out.Rows[rq] = []string{"r1"}
		}
	}
	for name := range snap.Rows {
		live := 0
		for _, c := range snap.Cards {
			if c.Holder == name && (c.State == "ready" || c.State == "working") {
				live++
			}
		}
		if name == "r1" {
			out.Width[name] = policy.Width{Work: 1, Ahead: live} // room for one more deal
		} else {
			out.Width[name] = policy.Width{Work: 1, Ahead: 0}
		}
	}
	if p.r.inTick { // the tick's ask (the cold start's seed asks nothing)
		p.r.fold(snap)
	}
	return out, nil
}

// fold is steps.Tick's entry: the folded snapshot it plans on.
func (r *runTrace) fold(s store.Snapshot) {
	r.t.Helper()
	r.add("ReadControl", fmt.Sprintf(", p |-> %q, observedSeq |-> %d, observedStopped |-> %s", r.proc, s.Seq, tlaBool(s.Stopped)))
	if s.Stopped {
		r.t.Fatalf("steps was asked to plan on a stopped snapshot at seq %d", s.Seq)
	}
	r.queue, r.folded = pendingJobs(s), true
	r.add("Fold", fmt.Sprintf(`, p |-> %q, procCopy |-> %s, procFold |-> %d, procCarry |-> %s, procPhase |-> "select"`, r.proc, tlaSeq(gensOf(s, traceBaseline)), r.rank(s.Seq), tlaSeq(r.queue)))
}

// --- the bus: records the send -----------------------------------------

type traceBus struct{ r *runTrace }

func (b traceBus) Send(_ context.Context, to, subject, body string) (string, error) {
	r := b.r
	if to != "r1" {
		r.t.Fatalf("a notice for %s, who is not the model's recipient", to)
	}
	if r.failSend {
		// A failed send changes nothing the model holds: the cursor stays.
		r.add("Stutter", fmt.Sprintf(`, cursor |-> [x \in {"r1"} |-> %d]`, r.rank(r.snap().Cursors["push:r1"])))
		return "", errors.New("fixture: nova-bus exited 1")
	}
	q := r.ticker.push.queue["r1"]
	lo, hi := r.rank(r.snap().Cursors["push:r1"])+1, r.rank(q[len(q)-1].seq)
	r.add("PreparePush", fmt.Sprintf(`, p |-> %q, r |-> "r1", push |-> %s`, r.proc, tracePush(r.proc, "sending", lo, hi)))
	r.sentHi = hi
	r.add("SendOK", fmt.Sprintf(`, p |-> %q, r |-> "r1", sent |-> [x \in {"r1"} |-> 1..%d], push |-> %s`, r.proc, hi, tracePush(r.proc, "sent", lo, hi)))
	return "1000-1", nil
}

func (traceBus) Who(context.Context) (map[string]bus.Presence, error) { return nil, nil }

// tracePush is the push map with proc's slot at stage lo..hi and the other
// process's slot idle (a crash resets a process's slot; a restart leaves it
// idle: this fixture never leaves a send prepared).
func tracePush(proc, stage string, lo, hi int) string {
	return fmt.Sprintf(`[x \in {"p1","p2"} |-> [y \in {"r1"} |-> IF x = %q THEN [stage |-> %q, lo |-> %d, hi |-> %d] ELSE [stage |-> "idle", lo |-> 0, hi |-> 0]]]`, proc, stage, lo, hi)
}

// --- the walk's actions --------------------------------------------------

var traceRoster = roster.Roster{Workers: map[string]roster.Worker{
	"r1": {Name: "r1", Width: 1, Tiers: []string{"pro"}},
	"r2": {Name: "r2", Width: 1, Tiers: []string{"flash"}},
}}

func (r *runTrace) outside(id string, ws ...store.Write) store.Receipt {
	r.t.Helper()
	op := store.Op{ID: id, Epoch: 1, Actor: "person", Kind: "trace-report", At: r.at, Writes: ws}
	rec, err := r.base.Apply(r.ctx, op)
	if err != nil || !rec.Accepted {
		r.t.Fatalf("outside write %s: %+v %v", id, rec, err)
	}
	return rec
}

// report is a person's write on a tracked card: its gen moves, its state
// does not (the model's Report).
func (r *runTrace) report(card string) {
	r.t.Helper()
	r.reports++
	r.outside(fmt.Sprintf("report-%d", r.reports), store.SetCardProse{Card: card, Prose: fmt.Sprintf("report %d", r.reports)})
	r.add("Report", fmt.Sprintf(", c |-> %d", jobOf(card))+r.global())
}

func (r *runTrace) raise() {
	r.t.Helper()
	r.reports++
	r.outside(fmt.Sprintf("report-%d", r.reports), store.SetCardPriority{Card: "p3", Priority: "blocker"})
	r.add("Report", ", c |-> 3"+r.global())
}

func (r *runTrace) setStopped(stopped bool) {
	r.t.Helper()
	op := store.Op{ID: fmt.Sprintf("stop-%d-%t", r.stops, stopped), Epoch: 1, Actor: "person", Kind: "stop", At: r.at,
		Writes: []store.Write{store.SetStopped{Stopped: stopped, Reason: "fixture"}}}
	rec, err := r.base.Apply(r.ctx, op)
	if err != nil || !rec.Accepted {
		r.t.Fatalf("stop %+v %v", rec, err)
	}
	r.stopped = stopped
	action := "Start"
	if stopped {
		action = "Stop"
		r.stops++
	}
	r.add(action, ", stopped |-> "+tlaBool(stopped))
}

// start is a process taking the lock and the hook and cold-starting.
func (r *runTrace) start(hookFails bool) bool {
	r.t.Helper()
	r.proc = fmt.Sprintf("p%d", r.numberNext%2+1)
	r.numberNext++
	r.session = &traceSession{r: r, fakeSession: fakeSession{locked: true}}
	h := &traceHook{r: r, fail: hookFails}
	ch, err := acquireChooser(r.ctx, r.session, h)
	if hookFails {
		if err == nil || !r.session.closed {
			r.t.Fatalf("a failed hook left the chooser: %v", err)
		}
		r.failures++
		r.add("HookFailure", fmt.Sprintf(`, p |-> %q, lock |-> "none", hook |-> "none", procPhase |-> "down"`, r.proc))
		return false
	}
	if err != nil {
		r.t.Fatal(err)
	}
	r.add("Hook", fmt.Sprintf(`, p |-> %q, lock |-> %q, hook |-> %q, procPhase |-> "ready"`, r.proc, r.proc, r.proc))
	r.chooser = ch
	l, err := coldStart(r.ctx, Config{Store: &traceStore{Fake: r.base, r: r}, Policy: tracePolicy{r}, Bus: traceBus{r}, Git: stillGit{},
		Clock: &beats{now: r.at}, Log: io.Discard}, ch)
	if err != nil {
		r.t.Fatal(err)
	}
	r.ticker = l.ticker
	r.active, r.alive = true, true
	return true
}

func (r *runTrace) tick() {
	r.t.Helper()
	r.at = r.at.Add(time.Second)
	r.inTick, r.folded, r.selected, r.queue, r.batch, r.putback, r.carry, r.applied = true, false, false, nil, nil, nil, nil, 0
	reqs, err := r.ticker.Tick(r.ctx, Observed{Now: r.at, Present: []string{"r1", "r2"}, Roster: traceRoster,
		Heads: map[string]string{"s": strings.Repeat("a", 40)}})
	r.inTick = false
	if !r.active {
		if !errors.Is(err, ErrLockLost) || len(reqs) != 0 {
			r.t.Fatalf("a tick after the loss: %v %v", reqs, err)
		}
		_ = r.chooser.Close(r.ctx)
		r.alive = false
		r.add("NoticeLost", fmt.Sprintf(`, p |-> %q, lock |-> "none", hook |-> "none", procPhase |-> "down"`, r.proc))
		return
	}
	if err != nil {
		r.t.Fatal(err)
	}
	if !r.folded {
		// the stopped gate: Tick folded and returned before steps.
		s := r.ticker.Snapshot()
		if !s.Stopped || len(reqs) != 0 {
			r.t.Fatalf("a tick that never planned: stopped %v, requests %v", s.Stopped, reqs)
		}
		r.add("ReadControl", fmt.Sprintf(", p |-> %q, observedSeq |-> %d, observedStopped |-> TRUE", r.proc, s.Seq))
		return
	}
	if !r.selected {
		if len(r.queue) != 0 {
			r.t.Fatalf("steps planned nothing while the model's queue is %v", r.queue)
		}
		r.add("Select", fmt.Sprintf(`, p |-> %q, procBatch |-> <<>>, procCarry |-> <<>>, procPhase |-> "apply"`, r.proc))
	}
	r.carry = append(slices.Clone(r.putback), r.carry...)
	r.putback = nil
	r.add("EndTick", fmt.Sprintf(`, p |-> %q, procCarry |-> %s, procBatch |-> <<>>, procPutback |-> <<>>, procPhase |-> "ready"`, r.proc, tlaSeq(r.carry)))
	for _, e := range reqs {
		j, ok := r.opJob[e.After]
		if e.Kind != "stop" || !ok {
			r.t.Fatalf("an edge request the model has no job for: %+v", e)
		}
		r.edgeDone = append(r.edgeDone, j)
		r.add("DeliverEdge", fmt.Sprintf(", j |-> %d, edgeDone |-> %s", j, tlaSet(r.edgeDone)))
	}
}

// canPush: the envelope would cover the whole tracked log (see the top).
func (r *runTrace) canPush() bool {
	q := r.ticker.push.queue["r1"]
	if !r.active || len(q) == 0 {
		return false
	}
	last := r.rank(q[len(q)-1].seq)
	return last == r.rank(r.snap().Seq) && last > r.rank(r.snap().Cursors["push:r1"])
}

func (r *runTrace) push(fail bool) {
	r.t.Helper()
	r.failSend = fail
	failed := r.ticker.Push(r.ctx, traceBus{r}, r.at)
	r.failSend = false
	if fail != (len(failed) == 1) {
		r.t.Fatalf("push failed for %v (fixture fail=%v)", failed, fail)
	}
	if !fail && r.rank(r.snap().Cursors["push:r1"]) != r.sentHi {
		r.t.Fatalf("cursor at rank %d, sent through %d", r.rank(r.snap().Cursors["push:r1"]), r.sentHi)
	}
}

// --- setup and the walk --------------------------------------------------

func traceSetup(t *testing.T, f *store.Fake, at time.Time) {
	t.Helper()
	ctx := context.Background()
	apply := func(id string, ws ...store.Write) {
		rec, err := f.Apply(ctx, store.Op{ID: id, Epoch: 1, Actor: "setup", Kind: "setup", At: at, Writes: ws})
		if err != nil || !rec.Accepted {
			t.Fatalf("setup %s: %+v %v", id, rec, err)
		}
	}
	apply("setup-1",
		store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"},
		store.PutRow{Name: "r1", Session: "sess-1", Role: "worker"}, store.SetRowState{Row: "r1", State: "up"},
		store.PutRow{Name: "r2"}, store.SetRowState{Row: "r2", State: "up"})
	var ws []store.Write
	for i, c := range append(slices.Clone(tracked), "p0", "p9") {
		tier := "pro"
		if c == "p0" || c == "p9" {
			tier = "flash"
		}
		ws = append(ws, store.PutCard{ID: c, Kind: "producer", Stream: "s", Tier: tier, Brief: "brief " + c, Paths: []string{fmt.Sprintf("f%d", i)}},
			store.SetCardState{Card: c, State: "ready"})
	}
	apply("setup-2", ws...)
	apply("setup-3", store.SetCardState{Card: "p0", State: "working"}, store.SetCardState{Card: "p9", State: "working"},
		store.PutCard{ID: "w0", Kind: "work", Of: "p0", Stream: "s", Tier: "flash", Holder: "r1", Attempt: 1},
		store.PutCard{ID: "w9", Kind: "work", Of: "p9", Stream: "s", Tier: "flash", Holder: "r2", Attempt: 1})
	apply("setup-4", store.SetCardState{Card: "w0", State: "ready"}, store.SetCardState{Card: "w9", State: "ready"})
	apply("setup-5", store.SetCardState{Card: "w0", State: "working"}, store.SetCardState{Card: "w9", State: "working"})
	// steps opens policy.rows once (a ready producer the tick has no room
	// for is a rows miss); open it here, with no deadline, so no tick op
	// outside the model's jobs opens it.
	apply("setup-6", store.PutJudgement{ID: "policy.rows.1", Kind: "policy", About: "rows", Group: "policy:v1",
		Question: "fixture: policy v1 failed question rows", Command: "nova-sprint judge policy.rows.1 retry --reason <why>"})
	// r1's cursor past the setup: the setup's deal of w0 is not the model's.
	s, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	apply("setup-7", store.SetCursor{Name: "push:r1", Seq: s.Seq})
}

func traceSeed(t *testing.T) int64 {
	t.Helper()
	raw := os.Getenv("NOVA_RUN_TRACE_SEED")
	if raw == "" {
		return 0
	}
	seed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return seed
}

func TestEmitRunTrace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	seed := traceSeed(t)
	r := &runTrace{t: t, ctx: ctx, rnd: rand.New(rand.NewSource(seed)), base: store.NewFake(), at: time.Unix(1_800_000_000, 0).UTC(), opJob: map[string]int{}}
	traceSetup(t, r.base, r.at)
	s := r.snap()
	traceBaseline = map[string]int{}
	for _, c := range tracked {
		traceBaseline[c] = s.Cards[c].Gen
	}
	r.add("Init", fmt.Sprintf(`, lock |-> "none", hook |-> "none"%s, cursor |-> [x \in {"r1"} |-> 0], sent |-> [x \in {"r1"} |-> {}], edgeDone |-> {}, stopped |-> %s`, r.global(), tlaBool(s.Stopped)))
	r.start(false)

	switch {
	case os.Getenv("NOVA_RUN_TRACE_MIDSTOP") == "1":
		// The finding: a Stop between the tick's fold and its apply does not
		// stop the op (no AtSeq fence). RunTrace refuses the Apply record.
		r.midStop = true
		r.tick()
		writeRunTrace(t, r.lines)
		return
	case os.Getenv("NOVA_RUN_TRACE_STOP") == "1":
		// Stop, a stopped tick, a report while stopped, two stopped ticks
		// (the fold carries the stop past the report), Start.
		r.setStopped(true)
		r.tick()
		r.report("p1")
		r.tick()
		r.tick()
		r.setStopped(false)
	}
	r.walk()
	for len(pendingJobs(r.snap())) > 0 || !r.alive {
		r.finish()
	}
	if r.canPush() {
		r.push(false)
	}
	if got := doneOf(r.snap()); len(got) != len(tracked) {
		t.Fatalf("the walk ended with jobs undone: %v", got)
	}
	if len(r.edgeDone) != 1 {
		t.Fatalf("the walk ended without the preemption's stop request: %v", r.edgeDone)
	}
	writeRunTrace(t, r.lines)
}

// walk is the seeded random part. Reports are capped so the model's
// MaxReports (8) holds: at most 4 plain ones, a mid-tick one while fewer
// than 5 (7 once p3 is the blocker), and the raise of p3.
func (r *runTrace) walk() {
	for range 40 {
		if !r.alive {
			if r.failures < 6 && r.rnd.Intn(4) == 0 {
				r.start(true)
				continue
			}
			r.start(false)
			continue
		}
		left := pendingJobs(r.snap())
		if len(left) == 1 && left[0] == 3 && r.snap().Cards["p3"].Priority != "blocker" && !r.stopped {
			r.raise()
			continue
		}
		midCap := 5
		if r.snap().Cards["p3"].Priority == "blocker" {
			midCap = 7
		}
		switch n := r.rnd.Intn(20); {
		case n < 9:
			if r.active && !r.stopped && len(left) > 0 && r.reports < midCap && r.rnd.Intn(3) == 0 {
				r.midReport = true
			}
			r.tick()
			r.midReport = false
			if r.canPush() && r.rnd.Intn(2) == 0 {
				r.push(r.rnd.Intn(4) == 0)
			}
		case n < 11 && r.reports < 4:
			r.report(tracked[r.rnd.Intn(len(tracked))])
		case n < 13 && r.canPush():
			r.push(r.rnd.Intn(3) == 0)
		case n < 15 && r.stops < 3 && !r.stopped:
			r.setStopped(true)
		case n < 16 && r.stopped:
			r.setStopped(false)
		case n < 17 && r.active && r.failures < 6:
			r.lose(r.rnd.Intn(2) == 0)
		case n < 18 && r.failures < 6:
			r.crash()
		default:
			r.tick()
		}
	}
}

// finish drives the walk to the end: running, alive, every job dealt.
func (r *runTrace) finish() {
	switch {
	case !r.alive:
		r.start(false)
	case r.stopped:
		r.setStopped(false)
	default:
		left := pendingJobs(r.snap())
		if len(left) == 1 && left[0] == 3 && r.snap().Cards["p3"].Priority != "blocker" {
			r.raise()
			return
		}
		r.tick()
	}
}

// lose is the database session dying (and with it the hook, as the model's
// LoseLock has it) or only the hook dying (LoseHook); the process notices at
// its next Check.
func (r *runTrace) lose(lock bool) {
	r.t.Helper()
	r.failures++
	r.active = false
	r.lease.checkErr = errors.New("fixture: the hook exited")
	if lock {
		r.session.checkErr = errors.New("fixture: the connection was lost")
		r.add("LoseLock", fmt.Sprintf(`, p |-> %q, lock |-> "none", hook |-> "none", procPhase |-> "ready"`, r.proc))
		return
	}
	r.add("LoseHook", fmt.Sprintf(`, p |-> %q, lock |-> %q, hook |-> "none", procPhase |-> "ready"`, r.proc, r.proc))
}

// crash is the process dying: its session and hook go with it.
func (r *runTrace) crash() {
	r.t.Helper()
	r.failures++
	_ = r.chooser.Close(r.ctx)
	r.active, r.alive = false, false
	r.add("Crash", fmt.Sprintf(`, p |-> %q, lock |-> "none", hook |-> "none", procPhase |-> "down"`, r.proc))
}

func writeRunTrace(t *testing.T, lines []string) {
	t.Helper()
	path := os.Getenv("NOVA_RUN_TRACE_PATH")
	if path == "" {
		t.Logf("records=%d (set NOVA_RUN_TRACE_PATH to write the trace)", len(lines))
		return
	}
	content := "---------------- MODULE RunTraceLog ----------------\nEXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(lines, ",\n") + "\n>>\n====================================================\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("trace: %s records=%d", path, len(lines))
}
