package policy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// broken is a body that fails on every tick (a runtime error in limits).
var broken, _ = override("limits", "def limits():\n    return 1 // 0")

// TestFallbackOrder: active, else the previous active, else the built-in;
// one failure per failing version; never a stall.
func TestFallbackOrder(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	snap, q := w.snap(), FullQuestions(w.snap(), nil)
	for _, tc := range []struct {
		name   string
		vs     Versions
		by     int
		failed []string
	}{
		{"active clean", Versions{Active: pv(3, defaultBody), Prev: pv(2, broken)}, 3, nil},
		{"active breaks", Versions{Active: pv(3, broken), Prev: pv(2, defaultBody)}, 2, []string{"v3"}},
		{"both break", Versions{Active: pv(3, broken), Prev: pv(2, broken)}, 0, []string{"v3", "v2"}},
		{"no previous", Versions{Active: pv(3, broken)}, 0, []string{"v3"}},
		{"none at all", Versions{}, 0, nil},
		{"prev is active", Versions{Active: pv(3, broken), Prev: pv(3, broken)}, 0, []string{"v3"}},
	} {
		a, fs := NewEngine().Answer(tc.vs, snap, q)
		assert.Equal(t, tc.by, a.Version, tc.name)
		var labels []string
		for _, f := range fs {
			labels = append(labels, f.Label)
			assert.Equal(t, fmt.Sprintf("POLICY REFUSED %s line %d limits(): floored division by zero", f.Label, defLine(broken, "limits")+1), f.Error())
		}
		assert.Equal(t, tc.failed, labels, tc.name)
	}
	e := NewEngine()
	p, f := compile(0, "default", broken)
	require.Nil(t, f)
	e.progs[0] = p // the built-in, broken: a bug in the binary
	a, fs := e.Answer(Versions{Active: pv(3, broken)}, snap, q)
	assert.Equal(t, -1, a.Version, "even the built-in failing returns, it never stalls")
	assert.Equal(t, []string{"v3", "default"}, []string{fs[0].Label, fs[1].Label})
}

// testBrokenMidSprint (in verbSuite, on Fake and PG): a version that
// divides by a stream name breaks when that stream arrives; the previous
// version answers, one judgement opens, the next tick opens no second one,
// and activating a clean version answers it. A policy change never moves a
// card, and check predicts the next tick.
func testBrokenMidSprint(t *testing.T, w *world) {
	e := NewEngine()
	w.up()
	breaks, line := override("landing", "def landing(stream):\n    return {\"backed_up\": 4 // (0 if stream.name == \"s3\" else 1), \"flowing\": 1}")
	v2 := w.set("breaks.star", breaks)
	_, err := Activate(w.ctx(), w.st, e, w.caller(), v2, "try it")
	require.NoError(t, err)
	tick := func() (Answers, []Failure) {
		vs, err := LoadVersions(w.ctx(), w.st)
		require.NoError(t, err)
		return e.Answer(vs, w.snap(), FullQuestions(w.snap(), nil))
	}
	a, fs := tick()
	require.Empty(t, fs)
	assert.Equal(t, 2, a.Version)
	w.apply(store.PutStream{Name: "s3", Repo: "git@example.com:r.git", Base: "main", Pos: 3})
	a, fs = tick()
	assert.Equal(t, 1, a.Version, "the previous active answers the whole tick")
	require.Len(t, fs, 1)
	assert.Equal(t, fmt.Sprintf("POLICY REFUSED v2 line %d landing(s3): floored division by zero", line+1), fs[0].Error())
	js := Judgements(w.snap(), fs)
	require.Len(t, js, 1)
	assert.Equal(t, store.PutJudgement{ID: fmt.Sprintf("policy-v2-s%d", w.snap().Seq), Kind: "policy", About: "v2",
		Question: fs[0].Error(), Command: "nova-sprint policy check v2", Group: "policy:v2"}, js[0])
	w.apply(js[0])
	_, fs = tick()
	require.Len(t, fs, 1, "the next tick tries the active again")
	assert.Empty(t, Judgements(w.snap(), fs), "at most one open policy judgement per version")
	before := w.snap()
	fixed := strings.Replace(breaks, "4 // (0 if stream.name == \"s3\" else 1)", "4", 1)
	v3 := w.set("fixed.star", fixed)
	rep, err := Check(w.ctx(), w.st, e, fmt.Sprint(v3))
	require.NoError(t, err)
	_, err = Activate(w.ctx(), w.st, e, w.caller(), v3, "fixed")
	require.NoError(t, err)
	after := w.snap()
	assert.Equal(t, before.Cards, after.Cards, "a policy change never moves a card")
	assert.Equal(t, "answered", after.Judgements[js[0].ID].State, "activating a clean version answers the judgement")
	a, fs = tick()
	require.Empty(t, fs)
	assert.Equal(t, 3, a.Version)
	for _, c := range rep.Changes {
		k, rest, _ := strings.Cut(c, ": ")
		assert.Equal(t, rest[strings.Index(rest, " -> ")+4:], a.shown[k], "check predicts the next tick: %s", c)
	}
	assert.Contains(t, rep.Changes, `landing(s3): (refused) -> {"backed_up": 4, "flowing": 1}`)
}

// TestDefaultAnswersEverySnapshot: the built-in answers every question on
// every snapshot shape the suite builds, and its answers are the design's.
func TestDefaultAnswersEverySnapshot(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	empty, err := store.NewFake().Snapshot(w.ctx())
	require.NoError(t, err)
	shapes := []store.Snapshot{empty, w.snap()}
	w.apply(store.SetCardBounces{Card: "p1", Bounces: 4}, store.SetCardProse{Card: "p2", Prose: "a Security fix"}, store.SetRowState{Row: "r3", State: "down"})
	shapes = append(shapes, w.snap())
	w.apply(store.SetCardState{Card: "p3", State: "merging"}, store.SetCardState{Card: "p1", State: "landed"}, store.SetRowTiers{Row: "r1", Tiers: []string{"pro"}})
	shapes = append(shapes, w.snap())
	for i, s := range shapes {
		a, fs := NewEngine().Answer(Versions{}, s, FullQuestions(s, nil))
		require.Empty(t, fs, "shape %d", i)
		assert.Equal(t, 0, a.Version)
		assert.Equal(t, Limits{Bounces: 6, Faults: 3, SameFinding: 2, ReportsPerTick: 1000, RouteRestWindow: 10, RouteRestAfter: 3}, a.Limits)
		assert.Equal(t, Timeouts{RowSilentGone: 600, RowSilentWithdraw: 1800, LandingStall: 60, BaseRegate: []int{120, 300}, Judgement: 3600, RouteRestFor: 1800}, a.Timeouts)
		assert.Empty(t, a.Prices)
	}
	a, _ := NewEngine().Answer(Versions{}, shapes[1], FullQuestions(shapes[1], nil))
	assert.Equal(t, []string{"r1"}, a.Rows[RowsQuestion{"p1", KindWork}], "p1 pro: its own rank, r4 held")
	assert.Equal(t, []string{"r1"}, a.Rows[RowsQuestion{"p2", KindWork}], "p2 heavy at 2 bounces wants frontier; no frontier row serves heavy, so r1")
	assert.Equal(t, []string{"r1"}, a.Rows[RowsQuestion{"p3", KindRead}], "p3 by flash r2: the rank above, never the author")
	assert.Empty(t, a.Rows[RowsQuestion{"p5", KindRead}], "p5 by r1: no candidate but its author (r4 held)")
	_, asked := a.Rows[RowsQuestion{"p5", KindRead}]
	assert.True(t, asked)
	assert.Equal(t, []any{int64(1), int64(10)}, a.Order["p1"])
	assert.Equal(t, []any{int64(0), int64(10)}, a.Order["p2"], "a fix attempt first")
	assert.Equal(t, "yes", a.Verdict["p3"])
	assert.Equal(t, Width{Work: 2, Ahead: 2}, a.Width["r1"])
	assert.Equal(t, Landing{BackedUp: 4, Flowing: 1}, a.Landing["s1"])
	assert.Equal(t, map[string]bool{"s1": true, "s2": true}, a.Stitch)
	a, _ = NewEngine().Answer(Versions{}, shapes[2], FullQuestions(shapes[2], nil))
	assert.Equal(t, []string{"r1"}, a.Rows[RowsQuestion{"p1", KindWork}], "p1 at 4 bounces wants frontier; none is up, so any rank at or above pro")
	assert.Equal(t, []string{"r1"}, a.Rows[RowsQuestion{"p2", KindWork}])
	a, _ = NewEngine().Answer(Versions{}, shapes[3], FullQuestions(shapes[3], nil))
	assert.Empty(t, a.Rows[RowsQuestion{"p2", KindWork}], "no row serves heavy: layer 3's judgement, not a refusal")
	assert.NotContains(t, a.Order, "p1", "a landed producer is not asked")
}

// TestJudgementStorable: a failure's text is made storable before
// PutJudgement: NUL and invalid UTF-8 replaced, at most 4096 bytes with a
// marker; the store accepts both.
func TestJudgementStorable(t *testing.T) {
	t.Parallel()
	nul, line := override("limits", "def limits():\n    fail(\"bad\" + chr(0))")
	huge, _ := override("limits", "def limits():\n    fail(\"x\" * 1000000)")
	for name, body := range map[string]string{"nul": nul, "1 MB": huge} {
		w := fakeWorld(t)
		snap := w.snap()
		_, fs := NewEngine().Answer(Versions{Active: pv(2, body)}, snap, FullQuestions(snap, nil))
		js := Judgements(snap, fs)
		require.Len(t, js, 1, name)
		assert.LessOrEqual(t, len(js[0].Question), 4096, name)
		if name == "nul" {
			assert.Equal(t, fmt.Sprintf("POLICY REFUSED v2 line %d limits(): fail: bad\uFFFD", line+1), js[0].Question)
		} else {
			assert.True(t, strings.HasSuffix(js[0].Question, "x [cut at 4096 bytes]"), js[0].Question[len(js[0].Question)-40:])
		}
		w.apply(js[0])
	}
	assert.Equal(t, "a\uFFFDb\uFFFD", storable("a\xffb\x00"))
}

// TestVerdictRetry: every done read faulted: the default answers retry;
// retry while an ok or fix read is done is refused.
func TestVerdictRetry(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.apply(store.SetCardResult{Card: "p3-r1", Result: "fault"})
	a, fs := NewEngine().Answer(Versions{}, w.snap(), FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	assert.Equal(t, "retry", a.Verdict["p3"])
	w.apply(store.SetCardResult{Card: "p3-r1", Result: "ok"})
	body, line := override("verdict", "def verdict(card, reads):\n    return \"retry\"")
	_, fs = NewEngine().Answer(Versions{Active: pv(2, body)}, w.snap(), FullQuestions(w.snap(), nil))
	require.Len(t, fs, 1)
	assert.Equal(t, fmt.Sprintf(`POLICY REFUSED v2 line %d verdict(p3): "retry" with a done ok or fix read`, line), fs[0].Error())
}
