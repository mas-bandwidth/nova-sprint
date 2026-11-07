package policy

// Tests of the verbs' refusals (bodies and reasons the store rejects), of
// the tick's deadline against long prose, of the joint check's read count,
// and of concurrent sets and ticks. Each names the exact sequence and the
// design line it holds the code to. The two timing tests read real time, as
// the engine's deadline is real time by design: they use wallClockTest (skipped
// under -race and -short, and unless NOVA_LOAD is set) and are not parallel.
// Engines are on the unit tests' fake clock (testEngine) except the timing
// tests', which are on the wall clock (timedEngine).
// TestConcurrentTicksStayExact and TestBodyAtTheCapIsAccepted record
// attacks that held.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// A policy file whose body holds a NUL byte (or invalid UTF-8, or whose
// base name is over store.MaxName) compiles in Starlark (the bytes sit in a
// comment) and passes check, then reaches the store, which refuses the op as
// ErrInvalid: the verb hands back a raw store error, not a refusal.
// Sequence: up; write default.star plus "# \x00" to nul.star; policy set.
// Design: "Every refusal is in the CORRECT-USE form" and "checked before
// stored" (The verbs; "Refusals are *Refusal in the CORRECT-USE
// form"); the fuzz's "the store never returning ErrInvalid" (How it is
// checked) — the layer built an op the store calls invalid.
func TestSetBodyOrNameTheStoreRefusesReachesItAsErrInvalid(t *testing.T) {
	t.Parallel()
	for name, file := range map[string][2]string{
		"NUL in a comment":     {"nul.star", DefaultBody() + "\n# a NUL \x00 in a comment\n"},
		"invalid UTF-8":        {"bad.star", DefaultBody() + "\n# bytes \xff\xfe in a comment\n"},
		"a 200-byte file name": {strings.Repeat("a", 195) + ".star", DefaultBody() + "\n# long name\n"},
	} {
		t.Run(name, func(t *testing.T) {
			w := fakeWorld(t)
			w.up()
			_, err := Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), w.file(file[0], file[1]), "test")
			require.Error(t, err)
			assert.False(t, errors.Is(err, store.ErrInvalid), "the verb let the store call its op invalid: %v", err)
			var r *Refusal
			assert.True(t, errors.As(err, &r), "want a *Refusal in the CORRECT-USE form, got %T: %v", err, err)
		})
	}
}

// The --reason of set and activate goes into the op's Note (and activate's
// into each AnswerJudgement), which the store checks as text: a reason in
// invalid UTF-8 (argv can carry any bytes but NUL) passes the verb's own
// check (non-blank) and reaches the store as ErrInvalid.
// Sequence: up; set a.star --reason "\xff"; and: up; set a.star; activate 2
// --reason "\xff".
// Design: "Every refusal is in the CORRECT-USE form".
func TestReasonInInvalidUTF8ReachesTheStoreAsErrInvalid(t *testing.T) {
	t.Parallel()
	body := DefaultBody() + "\n# a\n"
	t.Run("set", func(t *testing.T) {
		w := fakeWorld(t)
		w.up()
		_, err := Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), w.file("a.star", body), "why \xff")
		var r *Refusal
		assert.True(t, errors.As(err, &r), "want a *Refusal, got %T: %v", err, err)
	})
	t.Run("activate", func(t *testing.T) {
		w := fakeWorld(t)
		w.up()
		v := w.set("a.star", body)
		_, err := Activate(w.ctx(), w.st, w.ro, testEngine(), w.caller(), v, "why \xff")
		var r *Refusal
		assert.True(t, errors.As(err, &r), "want a *Refusal, got %T: %v", err, err)
	})
}

// proseSnap is 5,000 producers in review over 100 rows that serve every
// tier, each producer with prose of size bytes of plain ASCII (the store has
// no bound on prose; a brief of 16 KiB is ordinary).
func proseSnap(size int) (store.Snapshot, roster.Roster) {
	snap, ro := loadSnap(5000, 100)
	prose := strings.Repeat("abcdefgh", size/8)
	for id, c := range snap.Cards {
		c.Prose, c.State, c.Author = prose, "review", "r001"
		snap.Cards[id] = c
	}
	return snap, ro
}

// The tick's inputs are built before the deadline is first checked, and
// each rows, reads and verdict ask prints its card (prose included) for the
// answer cache's digest: the inputs alone outrun TickTime. Measured on the
// studio at f62551fe: 5,000 producers in review, 16 KiB prose each, the tick
// returned after 952 ms with "over the tick's time budget of 500ms building
// the tick's inputs" (8 KiB: 502 ms, and the built-in timed out in rows).
// Sequence: Answer(Versions{}, proseSnap(16 KiB), FullQuestions).
// Design: "One deadline per tick: ... TickTime ...
// from the first byte of input construction to the last answer returned".
func TestFunctionalProseMakesTheInputsOutrunTheTickDeadline(t *testing.T) {
	t.Parallel()
	wallClockTest(t)
	snap, ro := proseSnap(16 << 10)
	q := FullQuestions(snap, nil)
	start := time.Now()
	a, fs := timedEngine().Answer(Versions{}, snap, ro, q)
	took := time.Since(start)
	t.Logf("16 KiB prose x 5,000 in review: %s, answered by %d, failures %v", took, a.Version, fs)
	assert.Less(t, took, TickTime+deadlineMargin, "the tick must end near TickTime")
}

// The same snapshot leaves no version able to answer: the built-in alone
// (no stored version) fails every tick on it, so the tick deals nothing
// while the briefs are long (Version -1, NoAnswer). The design's load
// figure (5,000 producers, cold 61.7 ms) was measured with empty prose.
// Sequence: as above, at 16 KiB.
// Design: guarantee 4, "Every tick is answered by the active, the previous
// or the built-in version"; the default policy "answers every snapshot".
func TestProseLeavesTheBuiltInUnableToAnswer(t *testing.T) {
	t.Parallel()
	wallClockTest(t)
	snap, ro := proseSnap(16 << 10)
	a, fs := timedEngine().Answer(Versions{}, snap, ro, FullQuestions(snap, nil))
	assert.Equal(t, 0, a.Version, "the built-in must answer a snapshot of ordinary briefs; failures %v", fs)
}

// hungLimits is the default with a limits() that spends one long builtin
// step (str of a 60,000-deep list is quadratic in Starlark's cycle check)
// while allocating little: a version that hangs until the deadline.
func hungLimits() string {
	body, _ := override("limits", `def limits():
    x = []
    for _i in range(60000):
        x = [x]
    return {"bounces": min(6, len(str(x))), "faults": 3, "same_finding": 2, "reports_per_tick": 1000, "route_rest_window": 10, "route_rest_after": 3, `+steering+`}`)
	return body
}

// The fallback timer (fallbackAfter, TickTime/2) starts when race starts,
// after the inputs are built, not at the tick's first byte as the deadline
// does: when the inputs take more than TickTime/2, the built-in behind a
// hung active version starts after the deadline and the tick has no answer,
// though the built-in alone answers that snapshot inside TickTime. Measured
// on the studio at f62551fe, 5,000 producers in review with 4 KiB prose:
// inputs 255 ms, the built-in alone 319 ms (v0); behind a hung v2, 500 ms
// and Version -1 with two failures.
// Sequence: pick the smallest prose size at which the inputs take over
// TickTime/2 and the built-in alone still answers in time; Answer with
// Versions{} (answered by 0), then with Active = hungLimits().
// Design: "the versions after the one being
// waited on start at fallbackAfter = TickTime/2 ... if it has not answered,
// and the first version in order that passes answers, so starting early
// never changes which version answers (the model's Pick)"; tla/Policy.tla
// Tick: answered by the first of active, prev, Default not in breaks
// (Default does not break on this snapshot), TickAlwaysAnswered.
func TestFunctionalFallbackTimerStartsAfterTheInputs(t *testing.T) {
	t.Parallel()
	wallClockTest(t)
	var snap store.Snapshot
	var ro roster.Roster
	var q Questions
	found := false
	for size := 1 << 10; size <= 8<<10 && !found; size += 1 << 10 {
		snap, ro = proseSnap(size)
		q = FullQuestions(snap, nil)
		start := time.Now()
		_ = newIndex(snap, ro).asks(context.Background(), q)
		inputs := time.Since(start)
		start = time.Now()
		a, _ := timedEngine().Answer(Versions{}, snap, ro, q)
		alone := time.Since(start)
		if inputs > fallbackAfter+20*time.Millisecond && a.Version == 0 && alone < TickTime-50*time.Millisecond {
			t.Logf("prose %d bytes: inputs %s, the built-in alone %s", size, inputs, alone)
			found = true
		}
	}
	if !found {
		t.Skip("no prose size on this machine puts the inputs past TickTime/2 with the built-in alone answering")
	}
	a, fs := timedEngine().Answer(Versions{Active: pv(2, hungLimits())}, snap, ro, q)
	assert.Equal(t, 0, a.Version, "the built-in answers this snapshot alone, so it must answer behind a hung v2; failures %v", fs)
}

// A read needs reads(card) distinct rows, and the joint check asks only
// that one named row has work above 0. A policy answers reads 2 and gives
// the second candidate work 0 (each answer in domain): the review can be
// dealt one read and never the second, while the candidates themselves (two
// rows of width 1) could take both.
// Sequence: rows a, b, c serve flash, width 1, up; p in review, author c;
// active v2 = default with reads 2 and width(b) work 0; Answer.
// Design: "Beyond the per-answer domain, the validator checks the answers
// together, so no policy can leave the sprint unable to deal:
// it refuses width and rows answers that leave a card ... no row able to
// take it when the candidates themselves could"; rows: "for a read, count
// distinct rows in that order".
func TestJointCheckIgnoresTheReadCount(t *testing.T) {
	t.Parallel()
	ro := handRoster()
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Rows: map[string]store.Row{}, Cards: map[string]store.Card{
		"p": {ID: "p", Kind: "producer", Stream: "s", State: "review", Tier: "flash", Priority: "normal", Author: "c", Review: 1, Head: "h"},
	}}
	for _, n := range []string{"a", "b", "c"} {
		snap.Rows[n] = store.Row{Name: n, State: "up"}
		ro.Workers[n] = roster.Worker{Name: n, Width: 1, Tiers: []string{"flash"}, Harness: roster.WorkerHarness}
	}
	body := strings.Replace(DefaultBody(), "    return 1  # one read per review", "    return 2  # one read per review", 1)
	body = strings.Replace(body, `    return {"work": row.width, "ahead": row.width}`, `    w = 0 if row.name == "b" else row.width
    return {"work": w, "ahead": w}`, 1)
	require.NotEqual(t, DefaultBody(), body)
	a, fs := testEngine().Answer(Versions{Active: pv(2, body)}, snap, ro, FullQuestions(snap, nil))
	able := 0
	for _, r := range a.Rows[RowsQuestion{"p", KindRead}] {
		able += b2i(a.Width[r].Work > 0)
	}
	t.Logf("answered by %d: reads %d, rows able to read %d, failures %v", a.Version, a.Reads["p"], able, fs)
	if a.Version == 2 {
		assert.GreaterOrEqual(t, able, a.Reads["p"], "v2's answers leave the review owed %d distinct reads with %d row(s) able to take one", a.Reads["p"], able)
	}
}

// racingStore lands a second, identical policy-set between Set's check and
// its own op, as a second `policy set` of the same file (from another shell
// of the coordinator's) would.
type racingStore struct {
	store.Store
	once sync.Once
}

func (s *racingStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "policy-set" && len(op.Writes) == 1 {
		var err error
		s.once.Do(func() {
			twin := op
			twin.ID = op.ID + "-twin"
			_, err = s.Store.Apply(ctx, twin)
		})
		if err != nil {
			return store.Receipt{}, err
		}
	}
	return s.Store.Apply(ctx, op)
}

// Set reads every stored body, checks, then stores with no fence (no
// Expect): two sets of one file both pass the equal-body check and both
// store, so two versions hold one body. Up fences its op with AtSeq; Set
// does not.
// Sequence: up; set f.star, with an identical set landing between Set's
// read of the versions and its op; list the bodies.
// Design: The verbs, set refuses "a body equal to a stored version (\"v4 has
// this body: policy activate 4\")".
func TestSetRaceStoresOneBodyTwice(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	rs := &racingStore{Store: w.st}
	body := DefaultBody() + "\n# the same file\n"
	_, err := Set(w.ctx(), rs, w.ro, testEngine(), w.caller(), w.file("f.star", body), "test")
	all, verr := versions(w.ctx(), w.st)
	require.NoError(t, verr)
	n := 0
	for _, v := range all {
		n += b2i(v.Body == body)
	}
	assert.Equal(t, 1, n, "one body stored %d times (set returned %v)", n, err)
}

// Many ticks at once on one engine, over three versions and two
// snapshots, each answer equal to a fresh engine's on the same input (the
// cache stays exact under concurrent commits; run with -race).
func TestConcurrentTicksStayExact(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	s1 := w.snap()
	w.apply(store.SetCardState{Card: "p4", State: "ready"})
	s2 := w.snap()
	wide := seedRoster() // r1 at width 3 for the second snapshot
	r1 := wide.Workers["r1"]
	r1.Width = 3
	wide.Workers["r1"] = r1
	ros := []roster.Roster{w.ro, wide}
	v2, _ := override("reads", "def reads(card):\n    return 2")
	v3, _ := override("order", "def order(card):\n    return (card.id,)")
	vss := []Versions{{}, {Active: pv(2, v2)}, {Active: pv(3, v3), Prev: pv(2, v2)}}
	want := map[string]string{}
	for i, vs := range vss {
		for j, s := range []store.Snapshot{s1, s2} {
			a, _ := testEngine().Answer(vs, s, ros[j], FullQuestions(s, nil))
			want[fmt.Sprint(i, j)] = fmt.Sprint(a)
		}
	}
	e := testEngine()
	var wg sync.WaitGroup
	var mu sync.Mutex
	bad := []string{}
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range 30 {
				i, j := (g+k)%3, (g*k)%2
				s := []store.Snapshot{s1, s2}[j]
				a, _ := e.Answer(vss[i], s, ros[j], FullQuestions(s, nil))
				if got := fmt.Sprint(a); got != want[fmt.Sprint(i, j)] {
					mu.Lock()
					bad = append(bad, fmt.Sprint(i, j))
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	assert.Empty(t, bad)
}

// A body at exactly MaxBody (the default padded with comments) is
// stored and activates; one byte more is a refusal naming the cap; Starlark
// refuses deep nesting in the source ("excessive nesting") before it runs.
func TestBodyAtTheCapIsAccepted(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	pad := MaxBody - len(DefaultBody()) - 1
	body := DefaultBody() + "#" + strings.Repeat("x", pad-1) + "\n"
	require.Len(t, body, MaxBody)
	v := w.set("cap.star", body)
	_, err := Activate(w.ctx(), w.st, w.ro, testEngine(), w.caller(), v, "cap")
	require.NoError(t, err)
	_, err = Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), w.file("over.star", body+"#"), "over")
	var r *Refusal
	require.True(t, errors.As(err, &r), "%v", err)
	assert.Contains(t, r.What, "over 65536 bytes")
	_, fs := testEngine().runFile("deep.star", "_X = "+strings.Repeat("[", 29000)+strings.Repeat("]", 29000)+"\n"+DefaultBody(), w.snap(), w.ro)
	require.NotEmpty(t, fs)
	assert.Contains(t, fs[0].Msg, "excessive nesting")
}
