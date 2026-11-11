package run

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/require"
)

// The base-red trace walk (tla/BaseRedTrace.tla, "Trace validation" in
// tla/README.md): the run's own Ticker on the store fake, and its BaseWatch
// reading a fake forge, driven through the moves of BaseRed.tla. Each move
// is recorded with the state the code holds after it, read back from the
// code: fact from BaseWatch.Read, live from the store's producers, js from
// the store's base-red judgements, quiet and due from the clock against
// their times. TLC checks every recording is a behaviour of BaseRed.tla.

const (
	brAbout   = "o/r@main"
	brJudgeTO = 3600 * time.Second // answering's Judgement timeout
)

// brSets is the model's two red sets as the forge's failing checks.
var brSets = map[string][]string{"A": {"lint"}, "B": {"unit"}}

type baseRedWalk struct {
	t      *testing.T
	rng    *rand.Rand
	f      *store.Fake
	tk     *Ticker
	bw     *BaseWatch
	ff     *checksForge
	now    time.Time
	ops    int
	tip    int
	live   string // the live producer, "" for none
	cards  int
	ticked bool
	env    int
	broken bool // the breaker: the tick is not given the reads (the old behaviour)
	recs   []string
	seen   map[string]bool
}

func newBaseRedWalk(t *testing.T, seed int64, broken bool) *baseRedWalk {
	f := started(t)
	must(t, f, "stream", store.PutStream{Name: "s", Repo: "o/r", Base: "main", Forge: "github"})
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	require.NoError(t, err)
	ff := &checksForge{rollups: map[string]forge.CheckRollup{}}
	w := &baseRedWalk{t: t, rng: rand.New(rand.NewSource(seed)), f: f, tk: tk, ff: ff, bw: &BaseWatch{Forge: ff}, now: t0,
		broken: broken, seen: map[string]bool{}}
	w.addProducer()
	w.record("Init", "", "")
	return w
}

func (w *baseRedWalk) apply(kind string, expect []store.Expect, writes ...store.Write) {
	w.ops++
	r, err := w.f.Apply(context.Background(), store.Op{ID: fmt.Sprintf("walk%d", w.ops), Epoch: 1, Actor: "walk", Kind: kind, At: w.now, Expect: expect, Writes: writes})
	require.NoError(w.t, err)
	require.True(w.t, r.Accepted, "%s: %+v", kind, r)
}

func (w *baseRedWalk) addProducer() {
	w.cards++
	w.live = "p" + strconv.Itoa(w.cards)
	w.apply("add", nil, store.PutCard{ID: w.live, Kind: "producer", Stream: "s", Tier: "pro", Brief: "do " + w.live, Paths: []string{w.live}})
}

func (w *baseRedWalk) snap() store.Snapshot {
	s, err := w.f.Snapshot(context.Background())
	require.NoError(w.t, err)
	return s
}

// judgements is the base-red episodes in order.
func (w *baseRedWalk) judgements() []store.Judgement {
	var out []store.Judgement
	for _, j := range w.snap().Judgements {
		if j.Kind == "base-red" && j.About == brAbout {
			out = append(out, j)
		}
	}
	slices.SortFunc(out, func(a, b store.Judgement) int { return episodeOf(a.ID) - episodeOf(b.ID) })
	return out
}

func episodeOf(id string) int {
	n, _ := strconv.Atoi(id[strings.LastIndex(id, ".")+1:])
	return n
}

func (w *baseRedWalk) last() (store.Judgement, bool) {
	js := w.judgements()
	if len(js) == 0 {
		return store.Judgement{}, false
	}
	return js[len(js)-1], true
}

// quiet is the latest answer's judgement timeout still running.
func (w *baseRedWalk) quiet() bool {
	var at time.Time
	for _, j := range w.judgements() {
		if j.State == "answered" && j.ClosedAt.After(at) {
			at = j.ClosedAt
		}
	}
	return !at.IsZero() && w.now.Before(at.Add(brJudgeTO))
}

// due is the clock at or past the open one's deadline.
func (w *baseRedWalk) due() bool {
	j, ok := w.last()
	return ok && j.State == "open" && !w.now.Before(j.Deadline)
}

func (w *baseRedWalk) fact() string {
	f, ok := w.bw.Read()[brAbout]
	switch {
	case !ok:
		return "none"
	case !f.Red:
		return "green"
	}
	for name, failing := range brSets {
		if slices.Equal(f.Failing, failing) {
			return name
		}
	}
	w.t.Fatalf("a red read with failing %v is no red set of the model", f.Failing)
	return ""
}

func (w *baseRedWalk) liveNow() bool {
	for _, c := range w.snap().Cards {
		if c.Kind == "producer" && c.Stream == "s" && slices.Contains(liveStates, c.State) {
			return true
		}
	}
	return false
}

// setOf is the model's red set of a base-red question.
func setOf(q string) string {
	for name, failing := range brSets {
		if strings.Contains(q, "check red ("+failing[0]+")") {
			return name
		}
	}
	return "?"
}

func (w *baseRedWalk) record(act, arg, how string) {
	var js []string
	all := w.judgements()
	for i, j := range all {
		js = append(js, fmt.Sprintf("[set |-> %q, st |-> %q]", setOf(j.Question), j.State))
		switch {
		case strings.HasPrefix(j.Question, "UNANSWERED"):
			w.seen["escalated"] = true
		case strings.HasPrefix(j.Question, "STILL RED"):
			w.seen["still-red"] = true
		case i > 0 && all[i-1].State == "answered" && setOf(j.Question) == setOf(all[i-1].Question):
			w.seen["reopened-plain"] = true // the same red set after an answer, said plain: a new red stretch
		}
		if strings.HasPrefix(j.Answer, "the red set changed") {
			w.seen["changed"] = true
		}
		if strings.HasPrefix(j.Answer, "cleared: o/r main is green") {
			w.seen["cleared"] = true
		}
	}
	w.seen[act+":"+how] = true
	w.recs = append(w.recs, fmt.Sprintf("  [i |-> %d, action |-> %q, arg |-> %q, how |-> %q, fact |-> %q, live |-> %s, js |-> << %s >>, quiet |-> %s, due |-> %s, ticked |-> %s, env |-> %d]",
		len(w.recs)+1, act, arg, how, w.fact(), tlaBool(w.liveNow()), strings.Join(js, ", "), tlaBool(w.quiet()), tlaBool(w.due()), tlaBool(w.ticked), w.env))
}

func (w *baseRedWalk) step() { w.now = w.now.Add(time.Second) }

func (w *baseRedWalk) heads() map[string]string {
	return map[string]string{"s": fmt.Sprintf("%040x", w.tip)}
}

// tick is one run tick, as serve runs it: the reads so far go in, and the
// watch runs after it. What the watch changed is recorded as its own move:
// a base no longer watched forgets its read (Unwatch), and a tip read again
// (the coordinator's retry, a running tip past CIWait, cards back on a
// base) is a Read.
func (w *baseRedWalk) tick() {
	w.step()
	o := Observed{Now: w.now, Roster: roster.Roster{}, Heads: w.heads(), Bases: w.bw.Read()}
	if w.broken {
		o.Bases = nil
	}
	_, err := w.tk.Tick(context.Background(), o)
	require.NoError(w.t, err)
	w.ticked = true
	w.record("Tick", "", "")
	before := w.fact()
	w.bw.Watch(context.Background(), w.snap(), w.heads(), w.now)
	w.bw.Wait()
	switch after := w.fact(); {
	case after == before:
	case after == "none":
		w.record("Unwatch", "", "")
	default:
		w.env++
		w.ticked = false
		w.record("Read", after, "after-tick")
	}
}

// read is a new base tip whose checks the forge answers as how says, read
// by BaseWatch as serve has it read after a tick.
func (w *baseRedWalk) read(how string) {
	w.step()
	w.tip++
	sha := fmt.Sprintf("%040x", w.tip)
	r, err, arg := forge.CheckRollup{SHA: sha}, error(nil), "pending"
	switch how {
	case "green":
		r.State, arg = forge.CheckGreen, "green"
	case "A", "B":
		r.State, r.Failing, arg = forge.CheckRed, brSets[how], how
		r.URLs = []string{"https://example.invalid/actions/" + brSets[how][0]}
	case "running":
		r.State = forge.CheckPending
	case "A-running": // a failure shown, other checks still running: red once it has waited CIWait
		r.State, r.Waiting, r.Failing = forge.CheckRed, true, brSets["A"]
		r.URLs = []string{"https://example.invalid/actions/" + brSets["A"][0]}
	case "absent":
		r.State = forge.CheckAbsent
	case "rested":
		err = &forge.FaultError{Kind: forge.FaultRateLimit, Identity: forge.HostLogin, Reason: "HTTP 403: API rate limit exceeded"}
	}
	w.ff.set(sha, r, err)
	w.bw.Watch(context.Background(), w.snap(), w.heads(), w.now)
	w.bw.Wait()
	w.env++
	w.ticked = false
	w.record("Read", arg, how)
}

func (w *baseRedWalk) toggleCards() {
	w.step()
	if w.live != "" {
		w.apply("fail", nil, store.SetCardState{Card: w.live, State: "fail"})
		w.live = ""
	} else {
		w.addProducer()
	}
	w.env++
	w.ticked = false
	w.record("Cards", "", "")
}

// answer is the coordinator's retry, as the judge verb plans it.
func (w *baseRedWalk) answer() {
	w.step()
	j, _ := w.last()
	w.apply("judge", []store.Expect{store.JudgementOpen{ID: j.ID}}, store.AnswerJudgement{ID: j.ID, Answer: "retry", Actor: "coordinator"})
	w.env++
	w.ticked = false
	w.record("Answer", "", "")
}

// quietEnds is the clock reaching the end of the latest answer's quiet.
func (w *baseRedWalk) quietEnds() {
	var at time.Time
	for _, j := range w.judgements() {
		if j.State == "answered" && j.ClosedAt.After(at) {
			at = j.ClosedAt
		}
	}
	w.now = at.Add(brJudgeTO)
	w.env++
	w.ticked = false
	w.record("QuietEnds", "", "")
}

// lapse is the clock reaching the open one's deadline.
func (w *baseRedWalk) lapse() {
	j, _ := w.last()
	w.now = j.Deadline
	w.env++
	w.ticked = false
	w.record("Lapse", "", "")
}

// move picks one move enabled in the model at random. A deadline or a
// quiet's end moves the clock by the judgement timeout, so Lapse is taken
// only once no quiet runs (the jump would end it too).
func (w *baseRedWalk) move() {
	j, ok := w.last()
	open := ok && j.State == "open"
	var moves []func()
	if !w.ticked {
		moves = append(moves, w.tick, w.tick, w.tick)
	}
	if w.live != "" {
		for _, how := range []string{"green", "A", "B", "A", "B", "running", "A-running", "absent", "rested"} {
			moves = append(moves, func() { w.read(how) })
		}
	}
	moves = append(moves, w.toggleCards)
	if open {
		moves = append(moves, w.answer)
	}
	if w.quiet() {
		moves = append(moves, w.quietEnds)
	}
	if open && !w.due() && !w.quiet() {
		moves = append(moves, w.lapse, w.lapse)
	}
	moves[w.rng.Intn(len(moves))]()
}

func (w *baseRedWalk) module() string {
	return "---------------------------- MODULE BaseRedTraceLog ----------------------------\n" +
		"EXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(w.recs, ",\n") + "\n>>\n" +
		"=============================================================================\n"
}

func (w *baseRedWalk) write(dir, cfg string) {
	require.NoError(w.t, os.MkdirAll(dir, 0o755))
	require.NoError(w.t, os.WriteFile(filepath.Join(dir, "BaseRedTraceLog.tla"), []byte(w.module()), 0o644))
	if cfg != "" {
		require.NoError(w.t, os.WriteFile(filepath.Join(dir, "BaseRedTrace.cfg"), []byte(cfg), 0o644))
	}
}

// TestBaseRedTraceRefinementWalk writes walk 0, the scripted path (green,
// running, rested, red, the same red at a newer tip, a changed red set,
// green again; then red unanswered past its deadline, an answer, its quiet's
// end (STILL RED), an answer, green and the same red again inside the
// quiet (a new red stretch, said plain), and the cards gone), and eight
// random walks; with NOVA_TRACE_DIR set
// each one's trace is written for tla/check-trace.sh. It fails when the
// walks together never reach a move or an outcome of the model.
func TestBaseRedTraceRefinementWalk(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	walks := []*baseRedWalk{}
	w := newBaseRedWalk(t, 0, false)
	script := []func(){func() { w.read("green") }, w.tick, func() { w.read("running") }, w.tick, func() { w.read("rested") }, w.tick,
		func() { w.read("A") }, w.tick, func() { w.read("A") }, w.tick, func() { w.read("B") }, w.tick, func() { w.read("green") }, w.tick,
		func() { w.read("A") }, w.tick, w.lapse, w.tick, w.answer, w.tick, w.quietEnds, w.tick,
		w.answer, w.tick, func() { w.read("green") }, w.tick, func() { w.read("A") }, w.tick, w.quietEnds, w.tick,
		w.toggleCards, w.tick, w.lapse, w.tick}
	for _, m := range script {
		m()
	}
	walks = append(walks, w)
	for seed := int64(1); seed <= 8; seed++ {
		w := newBaseRedWalk(t, seed, false)
		for range 60 {
			w.move()
		}
		walks = append(walks, w)
	}
	for i, w := range walks {
		for k := range w.seen {
			seen[k] = true
		}
		if dir := os.Getenv("NOVA_TRACE_DIR"); dir != "" {
			w.write(filepath.Join(dir, fmt.Sprintf("basered-walk-%d", i)), "")
		}
	}
	for _, want := range []string{"Tick:", "Read:green", "Read:A", "Read:B", "Read:running", "Read:absent", "Read:rested",
		"Read:A-running", "Read:after-tick", "Unwatch:", "Cards:", "Answer:", "QuietEnds:", "Lapse:", "escalated", "still-red", "reopened-plain", "changed", "cleared"} {
		if !seen[want] {
			t.Errorf("the walks never reached %s", want)
		}
	}
}

// TestBaseRedTraceRefusesDivergence holds the breaker: the run before
// 2026-11-10, whose tick was not given the base reads. A red base with live
// cards, a tick, and no judgement: its trace is the fixture
// tla/traces/basered-breaker, which check-trace.sh must refuse at the tick.
func TestBaseRedTraceRefusesDivergence(t *testing.T) {
	t.Parallel()
	honest, broken := newBaseRedWalk(t, 0, false), newBaseRedWalk(t, 0, true)
	for _, w := range []*baseRedWalk{honest, broken} {
		w.read("A")
		w.tick()
	}
	require.Contains(t, honest.recs[2], `js |-> << [set |-> "A", st |-> "open"] >>`)
	require.Contains(t, broken.recs[2], `fact |-> "A", live |-> TRUE, js |-> <<  >>`)
	model, err := os.ReadFile(filepath.Join("..", "..", "tla", "BaseRedTrace.cfg"))
	require.NoError(t, err)
	cfg := strings.Replace(string(model), "\\* expect: matched", fmt.Sprintf("\\* expect: forbidden at %d", len(broken.recs)), 1)
	if out := os.Getenv("NOVA_TRACE_DIR"); out != "" {
		broken.write(filepath.Join(out, "basered-breaker"), cfg)
	}
	got := broken.module()
	want, err := os.ReadFile(filepath.Join("..", "..", "tla", "traces", "basered-breaker", "BaseRedTraceLog.tla"))
	require.NoError(t, err, "the fixture: NOVA_TRACE_DIR=tla/traces go test -run TestBaseRedTraceRefusesDivergence ./modules/run/ writes it")
	require.Equal(t, string(want), got, "tla/traces/basered-breaker/BaseRedTraceLog.tla is stale")
	wantCfg, err := os.ReadFile(filepath.Join("..", "..", "tla", "traces", "basered-breaker", "BaseRedTrace.cfg"))
	require.NoError(t, err)
	require.Equal(t, string(wantCfg), cfg, "tla/traces/basered-breaker/BaseRedTrace.cfg is stale")
}

// TestBaseRedRetryReadsTheTipAgain is the coordinator's retry at run level:
// the run's tick opens base-red on a red tip read once; the answer, at the
// next tick's watch, reads that tip's checks once more, and no more after.
func TestBaseRedRetryReadsTheTipAgain(t *testing.T) {
	t.Parallel()
	w := newBaseRedWalk(t, 0, false)
	w.read("A")
	w.tick()
	j, ok := w.last()
	require.True(t, ok && j.State == "open", "no base-red opened: %+v", j)
	n0, _ := w.ff.counts()
	w.tick()
	n1, _ := w.ff.counts()
	require.Equal(t, n0, n1, "a settled red tip read again without an answer")
	w.answer()
	w.now = w.now.Add(LandPoll) // the watch asks a tip at most once a LandPoll
	w.tick()
	w.tick()
	n2, _ := w.ff.counts()
	require.Equal(t, n1+1, n2, "the retry read the tip %d times, want once", n2-n1)
}
