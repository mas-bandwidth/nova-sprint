package steps

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The trace walk: the Go steps driving one of tla/Steps.tla's scenarios on
// store.Fake, each step one of the model's actions (Snap, Tick, Take, Report,
// an environment move, an edge result), each recorded with the model's
// variables projected from the code's state (trace.go). tla/check-trace.sh
// runs TLC on the records: a trace the model refuses names the first record
// it cannot take. TestTraceWalk runs the walk for depth; TestTraceFixtures
// writes the fixed seed set CI checks (NOVA_TRACES=<dir>).

// modelDefaults sets the engine's escalation to the model's (EscalateAfter
// 1: the model bounds bounces at MaxBounce, so it escalates after one) for
// the test and restores it after; the trace tests are the only ones that
// write it, and top-level tests run one at a time.
func modelDefaults(t *testing.T) {
	was := Engine.EscalateAfter
	Engine.EscalateAfter = 1
	t.Cleanup(func() { Engine.EscalateAfter = was })
}

// traceScenarios are the model's six, with the constants its cfgs carry.
var traceScenarios = map[string]struct {
	budget, pack int
}{"pre": {2, 2}, "deal": {2, 2}, "land": {2, 2}, "burst": {1, 1}, "faults": {2, 2}, "spread": {2, 2}}

// traceFaultCap is the model's FaultCap: the walk's limits().faults.
const traceFaultCap = 2

// traceWalk is one scenario's world and its trace.
type traceWalk struct {
	*sim
	scen     string
	sc       TraceScenario
	rng      *rand.Rand
	records  []string
	stamp    map[string]int
	reqID    map[string]string
	phase    string // idle or snapped
	pref     string // the asker's preferred row, "" for failed
	raced    bool   // a move came after the fold
	truth    *View  // the store as it is, for the records; the tick's view (w.v) is folded only at Snap
	tcursor  int64
	counting bool // picks count toward the depth: a producer is not final
	picked   int
	legal    int
	heads    int
}

// traceAsker answers as the model's oracle: every question in domain from
// the engine's own defaults (so the steps use the version's answers, never a
// miss), rows as the walk prefers, or failed.
type traceAsker struct{ w *traceWalk }

func (a traceAsker) Ask(q Questions) Answers {
	w := a.w
	out := Answers{Version: 1, Order: map[string][]any{}, Rows: map[string][]string{}, Reads: map[string]int{}, Verdict: map[string]string{},
		Width: map[string]WidthAnswer{}, Landing: map[string]LandingAnswer{}, Failed: map[string]string{}, Models: benchModels()}
	for _, c := range q.Order {
		out.Order[c.ID] = []any{} // id order, as the model
	}
	for _, c := range q.Reads {
		out.Reads[c.ID] = 1
	}
	for _, v := range q.Verdict {
		out.Verdict[v.Card.ID] = "yes"
		if slices.ContainsFunc(v.Reads, func(r ReadFacts) bool { return r.Result == "fix" }) {
			out.Verdict[v.Card.ID] = "fix"
		}
	}
	for _, r := range q.Width {
		out.Width[r.Name] = WidthAnswer{Work: r.Width}
	}
	for _, s := range q.Landing {
		// above the scenario's producers: the model's requests are rung 1's
		out.Landing[s.Name] = LandingAnswer{BackedUp: 1000, Flowing: 1, CIWait: 600}
	}
	for _, r := range q.Rows {
		switch w.pref {
		case "":
			out.Failed["rows"] = "the walk's policy failed"
		case "both": // the model's "both": r1 then r2, of one rank in spread
			out.Rows[r.Card.ID] = []string{"r1", "r2"}
		default:
			out.Rows[r.Card.ID] = []string{w.pref}
		}
	}
	day := 86400
	out.Limits = &Limits{Bounces: 100, Faults: traceFaultCap, SameFinding: 10, ReportsPerTick: 10000, RouteRestWindow: 100, RouteRestAfter: 100,
		Budget: traceScenarios[w.scen].budget, Pack: traceScenarios[w.scen].pack}
	out.Timeouts = &Timeouts{RowSilentGone: day, RowSilentWithdraw: day, LandingStall: day, Judgement: day, RouteRestFor: day, BaseRegate: []int{day}}
	return out
}

func newTraceWalk(t *testing.T, scen string, seed int64) *traceWalk {
	w := &traceWalk{sim: newSim(t), scen: scen, sc: TraceScenario{Tiers: []string{"pro", "heavy"}}, rng: rand.New(rand.NewSource(seed)),
		stamp: map[string]int{}, reqID: map[string]string{}, phase: "idle"}
	w.ask = traceAsker{w}
	landLike := scen == "land" || scen == "burst"
	w.stream("s1")
	w.stream("s2")
	if scen == "spread" { // both rows of one rank, width 2
		w.row("r1", 2, "pro")
		w.row("r2", 2, "pro")
	} else {
		w.row("r1", 1, "pro")
		w.row("r2", 2, "pro", "heavy")
	}
	qStream, qPath := "s1", "a/z"
	if landLike {
		qStream = "s2"
	} else if scen == "deal" {
		qPath = "a/x"
	}
	w.producer("p", "s1", "pro", "a/x", "a/*") // the model's glob a/* both list: never an overlap
	q := store.PutCard{ID: "q", Kind: "producer", Stream: qStream, Tier: "pro", Brief: "do q", Paths: []string{qPath, "a/*"}}
	if landLike {
		q.After = []string{"p"}
	}
	if scen != "faults" { // faults is p alone
		w.must("add", q)
	}
	if !landLike && scen != "faults" && scen != "spread" {
		b := store.PutCard{ID: "b", Kind: "producer", Stream: "s2", Tier: "pro", Brief: "do b", Paths: []string{"a/y"}}
		if scen == "deal" {
			b.Tier, b.Needs = "heavy", []string{"p"}
		} else {
			b.Priority = "blocker"
		}
		w.must("add", b)
	}
	if landLike { // p and q merging at head 1, author r1, review 1
		for _, id := range []string{"p", "q"} {
			w.must("merging", store.SetCardState{Card: id, State: "merging"}, store.SetCardHead{Card: id, Head: Sha(1)},
				store.SetCardAuthor{Card: id, Author: "r1"}, store.SetCardAttempt{Card: id, Attempt: 1}, store.SetCardReview{Card: id})
		}
	}
	if scen == "pre" {
		w.must("r1 down", store.SetRowState{Row: "r1", State: "down"})
		w.must("s2 held", store.SetStreamHeld{Stream: "s2", Held: true})
	}
	w.fold()
	w.sc.Gen0 = map[string]int{}
	for n, r := range w.v.Rows {
		w.sc.Gen0[n] = r.Gen
	}
	w.record("Init", nil)
	return w
}

// truthView is the store as it is now, folded from every event (the tick's
// own view, w.v, stays as the last Snap left it, so a tick plans on a stale
// view exactly as the model's does).
func (w *traceWalk) truthView() *View {
	w.t.Helper()
	if w.truth == nil {
		w.truth = Build(store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Rows: map[string]store.Row{}, Judgements: map[string]store.Judgement{}}, t0)
	}
	for {
		es, err := w.f.Events(w.t.Context(), w.tcursor, 0)
		require.NoError(w.t, err)
		if len(es) == 0 {
			return w.truth
		}
		w.truth.Fold(es)
		w.tcursor = es[len(es)-1].Seq
	}
}

// record appends one record: the action, its arguments and the model's
// variables projected from the store as the fake holds it now.
func (w *traceWalk) record(action string, args []string) {
	w.t.Helper()
	v := w.truthView()
	fields := []string{fmt.Sprintf("i |-> %d", len(w.records)+1), fmt.Sprintf("action |-> %q", action)}
	fields = append(fields, args...)
	fields = append(fields, StoreTLA(v, w.sc), "inbox |-> "+InboxTLA(w.inbox, w.stamp, w.reqID),
		"inflight |-> "+InflightTLA(w.inflight, w.reqID, w.sent), "js |-> "+JudgementsTLA(v, w.sc))
	w.records = append(w.records, "["+strings.Join(fields, ", ")+"]")
}

// asked says whether a question has subjects now (the model's Asked): a
// ready producer, a read owed (no read of the review live or said ok or
// fix, a faulted one owed again, until the faults since the coordinator's
// read answer reach the cap).
func (w *traceWalk) asked() bool {
	for _, c := range w.v.Cards {
		if isConsumer(c) {
			continue
		}
		if c.State == "ready" && c.Kind == "producer" {
			return true
		}
		if c.State == "review" && w.owed(c) {
			return true
		}
	}
	return false
}

// owed is the model's ReadOwed on the walk's view.
func (w *traceWalk) owed(c store.Card) bool {
	var since time.Time
	for _, j := range w.v.Judgements {
		if j.Kind == "read-faults" && j.About == c.ID && j.Answer == "read" && j.ClosedAt.After(since) {
			since = j.ClosedAt
		}
	}
	faults := 0
	for _, r := range w.v.Consumers(c.ID) {
		if r.Kind != "read" || r.Review != c.Review {
			continue
		}
		fault := r.State == "done" && r.Result == "fault"
		if r.State != "fail" && !fault {
			return false
		}
		if fault && !w.v.TakenAt[r.ID].Before(since) {
			faults++
		}
	}
	return faults < traceFaultCap
}

// step takes one model action at random: in idle Snap or a take; in snapped
// Tick; reports, environment moves and edge results in either phase. Picks
// count toward the depth only while a producer is not final (the scenario's
// world is three cards; once every one has landed nothing is legal but the
// tick).
func (w *traceWalk) step() {
	w.t.Helper()
	w.counting = false
	for _, c := range w.v.Cards {
		if !isConsumer(c) && c.State != "landed" && c.State != "fail" {
			w.counting = true
		}
	}
	var moves []func()
	if w.phase == "idle" {
		moves = append(moves, w.snapAction, w.snapAction)
	} else {
		moves = append(moves, w.tickOnce, w.tickOnce)
	}
	blind := w.rng.Intn(4) == 0 // one pick in four is blind, so refusals are met too
	if blind || len(w.consumersIn("working")) > 0 {
		moves = append(moves, w.reportOne)
	}
	if blind || slices.ContainsFunc(w.inflight, func(r EdgeRequest) bool { _, sent := w.sent[r.ID]; return !sent }) {
		moves = append(moves, w.edgeOne)
	}
	if blind || slices.Contains([]string{"pre", "deal", "land"}, w.scen) {
		moves = append(moves, w.envOne)
	}
	if len(w.openOf("land")) > 0 { // the coordinator answers only what is open
		moves = append(moves, w.answerOne)
	}
	if len(w.openOf("read-faults")) > 0 {
		moves = append(moves, w.answerFaults)
	}
	moves[w.rng.Intn(len(moves))]()
}

func (w *traceWalk) snapAction() {
	w.fold()
	w.phase, w.raced = "snapped", false
	w.record("Snap", nil)
}

func (w *traceWalk) tickOnce() {
	w.t.Helper()
	answer := "failed"
	if w.asked() && w.pref != "" {
		answer = w.pref
	}
	w.planTick()
	if !w.raced {
		require.Empty(w.t, w.rejected, "%s: an op refused with no race: %+v", w.scen, w.rejected)
	}
	for _, e := range w.last.Edges {
		w.reqID[e.ID] = ReqID(w.v, e)
	}
	w.phase = "idle"
	w.record("Tick", []string{fmt.Sprintf("answer |-> [rows |-> %q]", answer)})
	prefs := []string{"r1", "r2", ""}
	if w.scen == "spread" {
		prefs = append(prefs, "both")
	}
	w.pref = prefs[w.rng.Intn(len(prefs))]
}

func (w *traceWalk) moved(action string, args []string) {
	if w.phase == "snapped" {
		w.raced = true
	}
	w.record(action, args)
}

// The walk has no take: the deal is the take (defect 52), and a row's
// acknowledgement moves no card, so the model has no action for it.

// results are the scenario's result sets: work ok (and fail for p in deal),
// read ok (and fix for p in deal, fault in faults).
func (w *traceWalk) results(c store.Card) []string {
	bounce := w.scen == "deal" && c.Of == "p"
	switch {
	case c.Kind == "work" && bounce:
		return []string{"ok", "fail"}
	case c.Kind == "work":
		return []string{"ok"}
	case bounce:
		return []string{"ok", "fix"}
	case w.scen == "faults":
		return []string{"ok", "fault"}
	}
	return []string{"ok"}
}

func (w *traceWalk) reportOne() {
	ids := w.consumersIn("working")
	w.pick()
	if len(ids) == 0 {
		return
	}
	id := ids[w.rng.Intn(len(ids))]
	c := w.snap().Cards[id]
	results := w.results(c)
	res := results[w.rng.Intn(len(results))]
	head := c.Head
	if c.Kind == "work" {
		head = ""
		if res == "ok" {
			head = Sha(c.Attempt)
		}
	}
	pl, r := w.reportPlan(id, res, head, res+" by the walk")
	if r != nil || !w.apply("report", pl.Expect, pl.Writes...).Accepted {
		return
	}
	w.ok()
	w.moved("Report", []string{"card |-> " + SlotTLA(w.view(), id), fmt.Sprintf("res |-> %q", res)})
}

// envOne is the scenario's environment: pre lifts s2's hold and flips r2;
// deal flips r1; land holds and lifts s1; burst has none.
func (w *traceWalk) envOne() {
	w.pick()
	s := w.snap()
	switch w.scen {
	case "pre":
		if w.rng.Intn(2) == 0 && s.Streams["s2"].Held {
			w.unhold("s2")
			return
		}
		w.flip("r2")
	case "deal":
		w.flip("r1")
	case "land":
		if s.Streams["s1"].Held {
			w.unhold("s1")
		} else {
			w.hold("s1")
		}
	}
}

func (w *traceWalk) hold(stream string) {
	s := w.snap()
	pl, r := card.MoveStream(card.StreamFromStore(s.Streams[stream]), card.HoldStream{Reason: "walk", Ready: w.readyOf(stream), Seq: s.Seq})
	if r == nil && w.apply("hold", pl.Expect, pl.Writes...).Accepted {
		w.ok()
		w.moved("Hold", nil)
	}
}

func (w *traceWalk) unhold(stream string) {
	pl, r := card.MoveStream(card.StreamFromStore(w.snap().Streams[stream]), card.UnholdStream{Reason: "walk"})
	if r == nil && w.apply("unhold", pl.Expect, pl.Writes...).Accepted {
		w.ok()
		w.moved("Unhold", nil)
	}
}

func (w *traceWalk) flip(row string) {
	r := w.cardRow(w.snap().Rows[row])
	var e card.Event = card.Gone{}
	action := "Gone"
	if r.State == "down" {
		e, action = card.Seen{}, "Seen"
	}
	pl, ref := card.MoveRow(r, e)
	if ref == nil && w.apply("row", pl.Expect, pl.Writes...).Accepted {
		w.ok()
		w.moved(action, nil)
	}
}

func (w *traceWalk) readyOf(stream string) []card.Card {
	var out []card.Card
	for _, id := range sortedKeys(w.snap().Cards) {
		if c := w.snap().Cards[id]; c.Kind == "producer" && c.Stream == stream && c.State == "ready" {
			out = append(out, card.FromStore(c))
		}
	}
	return out
}

// edgeOne answers a request in flight not yet answered, once or twice.
func (w *traceWalk) edgeOne() {
	w.pick()
	var open []EdgeRequest
	for _, r := range w.inflight {
		if _, sent := w.sent[r.ID]; !sent {
			open = append(open, r)
		}
	}
	if len(open) == 0 {
		return
	}
	req := open[w.rng.Intn(len(open))]
	it := InboxItem{Kind: req.Kind, Request: req.ID, Outcome: "ack"}
	if req.Kind == "land" {
		it.Outcome, it.Landed = "landed", req.Heads
		if (w.scen == "land" || w.scen == "burst") && w.rng.Intn(2) == 0 {
			it.Outcome, it.Landed = "red", nil
		}
	}
	n := 1 + w.rng.Intn(2)
	for i := 0; i < n; i++ {
		w.arrive(it)
		w.stamp[w.inbox[len(w.inbox)-1].ID] = w.arrived
	}
	w.ok()
	w.moved("EdgeResult", []string{"id |-> " + w.reqID[req.ID], fmt.Sprintf("out |-> %q", it.Outcome), fmt.Sprintf("n |-> %d", n)})
}

// openOf is the producers with an open judgement of kind, by id.
func (w *traceWalk) openOf(kind string) []string {
	js := w.truthView().Judgements
	var out []string
	for _, id := range sortedKeys(js) {
		if j := js[id]; j.Kind == kind && j.State == "open" {
			out = append(out, j.About)
		}
	}
	return out
}

// answerOne is the coordinator's answer to a land judgement (the model's
// Answer): land, fix or retry, one verb op.
func (w *traceWalk) answerOne() {
	ps := w.openOf("land")
	w.pick()
	if len(ps) == 0 {
		return
	}
	p := ps[w.rng.Intn(len(ps))]
	choice := []string{"land", "fix", "retry"}[w.rng.Intn(3)]
	r := Request{Actor: "rowan", Judgement: "land." + p + "." + fmt.Sprint(w.episodes("land", p)), Answer: choice}
	head := w.snap().Cards[p].Head
	switch choice {
	case "land":
		r.Card, r.Event = p, card.Landed{Head: head, Merge: Sha(90), Reason: "by hand: the walk"}
	case "fix":
		r.Card, r.Event = p, card.Red{Head: head, Reason: "the walk's finding"}
	}
	r.Coordinators = []string{"rowan"}
	op, ref := Verb(Build(w.snap(), w.now), Inputs{Now: w.now}, r)
	if ref != nil || !w.apply("answer", op.Expect, op.Writes...).Accepted {
		return
	}
	w.ok()
	w.moved("Answer", []string{fmt.Sprintf("p |-> %q", p), fmt.Sprintf("choice |-> %q", choice)})
}

// answerFaults is the coordinator's answer to a read-faults judgement (the
// model's AnswerFaults): read (the answer alone), yes (JudgedYes) or fix
// (JudgedFix), one verb op; the machine refuses yes and fix unless a read
// faulted and none said ok or fix.
func (w *traceWalk) answerFaults() {
	ps := w.openOf("read-faults")
	w.pick()
	if len(ps) == 0 {
		return
	}
	p := ps[w.rng.Intn(len(ps))]
	choice := []string{"read", "yes", "fix"}[w.rng.Intn(3)]
	r := Request{Actor: "rowan", Judgement: "read-faults." + p + "." + fmt.Sprint(w.episodes("read-faults", p)), Answer: choice}
	switch choice {
	case "yes":
		r.Card, r.Event = p, card.JudgedYes{Reason: "the walk read it: sound"}
	case "fix":
		r.Card, r.Event = p, card.JudgedFix{Finding: "a/x:1 the walk's finding"}
	}
	r.Coordinators = []string{"rowan"}
	op, ref := Verb(Build(w.snap(), w.now), Inputs{Now: w.now}, r)
	if ref != nil || !w.apply("answer", op.Expect, op.Writes...).Accepted {
		return
	}
	w.ok()
	w.moved("AnswerFaults", []string{fmt.Sprintf("p |-> %q", p), fmt.Sprintf("choice |-> %q", choice)})
}

// episodes is how many judgements of kind p has had (the open one is the last).
func (w *traceWalk) episodes(kind, about string) int {
	n := 0
	for _, j := range w.truthView().Judgements {
		if j.Kind == kind && j.About == about {
			n++
		}
	}
	return n
}

func (w *traceWalk) view() *View { return w.truthView() }

func (w *traceWalk) pick() {
	if w.counting {
		w.picked++
	}
}

func (w *traceWalk) ok() {
	if w.counting {
		w.legal++
	}
}

func (w *traceWalk) consumersIn(state string) []string {
	var out []string
	for _, c := range w.snap().Cards {
		if isConsumer(c) && c.State == state {
			out = append(out, c.ID)
		}
	}
	slices.Sort(out)
	return out
}

// module is the StepsTraceLog module of the records, and cfg the trace cfg:
// the scenario's constants, the model's bounds above anything the walk
// reaches.
func (w *traceWalk) module() string {
	return "---- MODULE StepsTraceLog ----\n\\* Written by the steps trace walk (steps/trace_test.go); do not edit.\nEXTENDS TLC\n\nTraceLog == <<\n  " +
		strings.Join(w.records, ",\n  ") + "\n>>\n====\n"
}

func (w *traceWalk) cfg() string {
	c := traceScenarios[w.scen]
	return fmt.Sprintf("\\* Written by the steps trace walk; do not edit.\n\\* expect: matched\nCONSTANT Scenario = %q\nCONSTANT K = 60\nCONSTANT MaxAtt = 30\nCONSTANT MaxBounce = 30\nCONSTANT MaxEnv = 1000\nCONSTANT MaxArrive = 1000\nCONSTANT Budget = %d\nCONSTANT Pack = %d\nSPECIFICATION TraceSpec\nPROPERTY TraceMatched\nCHECK_DEADLOCK FALSE\n", w.scen, c.budget, c.pack)
}

// TestTraceWalk: every scenario, 100 seeds, 2,000 steps; at least 40% of the
// picked moves legal; no op refused without a race.
func TestTraceWalk(t *testing.T) {
	modelDefaults(t)
	steps, seeds := 2000, int64(100)
	if testing.Short() {
		steps, seeds = 300, 10
	}
	for scen := range traceScenarios {
		t.Run(scen, func(t *testing.T) {
			var mu sync.Mutex
			picked, legal := 0, 0
			for seed := int64(1); seed <= seeds; seed++ {
				t.Run(fmt.Sprint(seed), func(t *testing.T) {
					t.Parallel()
					w := newTraceWalk(t, scen, seed)
					for i := 0; i < steps; i++ {
						w.step()
					}
					mu.Lock()
					defer mu.Unlock()
					picked += w.picked
					legal += w.legal
				})
			}
			t.Cleanup(func() {
				t.Logf("%s: %d picked, %d legal (%.0f%%)", scen, picked, legal, 100*float64(legal)/float64(max(picked, 1)))
				assert.GreaterOrEqual(t, float64(legal), 0.4*float64(picked), "depth")
			})
		})
	}
}

// TestTraceFixtures writes the fixed seed set's traces under NOVA_TRACE_DIR
// (steps-walk-<scenario>-<seed>), for tla/make-traces.sh and check-trace.sh;
// without it the walk runs and writes nothing.
func TestTraceFixtures(t *testing.T) {
	modelDefaults(t)
	dir := os.Getenv("NOVA_TRACE_DIR") // make-traces.sh's, absolute
	if dir != "" && !filepath.IsAbs(dir) {
		dir = filepath.Join("..", dir) // relative to the repository root, as CI gives it
	}
	for _, scen := range []string{"pre", "deal", "land", "burst", "faults", "spread"} {
		for seed := int64(1); seed <= 3; seed++ {
			w := newTraceWalk(t, scen, seed)
			for i := 0; i < 150; i++ {
				w.step()
			}
			if dir == "" {
				continue
			}
			d := filepath.Join(dir, fmt.Sprintf("steps-walk-%s-%d", scen, seed))
			require.NoError(t, os.MkdirAll(d, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(d, "StepsTraceLog.tla"), []byte(w.module()), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(d, "StepsTrace.cfg"), []byte(w.cfg()), 0o644))
			t.Logf("%s: %d records", d, len(w.records))
		}
	}
}
