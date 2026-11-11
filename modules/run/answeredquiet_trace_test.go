package run

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/require"
)

// The answered-quiet trace walk (tla/AnsweredQuietTrace.tla, "Trace
// validation" in tla/README.md): the run's own Ticker on the store fake,
// with one watch cause it can turn on and off from outside, a producer that
// fails admission (Observed.Unadmitted: the admission judgement about its
// stream), driven through the moves of AnsweredQuiet.tla. Each move is
// recorded with the state the code holds after it, read back from the code:
// js from the store's admission judgements, quiet from the clock against
// the latest answer. broke is not recorded: TLC finds it from the moves,
// and the trace matches only if the code's quiet ends where the model's
// does. TLC checks every recording is a behaviour of AnsweredQuiet.tla.

const aqAbout = "s"

type quietWalk struct {
	t      *testing.T
	rng    *rand.Rand
	f      *store.Fake
	tk     *Ticker
	now    time.Time
	ops    int
	cause  bool
	ticked bool
	env    int
	old    bool // the breaker: the Ticker forgets each tick's break (the first quiet)
	recs   []string
	seen   map[string]bool
}

func newQuietWalk(t *testing.T, seed int64, old bool) *quietWalk {
	f := started(t)
	must(t, f, "stream", store.PutStream{Name: aqAbout, Repo: "o/r", Base: "main"})
	must(t, f, "add", store.PutCard{ID: "p1", Kind: "producer", Stream: aqAbout, Tier: "pro", Brief: "do p1", Paths: []string{"p1"}})
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	require.NoError(t, err)
	w := &quietWalk{t: t, rng: rand.New(rand.NewSource(seed)), f: f, tk: tk, now: t0, old: old, seen: map[string]bool{}}
	w.record("Init")
	return w
}

func (w *quietWalk) judgements() []store.Judgement {
	s, err := w.f.Snapshot(context.Background())
	require.NoError(w.t, err)
	var out []store.Judgement
	for _, j := range s.Judgements {
		if j.Kind == "admission" && j.About == aqAbout {
			out = append(out, j)
		}
	}
	slices.SortFunc(out, func(a, b store.Judgement) int { return episodeOf(a.ID) - episodeOf(b.ID) })
	return out
}

func (w *quietWalk) last() (store.Judgement, bool) {
	js := w.judgements()
	if len(js) == 0 {
		return store.Judgement{}, false
	}
	return js[len(js)-1], true
}

// answeredAt is the latest answer's time, zero for none.
func (w *quietWalk) answeredAt() time.Time {
	var at time.Time
	for _, j := range w.judgements() {
		if j.State == "answered" && j.ClosedAt.After(at) {
			at = j.ClosedAt
		}
	}
	return at
}

func (w *quietWalk) quiet() bool {
	at := w.answeredAt()
	return !at.IsZero() && w.now.Before(at.Add(brJudgeTO))
}

func (w *quietWalk) record(act string) {
	var js []string
	all := w.judgements()
	for i, j := range all {
		js = append(js, fmt.Sprintf("%q", j.State))
		if i > 0 && all[i-1].State == "answered" {
			if strings.HasPrefix(j.Question, "STILL HOLDS") {
				w.seen["still-holds"] = true
			} else {
				w.seen["reopened-plain"] = true
			}
		}
		if j.State == "expired" {
			w.seen["cleared"] = true
		}
	}
	w.seen[act] = true
	w.recs = append(w.recs, fmt.Sprintf("  [i |-> %d, action |-> %q, cause |-> %s, js |-> << %s >>, quiet |-> %s, ticked |-> %s, env |-> %d]",
		len(w.recs)+1, act, tlaBool(w.cause), strings.Join(js, ", "), tlaBool(w.quiet()), tlaBool(w.ticked), w.env))
}

// tick is one run tick, with the cause as Observed.Unadmitted says it.
func (w *quietWalk) tick() {
	w.now = w.now.Add(time.Second)
	o := Observed{Now: w.now, Roster: roster.Roster{}, Heads: map[string]string{aqAbout: strings.Repeat("a", 40)}}
	if w.cause {
		o.Unadmitted = map[string]string{"p1": "it touches a path the base no longer has"}
	}
	if w.old {
		w.tk.broken = nil
	}
	_, err := w.tk.Tick(context.Background(), o)
	require.NoError(w.t, err)
	w.ticked = true
	w.record("Tick")
}

func (w *quietWalk) envMove(act string) {
	w.env++
	w.ticked = false
	w.record(act)
}

func (w *quietWalk) toggle() {
	w.now = w.now.Add(time.Second)
	w.cause = !w.cause
	w.envMove("Cause")
}

func (w *quietWalk) answer() {
	w.now = w.now.Add(time.Second)
	j, _ := w.last()
	w.ops++
	r, err := w.f.Apply(context.Background(), store.Op{ID: fmt.Sprintf("qwalk%d", w.ops), Epoch: 1, Actor: "walk", Kind: "judge", At: w.now,
		Expect: []store.Expect{store.JudgementOpen{ID: j.ID}}, Writes: []store.Write{store.AnswerJudgement{ID: j.ID, Answer: "retry", Actor: "coordinator"}}})
	require.NoError(w.t, err)
	require.True(w.t, r.Accepted, "%+v", r)
	w.envMove("Answer")
}

// quietEnds is the clock reaching the end of the latest answer's quiet.
func (w *quietWalk) quietEnds() {
	w.now = w.answeredAt().Add(brJudgeTO)
	w.envMove("QuietEnds")
}

// move picks one move enabled in the model at random. An open episode's
// deadline is a judgement timeout after it opened, later than the quiet's
// end of the answer before it, so no deadline passes in a walk.
func (w *quietWalk) move() {
	j, ok := w.last()
	moves := []func(){w.toggle, w.toggle}
	if !w.ticked {
		moves = append(moves, w.tick, w.tick, w.tick)
	}
	if ok && j.State == "open" {
		moves = append(moves, w.answer)
	}
	if w.quiet() {
		moves = append(moves, w.quietEnds)
	}
	moves[w.rng.Intn(len(moves))]()
}

func (w *quietWalk) write(dir, cfg string) {
	module := "---------------------------- MODULE AnsweredQuietTraceLog ----------------------------\n" +
		"EXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(w.recs, ",\n") + "\n>>\n" +
		"=============================================================================\n"
	require.NoError(w.t, os.MkdirAll(dir, 0o755))
	require.NoError(w.t, os.WriteFile(filepath.Join(dir, "AnsweredQuietTraceLog.tla"), []byte(module), 0o644))
	if cfg != "" {
		require.NoError(w.t, os.WriteFile(filepath.Join(dir, "AnsweredQuietTrace.cfg"), []byte(cfg), 0o644))
	}
}

// quietScript is walk 0: open, answer, quiet, the cause clears and comes
// back inside the quiet (pushed at once, plain), answer again, the quiet's
// end with the cause unbroken (STILL HOLDS), and a clear.
func quietScript(w *quietWalk) []func() {
	return []func(){w.toggle, w.tick, w.answer, w.tick, w.toggle, w.tick, w.toggle, w.tick,
		w.answer, w.tick, w.quietEnds, w.tick, w.toggle, w.tick}
}

// TestAnsweredQuietTraceRefinementWalk writes walk 0 and eight random
// walks; with NOVA_TRACE_DIR set each one's trace is written for
// tla/check-trace.sh. It fails when the walks together never reach a move
// or an outcome of the model.
func TestAnsweredQuietTraceRefinementWalk(t *testing.T) {
	t.Parallel()
	walks := []*quietWalk{}
	w := newQuietWalk(t, 0, false)
	for _, m := range quietScript(w) {
		m()
	}
	walks = append(walks, w)
	for seed := int64(1); seed <= 8; seed++ {
		w := newQuietWalk(t, seed, false)
		for range 60 {
			w.move()
		}
		walks = append(walks, w)
	}
	seen := map[string]bool{}
	for i, w := range walks {
		for k := range w.seen {
			seen[k] = true
		}
		if dir := os.Getenv("NOVA_TRACE_DIR"); dir != "" {
			w.write(filepath.Join(dir, fmt.Sprintf("answeredquiet-walk-%d", i)), "")
		}
	}
	for _, want := range []string{"Tick", "Cause", "Answer", "QuietEnds", "still-holds", "reopened-plain", "cleared"} {
		if !seen[want] {
			t.Errorf("the walks never reached %s", want)
		}
	}
}

// TestAnsweredQuietRunCarriesTheBreak is walk 0's point at run level: the
// Ticker carries a tick's break to the next tick, so a cause that cleared
// and came back inside the quiet is pushed at once, with no STILL HOLDS.
func TestAnsweredQuietRunCarriesTheBreak(t *testing.T) {
	t.Parallel()
	w := newQuietWalk(t, 0, false)
	for _, m := range []func(){w.toggle, w.tick, w.answer, w.tick, w.toggle, w.tick, w.toggle, w.tick} {
		m()
	}
	j, _ := w.last()
	require.Equal(t, "admission.s.2", j.ID)
	require.Equal(t, "open", j.State)
	require.True(t, strings.HasPrefix(j.Question, "stream s: "), j.Question)
}

// TestAnsweredQuietTraceRefusesDivergence holds the breaker: the first
// quiet, keyed by kind and about only (the Ticker forgets each break). The
// cause cleared and came back inside the quiet, a tick, and nothing pushed:
// its trace is the fixture tla/traces/answeredquiet-breaker, which
// check-trace.sh must refuse at that tick.
func TestAnsweredQuietTraceRefusesDivergence(t *testing.T) {
	t.Parallel()
	broken := newQuietWalk(t, 0, true)
	for _, m := range []func(){broken.toggle, broken.tick, broken.answer, broken.tick, broken.toggle, broken.tick, broken.toggle, broken.tick} {
		m()
	}
	require.Contains(t, broken.recs[len(broken.recs)-1], `cause |-> TRUE, js |-> << "answered" >>, quiet |-> TRUE, ticked |-> TRUE`)
	model, err := os.ReadFile(filepath.Join("..", "..", "tla", "AnsweredQuietTrace.cfg"))
	require.NoError(t, err)
	cfg := strings.Replace(string(model), "\\* expect: matched", fmt.Sprintf("\\* expect: forbidden at %d", len(broken.recs)), 1)
	if out := os.Getenv("NOVA_TRACE_DIR"); out != "" {
		broken.write(filepath.Join(out, "answeredquiet-breaker"), cfg)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "tla", "traces", "answeredquiet-breaker", "AnsweredQuietTraceLog.tla"))
	require.NoError(t, err, "the fixture: NOVA_TRACE_DIR=tla/traces go test -run TestAnsweredQuietTraceRefusesDivergence ./modules/run/ writes it")
	got := "---------------------------- MODULE AnsweredQuietTraceLog ----------------------------\n" +
		"EXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(broken.recs, ",\n") + "\n>>\n" +
		"=============================================================================\n"
	require.Equal(t, string(want), got, "tla/traces/answeredquiet-breaker/AnsweredQuietTraceLog.tla is stale")
	wantCfg, err := os.ReadFile(filepath.Join("..", "..", "tla", "traces", "answeredquiet-breaker", "AnsweredQuietTrace.cfg"))
	require.NoError(t, err)
	require.Equal(t, string(wantCfg), cfg, "tla/traces/answeredquiet-breaker/AnsweredQuietTrace.cfg is stale")
}
