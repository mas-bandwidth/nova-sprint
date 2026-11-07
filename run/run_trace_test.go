package run

// This fixture emits observations of the real chooser, scheduler, engine and
// fake store. The three-item FIFO and its budget are fixture-owned; they are
// not evidence about layer 3's carry implementation.
import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type traceRecorder struct {
	t           *testing.T
	lines       []string
	s           *traceStore
	e           *Engine
	carry       []int
	batch       []int
	putback     []int
	pusher      *Pusher
	pushCount   int
	process     string
	reportCard  int
	reportLate  bool
	expectedSeq int64
}

func (r *traceRecorder) add(action, fields string) {
	s, err := r.s.Store.Snapshot(context.Background())
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
func (r *traceRecorder) control(s store.Snapshot) {
	r.add("ReadControl", fmt.Sprintf(", p |-> %q, observedSeq |-> %d, observedStopped |-> %s", r.process, s.Seq, tlaBool(s.Stopped)))
}
func traceSeq(v []int) string {
	a := make([]string, len(v))
	for i, n := range v {
		a[i] = fmt.Sprint(n)
	}
	return "<<" + strings.Join(a, ",") + ">>"
}
func traceIDs(t *testing.T, ops []store.Op) []int {
	t.Helper()
	out := make([]int, 0, len(ops))
	for _, o := range ops {
		var id int
		if _, err := fmt.Sscanf(o.ID, "move-%d", &id); err != nil || id < 1 || id > 3 {
			t.Fatalf("unexpected planned operation %q", o.ID)
		}
		out = append(out, id)
	}
	return out
}
func (r *traceRecorder) rank(seq int64) int {
	r.t.Helper()
	ev, err := r.s.Store.Events(context.Background(), 0, 0)
	if err != nil {
		r.t.Fatal(err)
	}
	n := 0
	for _, e := range ev {
		if e.Seq <= seq && e.Accepted && (e.Kind == "trace-report" || e.Kind == "trace-move") {
			n++
		}
	}
	return n
}
func (r *traceRecorder) project() (string, string, string) {
	r.t.Helper()
	s, err := r.s.Store.Snapshot(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	gens := []int{}
	done := []int{}
	for i := 1; i <= 3; i++ {
		c := s.Cards[fmt.Sprintf("c%d", i)]
		gens = append(gens, c.Gen)
		if c.Reason == "move" {
			done = append(done, i)
		}
	}
	ev, err := r.s.Store.Events(context.Background(), 0, 0)
	if err != nil {
		r.t.Fatal(err)
	}
	records := []string{}
	reports := 0
	for _, e := range ev {
		if !e.Accepted {
			continue
		}
		switch e.Kind {
		case "trace-report":
			reports++
			records = append(records, fmt.Sprintf("[kind |-> \"report\", id |-> %d]", 3+reports))
		case "trace-move":
			var id int
			if _, err := fmt.Sscanf(e.Op, "move-%d", &id); err != nil {
				r.t.Fatal(err)
			}
			records = append(records, fmt.Sprintf("[kind |-> \"tick\", id |-> %d]", id))
		}
	}
	return traceSeq(gens), "{" + strings.Trim(traceSeq(done), "<>") + "}", "<<" + strings.Join(records, ",") + ">>"
}
func (r *traceRecorder) global() string {
	g, d, e := r.project()
	return fmt.Sprintf(", gens |-> %s, done |-> %s, events |-> %s", g, d, e)
}

type traceSession struct {
	r *traceRecorder
	fakeSession
}

func (s *traceSession) Try(ctx context.Context) (bool, error) {
	ok, e := s.fakeSession.Try(ctx)
	if ok && e == nil {
		s.r.add("Take", fmt.Sprintf(`, p |-> %q, lock |-> %q, hook |-> "none", procPhase |-> "hook"`, s.r.process, s.r.process))
	}
	return ok, e
}

type traceHook struct{ r *traceRecorder }

func (h traceHook) Acquire(_ context.Context, name string) (HookLease, error) {
	if name != "sprint" {
		return nil, fmt.Errorf("unexpected hook %s", name)
	}
	h.r.add("Hook", fmt.Sprintf(`, p |-> %q, lock |-> %q, hook |-> %q, procPhase |-> "ready"`, h.r.process, h.r.process, h.r.process))
	return &fakeLease{}, nil
}

type traceSteps struct{ r *traceRecorder }

func (s traceSteps) Tick(_ context.Context, snap store.Snapshot, _ TickInput) (TickPlan, error) {
	r := s.r
	r.carry = nil
	for i := 1; i <= 3; i++ {
		if snap.Cards[fmt.Sprintf("c%d", i)].Reason != "move" {
			r.carry = append(r.carry, i)
		}
	}
	gens := []int{snap.Cards["c1"].Gen, snap.Cards["c2"].Gen, snap.Cards["c3"].Gen}
	r.control(snap)
	r.add("Fold", fmt.Sprintf(`, p |-> %q, procCopy |-> %s, procFold |-> %d, procCarry |-> %s, procPhase |-> "select"`, r.process, traceSeq(gens), r.rank(snap.Seq), traceSeq(r.carry)))
	plan := TickPlan{}
	for _, j := range r.carry[:min(2, len(r.carry))] {
		id := fmt.Sprintf("move-%d", j)
		if r.process == "p2" {
			id += ".retry"
		}
		o := op(id, store.SetCardReason{Card: fmt.Sprintf("c%d", j), Reason: "move"})
		o.Kind = "trace-move"
		o.Expect = []store.Expect{store.CardGen{Card: fmt.Sprintf("c%d", j), Gen: gens[j-1]}}
		plan.Ops = append(plan.Ops, o)
	}
	return plan, nil
}

type traceStore struct {
	store.Store
	r                  *traceRecorder
	selected, reported bool
	sampled            int
}

func (s *traceStore) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	r := s.r
	if o.Kind == "trace-move" && !s.selected {
		if r.e.pending == nil || len(r.e.pending.plan.Ops) == 0 {
			r.t.Fatal("plan not installed")
		}
		r.batch = traceIDs(r.t, r.e.pending.plan.Ops)
		if fmt.Sprint(r.batch) != fmt.Sprint(r.carry[:len(r.batch)]) {
			r.t.Fatalf("unexpected FIFO batch %v", r.batch)
		}
		r.carry = append([]int(nil), r.carry[len(r.batch):]...)
		s.selected = true
		r.add("Select", fmt.Sprintf(`, p |-> %q, procBatch |-> %s, procCarry |-> %s, procPhase |-> "apply"`, r.process, traceSeq(r.batch), traceSeq(r.carry)))
	}
	if o.Kind == "trace-move" {
		if r.e.pending.next > 0 {
			r.control(r.e.state)
		}
		count := 0
		for _, cond := range o.Expect {
			if v, ok := cond.(store.AtSeq); ok {
				r.expectedSeq = v.Seq
				count++
			}
		}
		if count != 1 || !r.e.pending.fenced {
			r.t.Fatal("missing pending AtSeq fence")
		}
		r.add("CaptureFence", fmt.Sprintf(", p |-> %q, expectedSeq |-> %d, procFenced |-> TRUE", r.process, r.expectedSeq))
	}
	if o.Kind == "trace-move" && !s.reported && (!r.reportLate || r.e.pending.next == 1) {
		report := op("report-1", store.SetCardReason{Card: fmt.Sprintf("c%d", r.reportCard), Reason: "report"})
		report.Kind = "trace-report"
		receipt, err := s.Store.Apply(ctx, report)
		if err != nil || !receipt.Accepted {
			r.t.Fatalf("report %+v: %v", receipt, err)
		}
		s.reported = true
		r.add("Report", fmt.Sprintf(", c |-> %d", r.reportCard)+r.global())
	}
	return s.Store.Apply(ctx, o)
}
func (s *traceStore) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	r := s.r
	if s.selected && r.e.pending != nil && r.e.pending.next > s.sampled {
		p := r.e.pending
		receipt := p.receipts[s.sampled]
		j := r.batch[0]
		if receipt.Op != p.plan.Ops[s.sampled].ID || traceIDs(r.t, []store.Op{p.plan.Ops[s.sampled]})[0] != j {
			r.t.Fatalf("receipt/plan mismatch %+v", receipt)
		}
		r.batch = r.batch[1:]
		s.sampled++
		if receipt.Accepted {
			applied := 0
			for _, rec := range p.receipts {
				if rec.Accepted {
					applied++
				}
			}
			r.add("Apply", fmt.Sprintf(`, p |-> %q%s, procBatch |-> %s, procApplied |-> %d, expectedSeq |-> %d, receiptSeq |-> %d, procFenced |-> FALSE`, r.process, r.global(), traceSeq(r.batch), applied, r.expectedSeq, receipt.Seq))
		} else {
			r.putback = append(r.putback, j)
			r.add("Refuse", fmt.Sprintf(`, p |-> %q, procBatch |-> %s, procPutback |-> %s, expectedSeq |-> %d, receiptSeq |-> %d, procFenced |-> FALSE`, r.process, traceSeq(r.batch), traceSeq(r.putback), r.expectedSeq, receipt.Seq))
		}
	}
	return s.Store.Events(ctx, after, max)
}

func (s *traceStore) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	r := s.r
	if r.pusher != nil {
		if p, ok := r.pusher.pending["r1"]; ok && p.op.ID == id {
			r.add("SendOK", fmt.Sprintf(`, p |-> "p1", r |-> "r1", sent |-> [r \in {"r1"} |-> 1..%d], push |-> %s`, r.pushCount, tracePush("sent", 1, r.pushCount)))
		}
	}
	return s.Store.Replay(ctx, id)
}

func tracePush(stage string, lo, hi int) string {
	return fmt.Sprintf(`[p \in {"p1","p2"} |-> [r \in {"r1"} |-> IF p = "p1" THEN [stage |-> %q, lo |-> %d, hi |-> %d] ELSE [stage |-> "idle", lo |-> 0, hi |-> 0]]]`, stage, lo, hi)
}

type traceNotices struct{}

func (traceNotices) ForEvent(e store.Event, _ store.Snapshot, _ string) ([]Notice, error) {
	if e.Kind == "trace-report" || e.Kind == "trace-move" {
		return []Notice{{Subject: "TRACE", Line: e.Op}}, nil
	}
	return nil, nil
}

type traceSender struct{ r *traceRecorder }

func (s traceSender) Send(ctx context.Context, to, subject, body string) (Delivery, error) {
	r := s.r
	events, err := r.s.Store.Events(ctx, 0, 0)
	if err != nil {
		return Delivery{}, err
	}
	var lines []string
	for _, e := range events {
		if e.Accepted && (e.Kind == "trace-report" || e.Kind == "trace-move") {
			lines = append(lines, e.Op)
		}
	}
	if to != "r1" || subject != "TRACE" || body != strings.Join(lines, "\n") || len(lines) == 0 {
		return Delivery{}, fmt.Errorf("unexpected single-batch body %q", body)
	}
	r.pushCount = len(lines)
	r.add("PreparePush", fmt.Sprintf(`, p |-> "p1", r |-> "r1", push |-> %s`, tracePush("sending", 1, len(lines))))
	return Delivery{Recipient: to, ID: "1000-1"}, nil
}

type traceEdges struct{ r *traceRecorder }

func (e traceEdges) Committed(_ context.Context, _ TickPlan, _ []store.Receipt) error {
	r := e.r
	if r.e.pending != nil {
		return fmt.Errorf("pending not cleared")
	}
	refused := make([]store.Op, 0, len(r.e.refused))
	for _, v := range r.e.refused {
		refused = append(refused, v.Op)
	}
	observed := traceIDs(r.t, refused)
	if fmt.Sprint(observed) != fmt.Sprint(r.putback) {
		return fmt.Errorf("refused queue mismatch: %v vs %v", observed, r.putback)
	}
	r.carry = append(observed, r.carry...)
	r.putback = nil
	r.add("EndTick", fmt.Sprintf(`, p |-> %q, procCarry |-> %s, procBatch |-> %s, procPutback |-> %s, procPhase |-> "ready"`, r.process, traceSeq(r.carry), traceSeq(r.batch), traceSeq(r.putback)))
	return nil
}

func traceReportChoice(t *testing.T) (int, bool) {
	t.Helper()
	seedValue := int64(0)
	if raw := os.Getenv("NOVA_RUN_TRACE_SEED"); raw != "" {
		var e error
		seedValue, e = strconv.ParseInt(raw, 10, 64)
		if e != nil {
			t.Fatal(e)
		}
	}
	choice := rand.New(rand.NewSource(seedValue)).Intn(3)
	card, late := 1, false
	if choice > 0 {
		card = 2
	}
	if choice == 2 {
		late = true
	}
	t.Logf("fixture random seed=%d report card=%d between-ops=%t", seedValue, card, late)
	return card, late
}
func TestEmitRunTrace(t *testing.T) {
	ctx := context.Background()
	base := store.NewFake()
	seed := op("trace-seed", store.PutStream{Name: "s"}, store.PutCard{ID: "c1", Kind: "producer", Stream: "s", Tier: "flash"}, store.PutCard{ID: "c2", Kind: "producer", Stream: "s", Tier: "flash"}, store.PutCard{ID: "c3", Kind: "producer", Stream: "s", Tier: "flash"})
	receipt, err := base.Apply(ctx, seed)
	if err != nil || !receipt.Accepted {
		t.Fatalf("seed %+v %v", receipt, err)
	}
	card, late := traceReportChoice(t)
	r := &traceRecorder{t: t, process: "p1", reportCard: card, reportLate: late}
	s := &traceStore{Store: base, r: r}
	r.s = s
	initial, err := base.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.add("Init", fmt.Sprintf(`, lock |-> "none", hook |-> "none"%s, cursor |-> [r \in {"r1"} |-> %d], sent |-> [r \in {"r1"} |-> {}], edgeDone |-> {}, stopped |-> %s`, r.global(), r.rank(initial.Cursors["push:r1"]), tlaBool(initial.Stopped)))
	session := &traceSession{r: r, fakeSession: fakeSession{locked: true}}
	chooser, err := acquireChooser(ctx, session, traceHook{r: r})
	if err != nil {
		t.Fatal(err)
	}
	defer chooser.Close(ctx)
	engine, err := NewEngine(ctx, s, chooser, traceSteps{r: r}, traceEdges{r: r})
	if err != nil {
		t.Fatal(err)
	}
	r.e = engine
	scheduler, err := NewScheduler(engine, &fakeClock{at: time.Unix(1000, 0)}, &fakePresence{}, &fakePublisher{}, &fakeLogger{}, fakeResults{}, fakeDeadline{})
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("NOVA_RUN_TRACE_STOP") == "1" {
		traceStopCycle(t, ctx, r, scheduler)
	}
	if err = scheduler.Step(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if !s.reported || s.sampled != 2 || len(r.putback) != 0 {
		t.Fatalf("unexpected trace state: %+v", r)
	}
	pusher := NewPusher(s, traceNotices{}, traceSender{r: r})
	r.pusher = pusher
	snap := engine.State()
	if err = pusher.Push(ctx, "r1", snap, time.Unix(1001, 0)); err != nil {
		t.Fatal(err)
	}
	after, err := base.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Cursors["push:r1"] != snap.Seq || r.pushCount < 1 {
		t.Fatalf("push cursor=%d target=%d count=%d", after.Cursors["push:r1"], snap.Seq, r.pushCount)
	}
	r.add("SaveCursor", fmt.Sprintf(`, p |-> "p1", r |-> "r1", cursor |-> [r \in {"r1"} |-> %d], push |-> %s`, r.pushCount, tracePush("idle", 0, 0)))
	traceRestart(t, ctx, r, session, chooser, scheduler)
	writeRunTrace(t, r.lines)
}

func traceStopCycle(t *testing.T, ctx context.Context, r *traceRecorder, scheduler *Scheduler) {
	t.Helper()
	stop := op("stop", store.SetStopped{Stopped: true, Reason: "pause"})
	stop.Kind = "trace-stop"
	rec, err := r.s.Store.Apply(ctx, stop)
	if err != nil || !rec.Accepted {
		t.Fatalf("stop %+v %v", rec, err)
	}
	snap, err := r.s.Store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.add("Stop", fmt.Sprintf(", stopped |-> %s", tlaBool(snap.Stopped)))
	if err = scheduler.Step(ctx, nil); err != nil || !r.e.state.Stopped {
		t.Fatalf("stopped tick: %v", err)
	}
	r.control(r.e.state)
	report := op("report-1", store.SetCardReason{Card: "c1", Reason: "report"})
	report.Kind = "trace-report"
	rec, err = r.s.Store.Apply(ctx, report)
	if err != nil || !rec.Accepted {
		t.Fatalf("report %+v %v", rec, err)
	}
	r.s.reported = true
	r.add("Report", `, c |-> 1`+r.global())
	for i := 0; i < 2; i++ {
		if err = scheduler.Step(ctx, nil); err != nil || !r.e.state.Stopped {
			t.Fatalf("stopped fold: %v", err)
		}
		r.control(r.e.state)
	}
	start := op("start", store.SetStopped{Stopped: false, Reason: "resume"})
	start.Kind = "trace-start"
	rec, err = r.s.Store.Apply(ctx, start)
	if err != nil || !rec.Accepted {
		t.Fatalf("start %+v %v", rec, err)
	}
	snap, err = r.s.Store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r.add("Start", fmt.Sprintf(", stopped |-> %s", tlaBool(snap.Stopped)))
}
func writeRunTrace(t *testing.T, lines []string) {
	t.Helper()
	path := os.Getenv("NOVA_RUN_TRACE_PATH")
	if path == "" {
		path = filepath.Join(t.TempDir(), "RunTraceLog.tla")
	}
	content := "---------------- MODULE RunTraceLog ----------------\nEXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(lines, ",\n") + "\n>>\n====================================================\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("trace: %s records=%d", path, len(lines))
}

func traceRestart(t *testing.T, ctx context.Context, r *traceRecorder, session *traceSession, chooser *Chooser, scheduler *Scheduler) {
	t.Helper()
	session.locked = false
	session.checkErr = errors.New("fixture connection lost")
	if err := chooser.hook.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r.add("LoseLock", `, p |-> "p1", lock |-> "none", hook |-> "none", procPhase |-> "ready"`)
	before, err := r.s.Store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = scheduler.Step(ctx, nil); !errors.Is(err, ErrLockLost) {
		t.Fatalf("lost chooser: %v", err)
	}
	after, err := r.s.Store.Snapshot(ctx)
	if err != nil || before.Seq != after.Seq {
		t.Fatalf("write after loss: %+v %v", after, err)
	}
	if err = chooser.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r.add("NoticeLost", `, p |-> "p1", lock |-> "none", hook |-> "none", procPhase |-> "down"`)
	r.process = "p2"
	r.s.selected = false
	r.s.sampled = 0
	r.pusher = nil
	newSession := &traceSession{r: r, fakeSession: fakeSession{locked: true}}
	newChooser, err := acquireChooser(ctx, newSession, traceHook{r: r})
	if err != nil {
		t.Fatal(err)
	}
	defer newChooser.Close(ctx)
	engine, err := NewEngine(ctx, r.s, newChooser, traceSteps{r: r}, traceEdges{r: r})
	if err != nil {
		t.Fatal(err)
	}
	r.e = engine
	next, err := NewScheduler(engine, &fakeClock{at: time.Unix(1002, 0)}, &fakePresence{}, &fakePublisher{}, &fakeLogger{}, fakeResults{}, fakeDeadline{})
	if err != nil {
		t.Fatal(err)
	}
	if err = next.Step(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(r.carry) > 0 {
		r.s.selected = false
		r.s.sampled = 0
		if err = next.Step(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.carry) != 0 {
		t.Fatalf("restart did not complete work: carry %v", r.carry)
	}
}
