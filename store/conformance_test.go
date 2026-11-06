package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t0 is the suite's clock: every op's At is t0 plus its number in seconds. The
// nanoseconds check that the store keeps times to the microsecond.
var t0 = time.Date(2026, 11, 6, 12, 0, 0, 123456789, time.UTC)

// RunConformance runs the same scenarios against any Store. newStore gives
// each scenario an empty store of its own. The unit tests run it against Fake
// and the functional tests against PG, so the two cannot drift.
func RunConformance(t *testing.T, newStore func(t *testing.T) Store) {
	for name, scenario := range map[string]func(h *harness){
		"conditions":              conditions,
		"writes":                  writes,
		"write refusals":          writeRefusals,
		"refusal changes nothing": refusalChangesNothing,
		"idempotent":              idempotent,
		"clear":                   clearScopes,
		"rebuild":                 rebuild,
		"events":                  eventsPaging,
		"policy":                  policy,
		"times":                   times,
		"bad ops":                 badOps,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			scenario(&harness{t: t, s: newStore(t), ctx: context.Background()})
		})
	}
}

type harness struct {
	t   *testing.T
	s   Store
	ctx context.Context
	n   int
}

func (h *harness) op(kind string, expect []Expect, w ...Write) Op {
	h.n++
	return Op{ID: fmt.Sprintf("%s-%d", kind, h.n), Actor: "tester", Kind: kind, At: t0.Add(time.Duration(h.n) * time.Second), Expect: expect, Writes: w, Note: "n" + fmt.Sprint(h.n)}
}

func (h *harness) ok(o Op) Receipt {
	h.t.Helper()
	r, err := h.s.Apply(h.ctx, o)
	require.NoError(h.t, err)
	require.True(h.t, r.Accepted, "%s refused: %s", o.ID, r.Reason)
	assert.Equal(h.t, o.ID, r.Op)
	assert.Equal(h.t, o.Kind, r.Kind)
	assert.Equal(h.t, o.Note, r.Note)
	assert.Positive(h.t, r.Seq)
	return r
}

func (h *harness) refused(o Op, reason string) Receipt {
	h.t.Helper()
	r, err := h.s.Apply(h.ctx, o)
	require.NoError(h.t, err)
	assert.False(h.t, r.Accepted, o.ID)
	assert.Equal(h.t, reason, r.Reason, o.ID)
	assert.Nil(h.t, r.After, o.ID)
	return r
}

func (h *harness) snap() Snapshot {
	h.t.Helper()
	s, err := h.s.Snapshot(h.ctx)
	require.NoError(h.t, err)
	return s
}

func (h *harness) events() []Event {
	h.t.Helper()
	e, err := h.s.Events(h.ctx, 0, 0)
	require.NoError(h.t, err)
	return e
}

var (
	s1 = Stream{Name: "s1", Repo: "mas-bandwidth/x", Base: "main", Pos: 1}
	w1 = Row{Name: "w1", Kind: "worker", Width: 2, State: "up", Seen: t0.Truncate(time.Microsecond)}
	c1 = Card{ID: "c1", Stream: "s1", State: "ready", Score: 10, Needs: []string{"a", "b"}, Brief: "do it"}
)

// seed adds stream s1, row w1 and card c1 (ready, gen 0, no holder).
func (h *harness) seed() {
	h.t.Helper()
	h.ok(h.op("seed", nil, PutStream{s1}, PutRow{w1}, PutCard{c1}))
}

func conditions(h *harness) {
	h.seed()
	h.ok(h.op("take", []Expect{CardIs{"c1", "ready", 0}}, SetCardHolder{"c1", "w1"}))
	before := h.snap()
	for _, c := range []struct {
		e      Expect
		reason string // "" when it holds
	}{
		{CardIs{"c1", "ready", 0}, ""},
		{CardIs{"c1", "ready", 1}, "card c1 is at gen 0, expected gen 1"},
		{CardIs{"c1", "working", 0}, "card c1 is ready, expected working"},
		{CardIs{"c9", "ready", 0}, "card c9 does not exist"},
		{CardHolder{"c1", "w1"}, ""},
		{CardHolder{"c1", ""}, "card c1 has holder w1, expected none"},
		{CardHolder{"c9", ""}, "card c9 does not exist"},
		{RowIs{"w1", "up"}, ""},
		{RowIs{"w1", "down"}, "row w1 is up, expected down"},
		{RowIs{"w9", "up"}, "row w9 does not exist"},
		{StreamHeld{"s1", false}, ""},
		{StreamHeld{"s1", true}, "stream s1 is not held, expected held"},
		{StreamHeld{"s9", false}, "stream s9 does not exist"},
		{CardAbsent{"c9"}, ""},
		{CardAbsent{"c1"}, "card c1 exists"},
		{EpochIs{0}, ""},
		{EpochIs{1}, "the epoch is 0, expected 1"},
	} {
		o := h.op("check", []Expect{c.e})
		if c.reason == "" {
			assert.Nil(h.t, h.ok(o).After, "%#v", c.e)
		} else {
			h.refused(o, c.reason)
		}
	}
	h.ok(h.op("hold", nil, SetStreamHeld{"s1", true}))
	h.ok(h.op("check", []Expect{StreamHeld{"s1", true}}))
	h.refused(h.op("check", []Expect{StreamHeld{"s1", false}}), "stream s1 is held, expected not held")
	h.refused(h.op("check", []Expect{CardHolder{"c1", "w1"}, RowIs{"w1", "up"}, CardIs{"c1", "left", 0}}), "card c1 is ready, expected left")
	h.ok(h.op("release", []Expect{CardHolder{"c1", "w1"}}, SetCardHolder{"c1", ""}))
	h.refused(h.op("check", []Expect{CardHolder{"c1", "w1"}}), "card c1 has holder none, expected w1")
	assert.Equal(h.t, before.Cards["c1"].Gen, h.snap().Cards["c1"].Gen, "conditions never change a row")
}

func writes(h *harness) {
	r := h.ok(h.op("seed", nil, PutStream{s1}, PutRow{w1}, PutCard{Card{ID: "c1", Stream: "s1", State: "ready", Gen: 7, Score: 10, Needs: []string{"a", "b"}, Brief: "do it", Holder: "w1"}}))
	want := c1
	want.Holder = "w1"
	assert.Equal(h.t, &State{Streams: map[string]Stream{"s1": s1}, Rows: map[string]Row{"w1": w1}, Cards: map[string]Card{"c1": want}}, r.After, "a card starts at gen 0")
	r = h.ok(h.op("move", nil, SetCardState{"c1", "working"}, SetCardState{"c1", "review"}, SetCardHeadPR{"c1", "abc123", 42}, SetCardReason{"c1", "why"}, SetCardHolder{"c1", ""}))
	want.State, want.Gen, want.Head, want.PR, want.Reason, want.Holder = "review", 2, "abc123", 42, "why", ""
	assert.Equal(h.t, &State{Cards: map[string]Card{"c1": want}}, r.After)
	s2 := s1
	s2.Repo, s2.Pos = "other", 5
	h.ok(h.op("stream", nil, PutStream{s2}, SetStreamHeld{"s1", true}))
	s2.Held = true
	w2 := Row{Name: "k1", Kind: "kin", Width: 1, State: "up"}
	h.ok(h.op("rows", nil, PutRow{w2}, SetRowState{"w1", "blocked", t0.Add(time.Hour), 4}))
	w1b := w1
	w1b.State, w1b.Seen, w1b.Flaps = "blocked", t0.Add(time.Hour).Truncate(time.Microsecond), 4
	got := h.snap()
	assert.Equal(h.t, Snapshot{Epoch: 0, Streams: map[string]Stream{"s1": s2}, Cards: map[string]Card{"c1": want}, Rows: map[string]Row{"w1": w1b, "k1": w2}}, got)
	ev := h.events()
	require.Len(h.t, ev, 4)
	assert.Equal(h.t, &State{Streams: map[string]Stream{"s1": s1}}, ev[2].Before, "before is the row as it was")
	assert.Equal(h.t, &State{Streams: map[string]Stream{"s1": s2}}, ev[2].After)
	assert.Equal(h.t, &State{Rows: map[string]Row{"w1": w1}}, ev[3].Before, "a new row has no before")
	assert.Nil(h.t, ev[0].Before)
}

// strangeWrite and strangeExpect are outside the closed sets: they carry the
// marker method only by embedding.
type strangeWrite struct{ PutStream }
type strangeExpect struct{ EpochIs }

func writeRefusals(h *harness) {
	h.seed()
	all := "waiting, ready, working, review, merging, landed, left"
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{PutCard{c1}, "card c1 exists"},
		{PutCard{Card{ID: "c2", Stream: "s9", State: "ready"}}, "stream s9 does not exist"},
		{PutCard{Card{ID: "c2", Stream: "s1", State: "ready", Holder: "w9"}}, "row w9 does not exist"},
		{PutCard{Card{ID: "c2", Stream: "s1", State: "flying"}}, `card state "flying" is not one of ` + all},
		{PutCard{Card{Stream: "s1", State: "ready"}}, "a card needs an id"},
		{SetCardState{"c9", "ready"}, "card c9 does not exist"},
		{SetCardState{"c1", "done"}, `card state "done" is not one of ` + all},
		{SetCardHolder{"c1", "w9"}, "row w9 does not exist"},
		{SetCardHolder{"c9", "w1"}, "card c9 does not exist"},
		{SetCardHeadPR{"c9", "h", 1}, "card c9 does not exist"},
		{SetCardReason{"c9", "r"}, "card c9 does not exist"},
		{SetStreamHeld{"s9", true}, "stream s9 does not exist"},
		{PutStream{Stream{}}, "a stream needs a name"},
		{PutRow{Row{Name: "w2", Kind: "robot", State: "up"}}, `row kind "robot" is not one of worker, kin`},
		{PutRow{Row{Name: "w2", Kind: "kin", State: "asleep"}}, `row state "asleep" is not one of up, down, blocked`},
		{PutRow{Row{Kind: "kin", State: "up"}}, "a row needs a name"},
		{SetRowState{"w9", "down", t0, 0}, "row w9 does not exist"},
		{SetRowState{"w1", "gone", t0, 0}, `row state "gone" is not one of up, down, blocked`},
		{PutPolicy{"yaml", "p", "b"}, `policy kind "yaml" is not one of script, data`},
		{SetActivePolicy{3}, "policy version 3 does not exist"},
		{strangeWrite{PutStream{s1}}, "unknown write store.strangeWrite"},
		{nil, "unknown write <nil>"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	h.refused(h.op("bad", []Expect{strangeExpect{}}), "unknown condition store.strangeExpect")
	h.refused(h.op("bad", []Expect{nil}), "unknown condition <nil>")
	assert.Equal(h.t, Snapshot{Streams: map[string]Stream{"s1": s1}, Rows: map[string]Row{"w1": w1}, Cards: map[string]Card{"c1": c1}}, h.snap())
}

func refusalChangesNothing(h *harness) {
	h.seed()
	before := h.snap()
	o := h.op("partial", []Expect{CardIs{"c1", "ready", 0}}, PutStream{Stream{Name: "s2"}}, SetCardState{"c1", "working"}, SetCardHolder{"c1", "w9"})
	h.refused(o, "row w9 does not exist")
	assert.Equal(h.t, before, h.snap(), "the writes before the refused one are undone")
	ev := h.events()
	require.Len(h.t, ev, 2)
	last := ev[1]
	assert.False(h.t, last.Accepted)
	assert.Equal(h.t, "row w9 does not exist", last.Reason)
	assert.Equal(h.t, o.ID, last.Op)
	assert.Equal(h.t, "tester", last.Actor)
	assert.Equal(h.t, "partial", last.Kind)
	assert.Nil(h.t, last.Before)
	assert.Nil(h.t, last.After)
	assert.Equal(h.t, last.Receipt.Reason, last.Reason)
	assert.Equal(h.t, last.Seq, last.Receipt.Seq)
}

func idempotent(h *harness) {
	h.seed()
	o := h.op("take", []Expect{CardIs{"c1", "ready", 0}}, SetCardState{"c1", "working"})
	first := h.ok(o)
	after := h.snap()
	again := o
	again.Writes = []Write{SetCardState{"c1", "left"}}
	again.Expect = []Expect{EpochIs{9}}
	r, err := h.s.Apply(h.ctx, again)
	require.NoError(h.t, err)
	assert.Equal(h.t, first, r, "a second Apply with the id returns the stored receipt whatever its body")
	assert.Equal(h.t, after, h.snap())
	got, ok, err := h.s.Replay(h.ctx, o.ID)
	require.NoError(h.t, err)
	assert.True(h.t, ok)
	assert.Equal(h.t, first, got)
	_, ok, err = h.s.Replay(h.ctx, "never-seen")
	require.NoError(h.t, err)
	assert.False(h.t, ok)
	no := h.op("bad", []Expect{EpochIs{5}})
	refused := h.refused(no, "the epoch is 0, expected 5")
	no.Expect = nil
	r, err = h.s.Apply(h.ctx, no)
	require.NoError(h.t, err)
	assert.Equal(h.t, refused, r, "a refused id stays refused")
	assert.Len(h.t, h.events(), 3, "a replay logs nothing")
}

func clearScopes(h *harness) {
	h.seed()
	h.ok(h.op("pol", nil, PutPolicy{"data", "p", "{}"}, SetActivePolicy{1}))
	o := h.op("clear", nil)
	o.Kind = ""
	n, err := h.s.Clear(h.ctx, o)
	require.NoError(h.t, err)
	assert.Equal(h.t, 1, n)
	assert.Equal(h.t, Snapshot{Epoch: 1, Policy: 1, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: map[string]Row{}}, h.snap(), "the policy is not epoch scoped")
	n, err = h.s.Clear(h.ctx, o)
	require.NoError(h.t, err)
	assert.Equal(h.t, 1, n, "a clear replays by op id")
	h.refused(h.op("old", []Expect{EpochIs{0}}), "the epoch is 1, expected 0")
	h.refused(h.op("old", nil, SetCardState{"c1", "left"}), "card c1 does not exist")
	h.ok(h.op("again", []Expect{EpochIs{1}, CardAbsent{"c1"}}, PutStream{s1}, PutCard{c1}))
	assert.Equal(h.t, map[string]Card{"c1": c1}, h.snap().Cards)
	ev := h.events()
	require.Len(h.t, ev, 6)
	assert.Equal(h.t, "clear", ev[2].Kind)
	assert.True(h.t, ev[2].Accepted)
	assert.Equal(h.t, 1, ev[2].Epoch)
	assert.Equal(h.t, &State{Epoch: 1}, ev[2].After)
	_, err = h.s.Clear(h.ctx, h.op("clear", []Expect{EpochIs{1}}))
	assert.Error(h.t, err, "a clear takes no conditions")
	_, err = h.s.Clear(h.ctx, Op{})
	assert.Error(h.t, err)
}

func rebuild(h *harness) {
	h.seed()
	h.ok(h.op("take", []Expect{CardIs{"c1", "ready", 0}}, SetCardState{"c1", "working"}, SetCardHolder{"c1", "w1"}))
	h.refused(h.op("take", []Expect{CardIs{"c1", "ready", 0}}, SetCardState{"c1", "working"}), "card c1 is at gen 1, expected gen 0")
	h.ok(h.op("pol", nil, PutPolicy{"script", "p1", "x"}, PutPolicy{"data", "p2", "y"}, SetActivePolicy{2}))
	assert.Equal(h.t, Rebuild(h.events()), h.snap())
	_, err := h.s.Clear(h.ctx, h.op("clear", nil))
	require.NoError(h.t, err)
	h.ok(h.op("seed2", nil, PutStream{s1}, PutRow{w1}, PutCard{c1}, SetActivePolicy{1}))
	h.ok(h.op("rows", nil, SetRowState{"w1", "down", t0.Add(time.Minute), 1}, SetCardReason{"c1", "r"}))
	got := h.snap()
	assert.Equal(h.t, Rebuild(h.events()), got)
	assert.Equal(h.t, 1, got.Policy)
	assert.Equal(h.t, 1, got.Epoch)
}

func eventsPaging(h *harness) {
	for range 5 {
		h.ok(h.op("s", nil, PutStream{s1}))
	}
	all := h.events()
	require.Len(h.t, all, 5)
	for i, e := range all {
		assert.Equal(h.t, int64(i+1), e.Seq, "seq is gapless and in order")
	}
	page, err := h.s.Events(h.ctx, 0, 2)
	require.NoError(h.t, err)
	assert.Equal(h.t, all[:2], page)
	page, err = h.s.Events(h.ctx, 2, 0)
	require.NoError(h.t, err)
	assert.Equal(h.t, all[2:], page)
	page, err = h.s.Events(h.ctx, 5, 10)
	require.NoError(h.t, err)
	assert.Empty(h.t, page)
}

func policy(h *harness) {
	r := h.ok(h.op("pol", nil, PutPolicy{"script", "deal", "def deal(): pass"}, PutPolicy{"data", "limits", "{}"}))
	at := t0.Add(time.Second).Truncate(time.Microsecond)
	p1 := Policy{Version: 1, Kind: "script", Name: "deal", Body: "def deal(): pass", Actor: "tester", At: at}
	p2 := Policy{Version: 2, Kind: "data", Name: "limits", Body: "{}", Actor: "tester", At: at}
	assert.Equal(h.t, &State{Policies: map[int]Policy{1: p1, 2: p2}}, r.After)
	assert.Equal(h.t, 0, h.snap().Policy)
	h.ok(h.op("on", nil, SetActivePolicy{1}))
	assert.Equal(h.t, 1, h.snap().Policy)
	r = h.ok(h.op("on", nil, SetActivePolicy{2}))
	p2.Active = true
	assert.Equal(h.t, &State{Policies: map[int]Policy{1: p1, 2: p2}}, r.After, "the old active version is switched off")
	assert.Equal(h.t, 2, h.snap().Policy)
	r = h.ok(h.op("pol", nil, PutPolicy{"data", "limits", "{\"a\":1}"}))
	assert.Equal(h.t, 3, r.After.Policies[3].Version)
	assert.Equal(h.t, 2, h.snap().Policy, "a new version is inactive")
}

func times(h *harness) {
	o := h.op("t", nil, PutRow{Row{Name: "w1", Kind: "worker", State: "up", Seen: t0.In(time.FixedZone("x", 3600))}})
	r := h.ok(o)
	assert.Equal(h.t, o.At.UTC().Truncate(time.Microsecond), r.At)
	assert.Equal(h.t, t0.Truncate(time.Microsecond), h.snap().Rows["w1"].Seen)
	h.ok(h.op("t", nil, PutRow{Row{Name: "w2", Kind: "worker", State: "up"}}))
	assert.True(h.t, h.snap().Rows["w2"].Seen.IsZero(), "never seen stays zero")
	assert.Equal(h.t, r.At, h.events()[0].At)
}

func badOps(h *harness) {
	_, err := h.s.Apply(h.ctx, Op{Kind: "x"})
	assert.Error(h.t, err, "an op needs an id")
	assert.NotErrorIs(h.t, err, ErrUnknown)
	assert.Empty(h.t, h.events(), "nothing is logged for an op with no id")
}
