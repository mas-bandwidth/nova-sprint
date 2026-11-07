package steps

// Cold read of layer 3 at 108e508a (rowan's cold reader, 2026-11-07), copied
// in as the probes of round 2: each TestColdreadDefect* is one defect, the
// sequence that shows it and the design line it breaks. Every one failed at
// 108e508a. Two setups are adapted to store round 22 (rows carry no width,
// tiers or model; the roster does): TestColdreadDefectReadIgnoresModel's
// row r2, and nothing else.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crMerging drives p (alone on rows r1 pro and r2 pro+heavy) to merging.
func crMerging(t *testing.T, s *sim, p string) {
	t.Helper()
	s.tick()
	s.tick()
	s.take(p + ".w1")
	s.report(p+".w1", "ok", sha(1), "done")
	s.tick()
	s.take(p + ".r1.1")
	s.report(p+".r1.1", "ok", sha(1), "fine")
	s.tick()
	s.requireState(p, "merging")
}

// crWrites is every write of the plan's ops.
func crWrites(p Plan) []store.Write {
	var out []store.Write
	for _, op := range p.Ops {
		out = append(out, op.Writes...)
	}
	return out
}

// Reports: "for the current work consumer of a working producer the same
// group moves the producer (Finished, Broken, Faulted ...), so no producer
// is left working under a reported consumer"; Requests: "Take and Report are
// the holder's"; LAYER-6 "Reports as ops": the card machine's plan for
// consumer and producer together; guarantee 9 (nothing stalls). Verb's
// Report moves the consumer alone and no tick step ever moves the producer:
// p1 is working forever under a done consumer.
func TestColdreadDefectVerbReportLeavesProducerWorking(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	rc, ref := s.verb(Request{Actor: "r1", Card: "p1.w1", Event: card.Take{Row: "r1"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	rc, ref = s.verb(Request{Actor: "r1", Card: "p1.w1", Event: card.Report{Row: "r1", Result: "ok", Head: sha(1), Reason: "done"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	s.run(5)
	s.requireState("p1.w1", "done")
	assert.Equal(t, "review", s.state("p1"), "a work report through the verb must move its producer in the same op (or a tick must)")
}

// Requests: the actor rule; "a verb refused for its actor writes nothing".
// Engine moves (Block, Landed, Start, Resolve, ReadYes ...) are no actor's
// verb, yet Verb hands any actor an op for them: a worker lands a merging
// card with no landing behind it.
func TestColdreadDefectVerbEngineMoveByAnyActor(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	crMerging(t, s, "p1")
	rc, ref := s.verb(Request{Actor: "emma", Card: "p1", Event: card.Landed{Head: sha(1)}})
	assert.NotNil(t, ref, "Landed is the engine's, from a land result, never a worker's verb")
	if ref == nil {
		assert.False(t, rc.Accepted, "a worker's Landed was applied")
	}
	assert.Equal(t, "merging", s.state("p1"))
}

// Requests: "Add is anyone hooked in". An actor that is no row at all, hooked
// or not, adds a card.
func TestColdreadDefectVerbAddByUnhookedActor(t *testing.T) {
	w := newWorld(t)
	w.stream("s1")
	w.row("r1", 2, "pro")
	w.fold()
	_, ref := Verb(w.v, w.inputs(), Request{Actor: "stranger", Coordinators: []string{"rowan"},
		Event: card.Add{ID: "p9", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p9", Paths: []string{"a/q"}}})
	assert.NotNil(t, ref, "an actor not hooked in to the sprint may not add")
}

// Guarantee 4: "A ready blocker with no room is dealt by preemption in the
// tick that finds it ready, unless no row serves its tier". A row with free
// width but its deal-ahead queue full (1 working, 3 ready, width 2, ahead 2)
// is "free width" to the preemption step and "no room" to the deal: the
// blocker is neither preempted nor dealt, tick after tick.
func TestColdreadDefectBlockerQueuedBehindReadyQueue(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	for i, p := range []string{"p1", "p2", "p3", "p4"} {
		s.producer(p, "s1", "pro", fmt.Sprintf("a/%d", i))
	}
	s.run(3)
	for _, p := range []string{"p1", "p2", "p3", "p4"} {
		s.requireState(p+".w1", "ready")
	}
	s.take("p1.w1")
	s.producer("b", "s1", "pro", "b/z")
	s.must("priority", store.SetCardPriority{Card: "b", Priority: "blocker"})
	s.tick()
	s.requireState("b", "ready")
	s.tick()
	s.tick()
	assert.Equal(t, "working", s.state("b"), "the ready blocker waits behind three ready consumers on a row with free width")
}

// Built: "Rows written by a group (Gone) are claimed like cards, so no group
// of a tick fences a record another group writes"; Deal: "open rows (up, not
// held ...)". The candidates are cached in questions(), before limits claims
// the row: the same tick plans r1 Gone and deals p1 to r1.
func TestColdreadDefectDealToRowGoneSameTick(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.requireState("p1", "ready")
	s.fold()
	in := s.inputs()
	in.Seen["r1"] = in.Now.Add(-time.Hour)
	plan := Tick(s.v, in, Default{})
	gone, dealt := false, false
	for _, wr := range crWrites(plan) {
		if x, ok := wr.(store.SetRowState); ok && x.Row == "r1" && x.State == "down" {
			gone = true
		}
		if x, ok := wr.(store.PutCard); ok && x.Holder == "r1" {
			dealt = true
		}
	}
	require.True(t, gone, "r1 is planned gone")
	assert.False(t, dealt, "a consumer is dealt to r1 in the tick that plans r1 gone")
	for _, op := range tickOnly(plan.Ops) {
		rc, err := s.f.Apply(t.Context(), op)
		require.NoError(t, err)
		assert.True(t, rc.Accepted, "the tick's own op is refused, no race: %s", rc.Reason)
	}
	if c, ok := s.snap().Cards["p1.w1"]; ok {
		assert.NotEqual(t, "down", s.snap().Rows[c.Holder].State, "p1.w1 sits ready on a row that is down")
	}
}

// Policy questions: "one policy judgement per version and question". A v1
// policy.order judgement still open swallows v2's failure of the same
// question: v2 fails order and no judgement names v2.
func TestColdreadDefectPolicyJudgementPerVersion(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.requireState("p1", "ready")
	s.must("judge", store.PutJudgement{ID: "policy.order.1", Kind: "policy", About: "order", Question: "v1 failed order", Group: "policy:v1",
		Command: "nova-sprint judge policy.order.1 retry", Deadline: s.now.Add(time.Hour)})
	s.ask = scripted{Answers{Version: 2, Failed: map[string]string{"order": "boom"}}}
	plan := s.tick()
	found := false
	for _, wr := range crWrites(plan) {
		if j, ok := wr.(store.PutJudgement); ok && j.Kind == "policy" && j.About == "order" && j.Group == "policy:v2" {
			found = true
		}
	}
	assert.True(t, found, "v2's failure of order has its own policy judgement")
}

// Deal and reads: "AskReads{rows} cutting every read a head needs in one
// plan, to distinct rows"; Fix: "verdict(producer, reads)" over all of them;
// reads(card) 1..5. Policy asks 2 reads, one row may read: one read is cut,
// it says ok, and the fix step merges on 1 of 2 reads.
func TestColdreadDefectMergeOnFewerReadsThanAsked(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.ask = scripted{Answers{Version: 1, Reads: map[string]int{"p1": 2}}}
	s.tick()
	s.tick()
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.requireState("p1", "review")
	var reads []string
	for id, c := range s.snap().Cards {
		if c.Kind == "read" && c.Of == "p1" {
			reads = append(reads, id)
		}
	}
	require.Len(t, reads, 1, "one row may read, so one read is cut of the two asked")
	s.take(reads[0])
	s.report(reads[0], "ok", sha(1), "fine")
	s.tick()
	assert.NotEqual(t, "merging", s.state("p1"), "p1 merged on 1 read of the 2 policy asked for")
}

// Reports: "the excess is refused ("row R is at width 2: finish C first")".
// The refusal names Live(row)[0], the first live consumer by id, which here
// is the ready consumer being taken: "finish a.w1 first" to the take of a.w1.
func TestColdreadDefectTakeRefusalNamesItself(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("a", "s1", "pro", "a/x")
	s.producer("z", "s1", "pro", "a/y")
	s.run(3)
	s.requireState("a.w1", "ready")
	s.requireState("z.w1", "ready")
	s.take("z.w1")
	s.arrive(InboxItem{Kind: "take", Row: "r1", Consumer: "a.w1"})
	plan := s.tick()
	require.Len(t, plan.Answers, 1)
	assert.True(t, plan.Answers[0].Refused)
	assert.Contains(t, plan.Answers[0].Text, "finish z.w1 first", "the fix names the working consumer, not the one being taken: %q", plan.Answers[0].Text)
}

// Tick step 7: "A group refused alone on fresh state is left out until its
// cards change, with an engine judgement naming the plan and the store's
// reason"; RefusedOp's own doc says the same. Nothing reads a refused op of
// one group: the same group is planned again every tick, no judgement.
func TestColdreadDefectRefusedAloneReplannedForever(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.requireState("p1", "ready")
	s.fold()
	gen := s.v.Cards["p1"].Gen
	in := s.inputs()
	in.Refused = []RefusedOp{{ID: "t1.9.x", Reason: "a store rule the steps did not pre-check",
		Groups: []GroupKey{{Kind: "start", Cards: []string{"p1"}, Gens: map[string]int{"p1": gen}}}}}
	plan := Tick(s.v, in, Default{})
	replanned, judged := false, false
	for _, op := range plan.Ops {
		for _, g := range OpGroups(op) {
			if slices.Contains(g.Cards, "p1") {
				replanned = true
			}
		}
		for _, wr := range op.Writes {
			if j, ok := wr.(store.PutJudgement); ok && j.Kind == "engine" {
				judged = true
			}
		}
	}
	assert.False(t, replanned, "the group refused alone on unchanged cards is planned again")
	assert.True(t, judged, "no engine judgement names the refused plan")
}

// Land, LANDING step 8: "the third failure stops the stream that met it;
// other streams that meet the same red are refused under that one judgement
// and not stopped". The count is per base across streams: s1 meets the red
// twice, s2 once, and s2 (its first meeting) is held.
func TestColdreadDefectBaseRedStopsAStreamOnItsFirstMeeting(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.stream("s2")
	s.row("r1", 2, "pro")
	for i, st := range []string{"s1", "s1", "s2"} {
		id := fmt.Sprintf("L%d", i)
		s.inflight = append(s.inflight, EdgeRequest{ID: id, Kind: "land", Stream: st})
		s.arrive(InboxItem{Kind: "land", Request: id, Outcome: "base-red", Base: "deadbeef"})
		s.tick()
	}
	assert.False(t, s.snap().Streams["s2"].Held, "s2 met the red base once and is stopped")
}

// LAYER-3 Land: "one base-red judgement per base"; LANDING step 8: "there is
// one judgement for the base, not one per stream". Each meeting opens one
// more: two streams meeting one red base leave two open judgements.
func TestColdreadDefectBaseRedOneJudgementPerBase(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.stream("s2")
	s.row("r1", 2, "pro")
	for i, st := range []string{"s1", "s2"} {
		id := fmt.Sprintf("L%d", i)
		s.inflight = append(s.inflight, EdgeRequest{ID: id, Kind: "land", Stream: st})
		s.arrive(InboxItem{Kind: "land", Request: id, Outcome: "base-red", Base: "deadbeef"})
		s.tick()
	}
	assert.Len(t, s.openJudgements("base-red"), 1, "one red base, one judgement")
}

// LAYER-3 Land: "stopping a stream at its third meeting and resuming all it
// stopped when green"; LANDING step 8. The stop is a HoldStream, a deal
// control that never touches landing (LANDING 9), so the stopped stream
// keeps sending its merging producer at the red base every tick.
func TestColdreadDefectBaseRedStopDoesNotStopLanding(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	crMerging(t, s, "p1")
	for i := 0; i < 3; i++ {
		s.tick()
		req := s.request("land", "s1")
		s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "base-red", Base: base})
		s.tick()
	}
	require.True(t, s.snap().Streams["s1"].Held, "the third meeting stops s1")
	p := s.tick()
	for _, e := range p.Edges {
		assert.NotEqual(t, "land", e.Kind, "stopped stream s1 is sent to land at the red base again: %+v", e)
	}
}

// LAYER-3 "Retire and fail limits": fault_limit, "3 faults of the last 10
// on a row in 30 minutes rest it 30 minutes ... a deal filter in the view"
// (LPM R34). Tick builds the candidate rows in questions(), before rest()
// fills the resting set, and the cache is never rebuilt: the filter never
// applies. Three faults in three seconds on r1 and the three producers are
// dealt straight back to r1 in the next tick.
func TestColdreadDefectRestedRowStillDealt(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 4, "pro")
	ps := []string{"p0", "p1", "p2"}
	for _, p := range ps {
		s.producer(p, "s1", "pro", "a/"+p)
	}
	s.run(3)
	for _, p := range ps {
		s.take(p + ".w1")
	}
	for _, p := range ps {
		s.report(p+".w1", "fault", "", "provider 503")
	}
	s.tick()
	for _, p := range ps {
		_, redealt := s.snap().Cards[p+".w2"]
		assert.False(t, redealt, "%s is dealt back to r1, which three faults in 30 minutes rested", p)
	}
}

// Guarantee 11 and "Sentinels and needs" Inputs: "dependants of each need
// landed or failed since the last tick"; "Nothing per tick is O(C) after
// the cold build". The needs step walks every waiting producer every tick
// (as the drops walk every failed one, limits every ready and review, and
// the alarms every landed card per stream): a tick over 20,000 waiting
// producers whose need never lands allocates in proportion to them.
func TestColdreadDefectTickScansEveryWaitingProducer(t *testing.T) {
	alloc := func(n int) uint64 {
		snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s1": {Name: "s1"}}, Cards: map[string]store.Card{}, Rows: map[string]store.Row{}}
		snap.Cards["need"] = store.Card{ID: "need", Kind: "producer", Stream: "s1", State: "working", Tier: "pro"}
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("w%06d", i)
			snap.Cards[id] = store.Card{ID: id, Kind: "producer", Stream: "s1", State: "waiting", Tier: "pro", Needs: []string{"need"}}
		}
		v := Build(snap, t0)
		in := Inputs{Now: t0, Heads: map[string]string{"s1": base}}
		Tick(v, in, Default{})
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		Tick(v, in, Default{})
		runtime.ReadMemStats(&b)
		return b.TotalAlloc - a.TotalAlloc
	}
	small, large := alloc(200), alloc(20000)
	assert.Less(t, large, 10*small, "a quiet tick costs %d bytes at 200 waiting and %d at 20,000", small, large)
}

// Build rules: "no dead code". An unexported function nothing calls.
func TestColdreadDefectDeadCode(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	decl := map[string]bool{}
	var srcs []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		srcs = append(srcs, string(src))
		f, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && !ast.IsExported(fd.Name.Name) {
				decl[fd.Name.Name] = true
			}
		}
	}
	all := strings.Join(srcs, "\n")
	var dead []string
	for name := range decl {
		if strings.Count(all, name+"(") < 2 {
			dead = append(dead, name)
		}
	}
	slices.Sort(dead)
	assert.Empty(t, dead, "functions declared and never called")
}

// crAsker answers every rows question it is asked (its first candidate) and
// reads(card) from a map: a policy that fails nothing it was asked.
type crAsker struct{ reads map[string]int }

func (a crAsker) Ask(q Questions) Answers {
	ans := Answers{Version: 1, Reads: a.reads, Rows: map[string][]string{}}
	for _, r := range q.Rows {
		var names []string
		for _, c := range r.Candidates {
			names = append(names, c.Name)
		}
		ans.Rows[r.Card.ID] = names
	}
	return ans
}

// Policy questions: an answer "that fails ... is the default for this tick,
// with one policy judgement per version and question"; LAYER-3 "Asker asks
// each question once per tick with all its subjects". questions() counts the
// reads owed with the engine's default (t.o is nil there), the deal with
// policy's: policy wants 2 reads, 1 is cut, so the read's rows question is
// never asked, the deal reads the absent answer as a failure and opens a
// policy judgement against a policy that answered everything it was asked.
func TestColdreadDefectPolicyJudgedForQuestionNeverAsked(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.row("r3", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.tick()
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.requireState("p1", "review")
	s.ask = crAsker{reads: map[string]int{"p1": 2}}
	s.tick()
	assert.NotContains(t, s.openJudgements("policy"), "policy.rows.1", "a rows judgement for a question the tick never asked (every rows question asked was answered)")
}

// Tick step 3: "blocker groups (never cut; one per row)". The preemption is
// one per row; a blocker's plain Start is not: three ready blockers are all
// dealt to one row in one tick, every one outside the budget.
func TestColdreadDefectBlockerStartsNotOnePerRow(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 4, "pro")
	for i, b := range []string{"b1", "b2", "b3"} {
		s.producer(b, "s1", "pro", fmt.Sprintf("b/%d", i))
		s.must("priority", store.SetCardPriority{Card: b, Priority: "blocker"})
	}
	s.tick()
	s.tick()
	n := 0
	for _, c := range s.snap().Cards {
		if c.Kind == "work" && c.Holder == "r1" {
			n++
		}
	}
	assert.LessOrEqual(t, n, 1, "blocker groups are one per row per tick; r1 got %d", n)
}

// Deal and reads, policy rows default: "for a read, one read on a row whose
// model differs from the author's when one is open, LESSONS 3.8". The
// engine's ranking never reads a row's model (RowFacts.Model is never even
// filled): the read goes to r2, the author's model, with r3 open.
func TestColdreadDefectReadIgnoresModel(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.roster.Workers["r2"] = roster.Worker{Name: "r2", Width: 1, Tiers: []string{"pro", "heavy"}, Harness: "m-r1"} // the author's model
	s.must("row", store.PutRow{Name: "r2", Session: "hook-r2", Role: "worker"})
	s.must("seen", store.SetRowState{Row: "r2", State: "up"})
	s.row("r3", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.tick()
	require.Equal(t, "r1", s.card("p1.w1").Holder)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	assert.Equal(t, "r3", s.card("p1.r1.1").Holder, "the read went to a row of the author's model (m-r1) while r3 (m-r3) was open")
}

// Reports: "A report matching a done consumer's result and head is acked
// from the record ("applied at seq N"), never applied twice, so layer 4 may
// redeliver". After a restart (a cold Build, when redelivery happens) the
// view never witnessed the write, and the ack names seq 0.
func TestColdreadDefectAckSeqZeroAfterRestart(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.tick()
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	v := Build(s.snap(), s.now)
	in := s.inputs()
	in.Inbox = []InboxItem{{ID: "x1", Kind: "report", Row: "r1", Consumer: "p1.w1", Result: "ok", Head: sha(1)}}
	plan := Tick(v, in, Default{})
	require.Len(t, plan.Answers, 1)
	assert.NotContains(t, plan.Answers[0].Text, "seq 0", "the ack names a seq that never applied it")
}

// Escalation: "one rank up once the producer's bounces reach escalate_after
// (default 2, unmeasured), one more per further step". desired() steps once
// at escalate_after and then jumps to the top at frontier_after: a flash
// card at 3 bounces is dealt at pro, not heavy.
func TestColdreadDefectEscalationOneMorePerStep(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("rf", 4, "flash")
	s.row("rp", 4, "flash", "pro")
	s.row("rh", 4, "flash", "pro", "heavy")
	s.row("rx", 4, "flash", "pro", "heavy", "frontier")
	s.producer("p1", "s1", "flash", "a/x")
	s.must("bounces", store.SetCardBounces{Card: "p1", Bounces: 3})
	s.tick()
	s.tick()
	assert.Equal(t, "rh", s.card("p1.w1").Holder, "bounces 3 = escalate_after + 1: two ranks up")
}
