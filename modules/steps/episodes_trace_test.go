package steps

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/require"
)

// The trace walk of tla/JudgementId.tla ("Trace validation" in
// tla/README.md): the steps' own Tick on the store fake, every op applied
// through the store's check (an invalid op fails the walk), on a store that
// starts with judgements of any episode numbers and states, as one written
// by an older planner holds them. byCount walks one row's flap judgements
// (judgeAt: a count names its judgement), the other one stream's admission
// judgements (judge: a cause a step detects). The moves are the model's:
// Flap, Cause, Answer (any open one), Deadline (the clock past every open
// one's deadline) and Tick. After each the model's variables are read back
// from the code: js from the store (the episode from the id, the state, the
// count from the question), count from the row, due from the clock against
// the open deadlines. TLC checks every recording is a behaviour of
// JudgementId.tla.

type idWalk struct {
	s       *sim
	rng     *rand.Rand
	byCount bool
	kind    string
	about   string
	ticked  bool
	env     int
	wedged  bool // the breaker: a tick whose op the store's check threw away
	recs    []string
	seen    map[string]bool
}

// idLegacy is one judgement the walk's store starts with: its episode, its
// state, and for a flap the count its question reports.
type idLegacy struct {
	ep  int
	st  string
	cnt int
}

func newIDWalk(t *testing.T, seed int64, byCount bool, flaps int, legacy []idLegacy) *idWalk {
	w := &idWalk{s: newSim(t), rng: rand.New(rand.NewSource(seed)), byCount: byCount, kind: "admission", about: "s1", seen: map[string]bool{}}
	w.s.row("r", 1, "pro")
	if byCount {
		w.kind, w.about = "flap", "r"
		if flaps > 0 {
			w.s.must("flaps", store.SetRowFlaps{Row: "r", Flaps: flaps})
		}
	} else {
		w.s.stream("s1")
		w.s.producer("p1", "s1", "heavy", "a.go") // no row serves heavy: it stays in the queue, where admission judges it
	}
	timeout := seconds(Engine.Timeouts.Judgement)
	for _, l := range legacy {
		q := "stream s1: an older cause"
		if byCount {
			q = flapQuestion("r", l.cnt)
		}
		id := w.s.legacy(w.kind, w.about, l.ep, q, w.s.now.Add(3*timeout))
		switch l.st {
		case "answered":
			w.s.must("answer", store.AnswerJudgement{ID: id, Answer: "retry", Actor: "carol"})
		case "expired":
			w.s.must("expire", store.ExpireJudgement{ID: id, Reason: "cleared"})
		}
	}
	w.s.now = w.s.now.Add(timeout + time.Second) // past the quiet of every answer the store starts with
	w.record("Init")
	return w
}

func (w *idWalk) judgements() []store.Judgement {
	var out []store.Judgement
	for _, j := range w.s.snap().Judgements {
		if j.Kind == w.kind && j.About == w.about {
			out = append(out, j)
		}
	}
	slices.SortFunc(out, func(a, b store.Judgement) int { return episodeOf(a) - episodeOf(b) })
	return out
}

func (w *idWalk) open() []store.Judgement {
	return slices.DeleteFunc(w.judgements(), func(j store.Judgement) bool { return j.State != "open" })
}

// cnt is the count j's question reports: the model's cnt (0 for a kind no
// count names).
func (w *idWalk) cnt(j store.Judgement) int {
	n := 0
	if _, q, ok := strings.Cut(j.Question, "has flapped "); ok && w.byCount {
		_, err := fmt.Sscanf(q, "%d", &n)
		require.NoError(w.s.t, err, j.Question)
	}
	return n
}

// due is the model's due: an open judgement's deadline has passed.
func (w *idWalk) due() bool {
	return slices.ContainsFunc(w.open(), func(j store.Judgement) bool { return !w.s.now.Before(j.Deadline) })
}

func (w *idWalk) record(act string) {
	var js []string
	for _, j := range w.judgements() {
		js = append(js, fmt.Sprintf("[ep |-> %d, st |-> %q, cnt |-> %d]", episodeOf(j), j.State, w.cnt(j)))
		switch {
		case strings.HasPrefix(j.Answer, "superseded by "):
			w.seen["superseded"] = true
		case strings.HasPrefix(j.Answer, "deadline passed unanswered"):
			w.seen["reopened"] = true
		case w.byCount && !strings.Contains(j.Question, repeated) && episodeOf(j) > w.cnt(j):
			w.seen["past its count"] = true
		case strings.HasPrefix(j.Answer, "stream s1's waiting and ready cards pass admission"):
			w.seen["cleared"] = true
		}
	}
	w.seen[act] = true
	b := map[bool]string{true: "TRUE", false: "FALSE"}
	w.recs = append(w.recs, fmt.Sprintf("  [i |-> %d, action |-> %q, js |-> {%s}, count |-> %d, cause |-> %s, due |-> %s, ticked |-> %s, env |-> %d]",
		len(w.recs)+1, act, strings.Join(js, ", "), w.s.snap().Rows["r"].Flaps, b[w.s.unadmitted != nil], b[w.due()], b[w.ticked], w.env))
}

// tick is one tick of the steps, applied. The breaker's is the tick as the
// planner that named a reopened episode by the count of judgements left
// it: its one judgement op invalid, nothing applied.
func (w *idWalk) tick() {
	if !w.wedged {
		w.s.checkedTick()
	}
	w.ticked = true
	w.record("Tick")
}

func (w *idWalk) envMove(act string) {
	w.env++
	w.ticked = false
	w.record(act)
}

func (w *idWalk) flap() {
	w.s.must("flaps", store.SetRowFlaps{Row: "r", Flaps: w.s.snap().Rows["r"].Flaps + 1})
	w.envMove("Flap")
}

func (w *idWalk) toggle() {
	if w.s.unadmitted == nil {
		w.s.unadmitted = map[string]string{"p1": "it touches a path the base no longer has"}
	} else {
		w.s.unadmitted = nil
	}
	w.envMove("Cause")
}

// answer is the coordinator's answer to open judgement j. An answered
// watch judgement stays quiet for the judgement timeout
// (tla/AnsweredQuiet.tla), which this model leaves out: the walk answers an
// admission judgement only when it is the one open, and moves the clock past
// the quiet.
func (w *idWalk) answer(j store.Judgement) {
	w.s.must("answer", store.AnswerJudgement{ID: j.ID, Answer: "retry", Actor: "carol"})
	if !w.byCount {
		w.s.now = w.s.now.Add(seconds(Engine.Timeouts.Judgement) + time.Second)
	}
	w.envMove("Answer")
}

// deadline is the clock passing every open judgement's deadline.
func (w *idWalk) deadline() {
	for _, j := range w.open() {
		if !j.Deadline.Before(w.s.now) {
			w.s.now = j.Deadline.Add(time.Second)
		}
	}
	w.envMove("Deadline")
}

// move picks one move enabled in the model at random.
func (w *idWalk) move() {
	moves := []func(){}
	if !w.ticked {
		moves = append(moves, w.tick, w.tick, w.tick)
	}
	if w.byCount {
		moves = append(moves, w.flap)
	} else {
		moves = append(moves, w.toggle)
	}
	if open := w.open(); len(open) > 0 {
		if !w.due() {
			moves = append(moves, w.deadline, w.deadline)
		}
		if j := open[w.rng.Intn(len(open))]; w.byCount || len(open) == 1 {
			moves = append(moves, func() { w.answer(j) })
		}
	}
	moves[w.rng.Intn(len(moves))]()
}

func (w *idWalk) log() string {
	return "---------------------------- MODULE JudgementIdTraceLog ----------------------------\n" +
		"EXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(w.recs, ",\n") + "\n>>\n" +
		"=============================================================================\n"
}

// cfg is the trace's cfg: the model's own, for the walk's kind, expecting
// want.
func (w *idWalk) cfg(want string) string {
	model, err := os.ReadFile(filepath.Join("..", "..", "tla", "JudgementIdTrace.cfg"))
	require.NoError(w.s.t, err)
	cfg := strings.Replace(string(model), "\\* expect: matched", "\\* expect: "+want, 1)
	if !w.byCount {
		cfg = strings.Replace(cfg, "ByCount = TRUE", "ByCount = FALSE", 1)
	}
	return cfg
}

func (w *idWalk) write(dir, want string) {
	require.NoError(w.s.t, os.MkdirAll(dir, 0o755))
	require.NoError(w.s.t, os.WriteFile(filepath.Join(dir, "JudgementIdTraceLog.tla"), []byte(w.log()), 0o644))
	require.NoError(w.s.t, os.WriteFile(filepath.Join(dir, "JudgementIdTrace.cfg"), []byte(w.cfg(want)), 0o644))
}

// collided is the store the collision was seen on, at the model's size: a
// row at 5 flaps with flap.r.3, .4 and .5, one per count, all open.
var collided = []idLegacy{{3, "open", 3}, {4, "open", 4}, {5, "open", 5}}

// randomLegacy is up to four judgements of distinct episodes in 1..9, in
// any state; a flap's count is at least the limit and at most flaps.
func randomLegacy(rng *rand.Rand, flaps int) []idLegacy {
	var out []idLegacy
	for _, ep := range rng.Perm(9)[:rng.Intn(5)] {
		l := idLegacy{ep: ep + 1, st: []string{"open", "open", "answered", "expired"}[rng.Intn(4)]}
		if flaps > 0 {
			l.cnt = Engine.Limits.FlapLimit + rng.Intn(flaps-Engine.Limits.FlapLimit+1)
		}
		out = append(out, l)
	}
	return out
}

// TestJudgementIdTraceWalk writes, for each kind, a scripted walk (the
// collided store for the flaps: its deadline, the tick that reopens one and
// supersedes two, a flap asked past its count, an answer; three open
// episodes with a gap for admission: the deadline, the cause cleared and
// back) and six random walks on random stores; with NOVA_TRACE_DIR set each
// one's trace is written for tla/check-trace.sh. It fails when the walks
// together never reach a move or an outcome of the model.
func TestJudgementIdTraceWalk(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, byCount := range []bool{true, false} {
		name := map[bool]string{true: "count", false: "episode"}[byCount]
		var walks []*idWalk
		if byCount {
			w := newIDWalk(t, 0, true, 5, collided)
			for _, m := range []func(){w.deadline, w.tick, w.flap, w.tick, w.deadline, w.tick, w.flap, w.flap, w.tick} {
				m()
			}
			w.answer(w.open()[0])
			w.tick()
			walks = append(walks, w)
		} else {
			w := newIDWalk(t, 0, false, 0, []idLegacy{{1, "open", 0}, {4, "open", 0}, {7, "open", 0}})
			for _, m := range []func(){w.toggle, w.deadline, w.tick, w.toggle, w.tick, w.toggle, w.tick} {
				m()
			}
			w.answer(w.open()[0])
			w.tick()
			walks = append(walks, w)
		}
		for seed := int64(1); seed <= 6; seed++ {
			rng := rand.New(rand.NewSource(seed))
			flaps := 0
			if byCount && rng.Intn(4) > 0 {
				flaps = Engine.Limits.FlapLimit + rng.Intn(4)
			}
			var legacy []idLegacy
			if !byCount || flaps > 0 {
				legacy = randomLegacy(rng, flaps)
			}
			w := newIDWalk(t, seed, byCount, flaps, legacy)
			for range 50 {
				w.move()
			}
			walks = append(walks, w)
		}
		for i, w := range walks {
			for k := range w.seen {
				seen[k] = true
			}
			if dir := os.Getenv("NOVA_TRACE_DIR"); dir != "" {
				w.write(filepath.Join(dir, fmt.Sprintf("judgementid-walk-%s-%d", name, i)), "matched")
			}
		}
	}
	for _, want := range []string{"Tick", "Flap", "Cause", "Answer", "Deadline", "superseded", "reopened", "past its count", "cleared"} {
		if !seen[want] {
			t.Errorf("the walks never reached %s", want)
		}
	}
}

// TestJudgementIdTraceRefusesDivergence holds the breaker: the collided
// store, its deadline, and the tick as the first planner left it (every
// open episode reopened as the count of judgements plus one, an id that
// exists, so the store's check threw the op away and the store stayed as it
// was). Its trace is the fixture tla/traces/judgementid-breaker, which
// check-trace.sh must refuse at that tick.
func TestJudgementIdTraceRefusesDivergence(t *testing.T) {
	t.Parallel()
	w := newIDWalk(t, 0, true, 5, collided)
	w.deadline()
	w.wedged = true
	w.tick()
	require.Contains(t, w.recs[len(w.recs)-1], `[ep |-> 5, st |-> "open", cnt |-> 5]}, count |-> 5, cause |-> FALSE, due |-> TRUE, ticked |-> TRUE`)
	cfg := w.cfg(fmt.Sprintf("forbidden at %d", len(w.recs)))
	if out := os.Getenv("NOVA_TRACE_DIR"); out != "" {
		w.write(filepath.Join(out, "judgementid-breaker"), fmt.Sprintf("forbidden at %d", len(w.recs)))
	}
	dir := filepath.Join("..", "..", "tla", "traces", "judgementid-breaker")
	want, err := os.ReadFile(filepath.Join(dir, "JudgementIdTraceLog.tla"))
	require.NoError(t, err, "the fixture: NOVA_TRACE_DIR=tla/traces go test -run TestJudgementIdTraceRefusesDivergence ./modules/steps/ writes it")
	require.Equal(t, string(want), w.log(), "tla/traces/judgementid-breaker/JudgementIdTraceLog.tla is stale")
	wantCfg, err := os.ReadFile(filepath.Join(dir, "JudgementIdTrace.cfg"))
	require.NoError(t, err)
	require.Equal(t, string(wantCfg), cfg, "tla/traces/judgementid-breaker/JudgementIdTrace.cfg is stale")
}
