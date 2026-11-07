package policy

// Third cold read (2026-11-07, head f62551fe), by a reader with no memory of
// the design discussions. Each TestColdread3Defect* fails at that head and
// names the sequence and the design, Built or model line it breaks. All use
// store.Fake or hand-built snapshots; only the abandoned-run probe reads the
// wall clock, because the deadline it probes is one (TickTime).
//
// Copied into v2-integrate-22 in policy round 5 from
// the work directory with these changes only, to the
// round 5 API: verbs and ticks take the roster (w.ro; readWorld configures
// its rows by hand), engines are on the unit tests' fake clock
// (testEngine), LoadVersions reads at the snapshot it is given, and the
// abandoned-run probe runs a process engine (the evaluator, ruling 1) on
// the wall clock behind NOVA_LOAD (breakTiming).

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// A top-level name bound by a tuple or list assignment is refused with no
// line: topLines (engine.go) records only a def and a plain `name = ...`, so
// bind gets line 0 and Failure.Error prints no line. Design, the question
// set: "The validator's refusal names the version, the question, the
// subject and the line"; Built round 3 ruling 2: "Every refusal has a line
// ... an unknown top-level name ... is refused at its own line".
func TestColdread3DefectTupleBoundNameRefusedWithoutLine(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	snap := w.snap()
	for _, extra := range []string{"foo, _x = 1, 2\n", "[foo, _y] = [1, 2]\n"} {
		body, line := appended(extra)
		_, fs := testEngine().Answer(Versions{Active: pv(2, body)}, snap, w.ro, FullQuestions(snap, nil))
		require.NotEmpty(t, fs, extra)
		assert.True(t, strings.HasPrefix(fs[0].Error(), fmt.Sprintf("POLICY REFUSED v2 line %d: unknown top-level name foo", line)),
			"%q: got %q", extra, fs[0].Error())
	}
}

// A question defined with keyword-only parameters passes the arity check:
// Function.NumParams counts keyword-only parameters, and bind checks only
// NumParams, HasVarargs and HasKwargs, so `def reads(*, card)` is stored. It
// then fails on every tick that asks reads (no positional parameter), which
// `set` could not see on a snapshot with no producer in review. Design, the
// verbs: set refuses "a wrong arity"; the question set: "Every question is a
// Starlark function of the arity shown".
func TestColdread3DefectKeywordOnlyParamPassesArity(t *testing.T) {
	t.Parallel()
	w := &world{t: t, st: store.NewFake(), ro: handRoster()}
	w.up()
	body, _ := override("reads", "def reads(*, card):\n    return 1")
	_, err := Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), w.file("kw.star", body), "keyword-only")
	require.Error(t, err, "def reads(*, card) takes no positional parameter: set must refuse it")
	assert.Contains(t, err.Error(), "reads takes 1 parameter(s)")
}

// readWorld: stream s1; rows a, b, c (pro, width 1, up); producer p (pro,
// review, author a). The candidates for p's reads are b and c.
func readWorld(t *testing.T) *world {
	w := &world{t: t, st: store.NewFake(), ro: handRoster()}
	writes := []store.Write{store.PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1},
		store.PutCard{ID: "p", Kind: "producer", Stream: "s1", Tier: "pro", Score: 1, Brief: "do p", Prose: "do p"}}
	for _, r := range []string{"a", "b", "c"} {
		writes = append(writes, store.PutRow{Name: r})
		w.configure(rc{r, "worker", 1, []string{"pro"}, ""})
	}
	w.apply(writes...)
	w.apply(store.SetRowState{Row: "a", State: "up"}, store.SetRowState{Row: "b", State: "up"}, store.SetRowState{Row: "c", State: "up"},
		store.SetCardState{Card: "p", State: "review"}, store.SetCardReview{Card: "p"}, store.SetCardAuthor{Card: "p", Author: "a"})
	w.up()
	return w
}

// The joint check ignores the read count: reads(p) = 2 and rows(p, read) =
// [b] pass, though layer 3 deals "count distinct rows" and only one is named
// while candidate c could take the second read, so p's second read has no
// row and p never leaves review. Design, the question table (rows): "for a
// read, `count` distinct rows in that order"; the joint check: it "refuses
// width and rows answers that leave a card with ready work and no row able
// to take it when the candidates themselves could" (LESSONS 5.6).
func TestColdread3DefectJointCheckIgnoresReadCount(t *testing.T) {
	t.Parallel()
	w := readWorld(t)
	snap := w.snap()
	body, _ := override("reads", "def reads(card):\n    return 2")
	body = strings.Replace(body, "    return prefer(candidates, _want(card, kind))", "    return [candidates[0].name]", 1)
	a, fs := testEngine().Answer(Versions{Active: pv(2, body)}, snap, w.ro, FullQuestions(snap, nil))
	// Round 5: the probe's first line required a.Rows to hold v2's answer
	// ["b"], which no passing assertion below allows (once v2 is refused
	// the built-in answers). The refusal itself is required instead.
	require.NotEmpty(t, fs)
	assert.Contains(t, fs[0].Error(), "rows(p read): reads is 2, and 1 row(s) named have work above 0 in width, while 2 candidate(s) could take a read")
	assert.NotEmpty(t, fs, "2 reads over 1 named row, while c could take the second: the joint check must refuse")
	assert.NotEqual(t, 2, a.Version, "v2 leaves p's second read with no row")
}

// A tick whose context has already expired (the caller's deadline, which
// AnswerContext honours) fails at input construction and returns one
// failure, the built-in's, at "default line 1": no judgement opens for the
// active or the previous version, and the one that opens (policy:v0, "policy
// check default") blames the binary. tla/Policy.tla Tick: every version in
// Failing (active, prev, Default when all break) opens its judgement;
// LAYER-5 the tick's answer: the judgement is the active version's,
// "group key policy:v<n>". This path is never in the trace walk.
func TestColdread3DefectExpiredContextJudgesOnlyTheBuiltin(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	snap := w.snap()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, fs := testEngine().AnswerContext(ctx, Versions{Active: pv(3, defaultBody), Prev: pv(2, defaultBody)}, snap, w.ro, FullQuestions(snap, nil))
	require.Equal(t, -1, a.Version)
	var groups []string
	for _, j := range Judgements(snap, w.ro, fs) {
		groups = append(groups, j.Group)
	}
	assert.Equal(t, []string{"policy:v3", "policy:v2", "policy:v0"}, groups,
		"every version tried failed: one judgement each, as the model's Failing")
}

// A version activation racing a tick: the tick reads its Versions
// (LoadVersions, which takes a snapshot of its own and returns no seq) and
// then its snapshot; v2 is activated between the two reads. Answer takes
// Versions{Active: v1} with a snapshot whose active is v2 and answers by v1,
// which is neither that snapshot's active nor its previous active's first
// pick (v2 runs clean). Nothing in the API pairs the two or refuses the
// mismatch. tla/Policy.tla Pick: "IF active \notin breaks THEN active";
// LAYER-5, the tick's answer: answered_by beside "the event's policy (the
// active version)".
func TestColdread3DefectAnswerTakesVersionsTheSnapshotDoesNotHold(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	v2 := w.set("two.star", strings.Replace(defaultBody, "return 1  #", "return 2  #", 1))
	vs, err := LoadVersions(w.ctx(), w.st, w.snap())
	require.NoError(t, err)
	_, err = Activate(w.ctx(), w.st, w.ro, testEngine(), w.caller(), v2, "race")
	require.NoError(t, err)
	snap := w.snap()
	require.Equal(t, v2, snap.Policy)
	a, fs := testEngine().Answer(vs, snap, w.ro, FullQuestions(snap, nil))
	assert.True(t, a.Version == snap.Policy || len(fs) > 0,
		"answered by v%d on a snapshot whose active is v%d, with no refusal", a.Version, snap.Policy)
}

// check is blind to the rung: FullQuestions asks landing on "flowing" only,
// so a version whose landing fails on a backed-up stream passes check, set
// and activate on a snapshot, and then fails every tick in which layer 3
// asks landing for a backed-up stream on that same snapshot. Design,
// guarantee 6: "check on a snapshot predicts the tick on that snapshot";
// the verbs: activate "re-runs check on the current snapshot" (a version
// that runs clean); the cold-read question "When can check and the next
// tick disagree?".
func TestColdread3DefectCheckBlindToBackedUpRung(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	body, _ := override("landing", "def landing(stream):\n    return {\"backed_up\": 4 // (0 if stream.rung == \"backed_up\" else 1), \"flowing\": 1}")
	rep, err := Check(w.ctx(), w.st, w.ro, testEngine(), w.file("rung.star", body))
	require.NoError(t, err)
	snap := w.snap()
	_, fs := testEngine().Answer(Versions{Active: pv(2, body)}, snap, w.ro, Questions{Landing: []LandingQuestion{{"s1", RungBackedUp}}})
	assert.Equal(t, len(fs) > 0, rep.Code() == 1, "check said %d refused; the tick on the same snapshot failed: %v", len(rep.Refusals), fs)
}

// A run abandoned at the deadline keeps running: str() of a list nested
// 300,000 deep is one builtin step (writeValue, quadratic in depth through
// pathContains) and allocates little, so the thread's cancel is never seen
// and the goroutine burns a core for tens of seconds after the tick ended.
// Every tick re-runs the active version first, so each tick adds one, until
// the cores are gone and the built-in's own tick misses the deadline (a
// stall in all but name). tick.go: "a single builtin step that outlasts it
// ... is abandoned, not waited for: its goroutine ends at its next step";
// Built ruling 4 bounds an abandoned run only by what one step allocates.
// Reads the wall clock: the deadline it probes is one.
func TestColdread3DefectAbandonedRunOutlivesDeadline(t *testing.T) {
	breakTiming(t)
	stuck := func() int {
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		n := 0
		for _, g := range strings.Split(string(buf), "\n\n") {
			n += b2i(strings.Contains(g, "go.starlark.net/starlark.writeValue"))
		}
		return n
	}
	require.Equal(t, 0, stuck(), "no writeValue goroutine before the probe")
	w := fakeWorld(t)
	snap := w.snap()
	body, _ := override("stitch", "def stitch(stream):\n    x = []\n    for _i in range(30000):\n        x = [[[[[[[[[[x]]]]]]]]]]\n    return len(str(x)) > 0")
	a, fs := timedEngine().Answer(Versions{Active: pv(2, body)}, snap, w.ro, FullQuestions(snap, nil))
	require.Equal(t, 0, a.Version, "the built-in answers: %v", fs)
	require.NotEmpty(t, fs)
	assert.Eventually(t, func() bool { return stuck() == 0 }, 2*TickTime, TickTime/20,
		"the abandoned run must end at its next step; it is still inside one str()")
}

// set's equal-body refusal names a data version and tells the coordinator
// to activate it, which activate refuses ("v2 is of kind data, which policy
// does not run"): the fix a refusal names must work (CORRECT-USE). The
// body is compared before it is compiled, against every row, data included.
// Design, the verbs: set refuses "a body equal to a stored version ("v4 has
// this body: policy activate 4")"; activate refuses "a version of kind data".
func TestColdread3DefectSetEqualBodyNamesDataVersion(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	w.apply(store.PutPolicy{Kind: "data", Name: "table", Body: "x = 1\n"})
	_, err := Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), w.file("x.star", "x = 1\n"), "same as data")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "nova-sprint policy activate 2", "v2 is a data row: activate refuses it")
}
