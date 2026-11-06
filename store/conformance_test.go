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
		"seq fence and epoch":     fences,
		"writes":                  writes,
		"write refusals":          writeRefusals,
		"invalid":                 invalidOps,
		"refusal changes nothing": refusalChangesNothing,
		"idempotent":              idempotent,
		"clear":                   clearCarriesRows,
		"clear refusals":          clearRefusals,
		"rebuild":                 rebuild,
		"events":                  eventsPaging,
		"policy":                  policy,
		"times":                   times,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			scenario(&harness{t: t, s: newStore(t), ctx: context.Background(), epoch: 1})
		})
	}
}

type harness struct {
	t     *testing.T
	s     Store
	ctx   context.Context
	n     int
	epoch int
}

func (h *harness) op(kind string, expect []Expect, w ...Write) Op {
	h.n++
	return Op{ID: fmt.Sprintf("%s-%d", kind, h.n), Epoch: h.epoch, Actor: "tester", Kind: kind, At: t0.Add(time.Duration(h.n) * time.Second), Expect: expect, Writes: w, Note: fmt.Sprint("n", h.n)}
}

func (h *harness) ok(o Op) Receipt {
	h.t.Helper()
	r, err := h.s.Apply(h.ctx, o)
	require.NoError(h.t, err)
	require.True(h.t, r.Accepted, "%s refused: %s", o.ID, r.Reason)
	assert.Equal(h.t, Receipt{Op: o.ID, Kind: o.Kind, Actor: o.Actor, Epoch: o.Epoch, At: at(o.At), Note: o.Note, Accepted: true, Seq: r.Seq, After: r.After}, r)
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
	assert.Equal(h.t, o.Epoch, r.Epoch, o.ID)
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

func (h *harness) clear(expect ...Expect) Receipt {
	h.t.Helper()
	o := h.op("clear", expect)
	r, err := h.s.Clear(h.ctx, o)
	require.NoError(h.t, err)
	require.True(h.t, r.Accepted, r.Reason)
	h.epoch = r.NewEpoch
	return r
}

// The seed: stream s1; row w1 (worker, width 2, up); card c0 landed (gen 1);
// card c1 waiting, needs c0, gen 0; card c2 waiting, needs c1.
var (
	s1 = Stream{Name: "s1", Repo: "mas-bandwidth/x", Base: "main", Pos: 1}
	w1 = Row{Name: "w1", Kind: "worker", Width: 2, State: "up", Session: "sess-a"}
	c0 = Card{ID: "c0", Stream: "s1", State: "landed", Gen: 1}
	c1 = Card{ID: "c1", Stream: "s1", State: "waiting", Score: 10, Needs: []string{"c0"}, Brief: "do it"}
	c2 = Card{ID: "c2", Stream: "s1", State: "waiting", Needs: []string{"c1"}}
)

func (h *harness) seed() {
	h.t.Helper()
	h.ok(h.op("seed", nil, PutStream{"s1", s1.Repo, s1.Base, s1.Pos}, PutRow{"w1", "worker", 2, "sess-a"}, SetRowState{"w1", "up"},
		PutCard{ID: "c0", Stream: "s1"}, SetCardState{"c0", "landed"},
		PutCard{ID: "c1", Stream: "s1", Score: 10, Needs: []string{"c0"}, Brief: "do it"}, PutCard{ID: "c2", Stream: "s1", Needs: []string{"c1"}}))
}

func (h *harness) seeded() Snapshot {
	return Snapshot{Epoch: 1, Streams: map[string]Stream{"s1": s1}, Rows: map[string]Row{"w1": w1}, Cards: map[string]Card{"c0": c0, "c1": c1, "c2": c2}}
}

func conditions(h *harness) {
	h.seed()
	h.ok(h.op("take", nil, SetCardHolder{"c1", "w1"}))
	for _, c := range []struct {
		e      Expect
		reason string // "" when it holds
	}{
		{CardGen{"c1", 1}, ""},
		{CardGen{"c1", 0}, "card c1 is at gen 1, expected gen 0"},
		{CardGen{"c9", 0}, "card c9 does not exist"},
		{CardState{"c1", "waiting"}, ""},
		{CardState{"c1", "ready"}, "card c1 is waiting, expected ready"},
		{CardState{"c9", "ready"}, "card c9 does not exist"},
		{CardHolder{"c1", "w1"}, ""},
		{CardHolder{"c1", ""}, "card c1 has holder w1, expected none"},
		{CardHolder{"c2", "w1"}, "card c2 has holder none, expected w1"},
		{CardHolder{"c9", ""}, "card c9 does not exist"},
		{NeedsLanded{"c1"}, ""},
		{NeedsLanded{"c0"}, ""},
		{NeedsLanded{"c2"}, "need c1 of card c2 is waiting, not landed"},
		{NeedsLanded{"c9"}, "card c9 does not exist"},
		{RowIs{"w1", "up"}, ""},
		{RowIs{"w1", "down"}, "row w1 is up, expected down"},
		{RowIs{"w9", "up"}, "row w9 does not exist"},
		{RowUnder{"w1", 2}, ""},
		{RowUnder{"w1", 1}, "row w1 holds 1 cards, expected fewer than 1"},
		{RowUnder{"w9", 1}, "row w9 does not exist"},
		{StreamHeld{"s1", false}, ""},
		{StreamHeld{"s1", true}, "stream s1 is not held, expected held"},
		{StreamHeld{"s9", false}, "stream s9 does not exist"},
		{CardAbsent{"c9"}, ""},
		{CardAbsent{"c1"}, "card c1 exists"},
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
	h.refused(h.op("check", []Expect{CardHolder{"c1", "w1"}, RowIs{"w1", "up"}, CardState{"c1", "left"}}), "card c1 is waiting, expected left")
	assert.Equal(h.t, 1, h.snap().Cards["c1"].Gen, "conditions never change a row")
}

func fences(h *harness) {
	h.seed()
	seq := h.snap().Seq
	assert.Equal(h.t, int64(1), seq)
	h.ok(h.op("fence", []Expect{AtSeq{seq}}))
	h.refused(h.op("fence", []Expect{AtSeq{seq}}), "the store is at seq 2, expected seq 1")
	o := h.op("wrong", []Expect{CardAbsent{"c9"}}, SetCardState{"c1", "ready"})
	o.Epoch = 2
	h.refused(o, "the op is for epoch 2, the store is at epoch 1")
	assert.Equal(h.t, "waiting", h.snap().Cards["c1"].State)
	assert.Equal(h.t, int64(4), h.snap().Seq, "refusals are events")
}

func writes(h *harness) {
	r := h.ok(h.op("add", nil, PutStream{"s1", s1.Repo, s1.Base, s1.Pos}, PutRow{"w1", "worker", 2, "sess-a"}, PutCard{ID: "c0", Stream: "s1"},
		PutCard{ID: "c1", Stream: "s1", Score: 10, Needs: []string{"c0"}, Brief: "do it"}))
	row := Row{Name: "w1", Kind: "worker", Width: 2, State: "down", Session: "sess-a"}
	card0 := Card{ID: "c0", Stream: "s1", State: "waiting"}
	assert.Equal(h.t, &State{Streams: map[string]Stream{"s1": s1}, Rows: map[string]Row{"w1": row}, Cards: map[string]Card{"c0": card0, "c1": c1}}, r.After, "a row starts down, a card waiting at gen 0")
	r = h.ok(h.op("move", nil, SetCardState{"c1", "working"}, SetCardHolder{"c1", "w1"}, SetCardAttempt{"c1", 2}, SetCardBranch{"c1", "b1"},
		SetCardHead{"c1", "abc123"}, SetCardPR{"c1", 42}, SetCardReason{"c1", "why"}, SetCardScore{"c1", 3}, SetCardNeeds{"c1", nil}))
	want := Card{ID: "c1", Stream: "s1", State: "working", Gen: 9, Attempt: 2, Score: 3, Holder: "w1", Branch: "b1", Head: "abc123", PR: 42, Brief: "do it", Reason: "why"}
	assert.Equal(h.t, &State{Cards: map[string]Card{"c1": want}}, r.After, "every card write raises gen by one")
	h.ok(h.op("rows", nil, SetRowState{"w1", "blocked"}, SetRowSession{"w1", "sess-b"}, SetRowFlaps{"w1", 4}, SetStreamHeld{"s1", true}))
	h.ok(h.op("over", nil, PutRow{"w1", "kin", 1, "sess-c"}, PutStream{"s1", "other", "dev", 5}, SetCardHolder{"c1", ""}, SetCardNeeds{"c1", []string{"c0"}}))
	want.Holder, want.Needs, want.Gen = "", []string{"c0"}, 11
	got := h.snap()
	assert.Equal(h.t, Row{Name: "w1", Kind: "kin", Width: 1, State: "blocked", Session: "sess-c", Flaps: 4}, got.Rows["w1"], "put row keeps state and flaps")
	assert.Equal(h.t, Stream{Name: "s1", Repo: "other", Base: "dev", Held: true, Pos: 5}, got.Streams["s1"], "put stream keeps held")
	assert.Equal(h.t, map[string]Card{"c0": card0, "c1": want}, got.Cards)
	ev := h.events()
	require.Len(h.t, ev, 4)
	assert.Nil(h.t, ev[0].Before, "new rows have no before-image")
	row.State, row.Session, row.Flaps = "blocked", "sess-b", 4
	assert.Equal(h.t, &State{Streams: map[string]Stream{"s1": {Name: "s1", Repo: s1.Repo, Base: s1.Base, Held: true, Pos: 1}}, Rows: map[string]Row{"w1": row}, Cards: map[string]Card{"c1": ev[1].After.Cards["c1"]}}, ev[3].Before)
}

// strangeWrite and strangeExpect are outside the closed sets: they carry the
// marker method only by embedding.
type strangeWrite struct{ SetCardState }
type strangeExpect struct{ AtSeq }

func writeRefusals(h *harness) {
	h.seed()
	all := "waiting, ready, working, review, merging, landed, left"
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{PutCard{ID: "c1", Stream: "s1"}, "card c1 exists"},
		{PutCard{ID: "c3", Stream: "s9"}, "stream s9 does not exist"},
		{PutCard{ID: "c3", Stream: "s1", Needs: []string{"c3"}}, "card c3 cannot need itself"},
		{PutCard{ID: "c3", Stream: "s1", Needs: []string{"c0", "c0"}}, "card c3 names need c0 twice"},
		{PutCard{ID: "c3", Stream: "s1", Needs: []string{"c9"}}, "need c9 of card c3 does not exist"},
		{PutCard{Stream: "s1"}, "a card needs an id"},
		{SetCardState{"c1", "done"}, `card state "done" is not one of ` + all},
		{SetCardHolder{"c1", "w9"}, "row w9 does not exist"},
		{SetCardNeeds{"c1", []string{"c2", "c2"}}, "card c1 names need c2 twice"},
		{SetCardNeeds{"c1", []string{"c1"}}, "card c1 cannot need itself"},
		{SetCardNeeds{"c1", []string{"c9"}}, "need c9 of card c1 does not exist"},
		{SetStreamHeld{"s9", true}, "stream s9 does not exist"},
		{PutStream{}, "a stream needs a name"},
		{PutRow{Name: "w2", Kind: "robot"}, `row kind "robot" is not one of worker, kin`},
		{PutRow{Kind: "kin"}, "a row needs a name"},
		{SetRowState{"w1", "gone"}, `row state "gone" is not one of up, down, blocked`},
		{SetRowState{"w9", "down"}, "row w9 does not exist"},
		{SetRowSession{"w9", "s"}, "row w9 does not exist"},
		{SetRowFlaps{"w9", 1}, "row w9 does not exist"},
		{PutPolicy{"yaml", "p", "b"}, `policy kind "yaml" is not one of script, data`},
		{SetActivePolicy{3}, "policy version 3 does not exist"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	for _, w := range []Write{SetCardState{"c9", "ready"}, SetCardHolder{"c9", ""}, SetCardAttempt{"c9", 1}, SetCardBranch{"c9", "b"}, SetCardHead{"c9", "h"},
		SetCardPR{"c9", 1}, SetCardReason{"c9", "r"}, SetCardScore{"c9", 1}, SetCardNeeds{"c9", nil}} {
		h.refused(h.op("bad", nil, w), "card c9 does not exist")
	}
	got := h.snap()
	got.Seq = 0
	assert.Equal(h.t, h.seeded(), got, "refused writes change nothing")
}

func invalidOps(h *harness) {
	h.seed()
	n := len(h.events())
	for name, o := range map[string]Op{
		"no id":             {Epoch: 1},
		"no epoch":          {ID: "x"},
		"strange write":     h.op("x", nil, strangeWrite{SetCardState{"c1", "ready"}}),
		"nil write":         h.op("x", nil, nil),
		"strange condition": h.op("x", []Expect{strangeExpect{}}),
		"nil condition":     h.op("x", []Expect{nil}),
		"state twice":       h.op("x", nil, SetCardState{"c1", "ready"}, SetCardState{"c1", "working"}),
		"add then score":    h.op("x", nil, PutCard{ID: "c3", Stream: "s1"}, SetCardScore{"c3", 2}),
		"stream twice":      h.op("x", nil, PutStream{Name: "s1"}, PutStream{Name: "s1"}),
		"row then session":  h.op("x", nil, PutRow{"w1", "kin", 1, ""}, SetRowSession{"w1", "s"}),
		"activate twice":    h.op("x", nil, SetActivePolicy{1}, SetActivePolicy{2}),
	} {
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
		assert.NotErrorIs(h.t, err, ErrUnknown, name)
	}
	for name, o := range map[string]Op{
		"clear with writes":    h.op("clear", nil, SetCardState{"c1", "ready"}),
		"clear with condition": h.op("clear", []Expect{CardAbsent{"c1"}}),
		"clear of other kind":  h.op("wipe", nil),
		"clear with no epoch":  {ID: "y"},
	} {
		_, err := h.s.Clear(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
	}
	assert.Len(h.t, h.events(), n, "nothing invalid is logged")
	h.ok(h.op("fine", nil, SetCardState{"c1", "ready"}, SetCardHolder{"c1", "w1"}))
}

func refusalChangesNothing(h *harness) {
	h.seed()
	before := h.snap()
	o := h.op("partial", []Expect{CardGen{"c1", 0}}, PutStream{Name: "s2"}, SetCardState{"c1", "working"}, SetCardHolder{"c1", "w9"})
	h.refused(o, "row w9 does not exist")
	after := h.snap()
	after.Seq--
	assert.Equal(h.t, before, after, "the writes before the refused one are undone")
	ev := h.events()
	require.Len(h.t, ev, 2)
	last := ev[1]
	assert.Equal(h.t, Event{Seq: 2, Op: o.ID, Actor: "tester", Kind: "partial", Reason: "row w9 does not exist", Receipt: last.Receipt, At: at(o.At), Epoch: 1}, last)
	assert.Equal(h.t, Receipt{Op: o.ID, Kind: "partial", Actor: "tester", Epoch: 1, At: at(o.At), Note: o.Note, Reason: "row w9 does not exist", Seq: 2}, last.Receipt)
}

func idempotent(h *harness) {
	h.seed()
	o := h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"})
	first := h.ok(o)
	after := h.snap()
	again := o
	again.Writes, again.Expect, again.Actor, again.Kind = []Write{SetCardState{"c1", "left"}}, []Expect{AtSeq{99}}, "someone", "other"
	r, err := h.s.Apply(h.ctx, again)
	require.NoError(h.t, err)
	assert.Equal(h.t, first, r, "a second Apply with the id returns the stored receipt whatever its body")
	assert.Equal(h.t, "tester", r.Actor, "the receipt names the original op")
	assert.Equal(h.t, after, h.snap())
	got, ok, err := h.s.Replay(h.ctx, o.ID)
	require.NoError(h.t, err)
	assert.True(h.t, ok)
	assert.Equal(h.t, first, got)
	_, ok, err = h.s.Replay(h.ctx, "never-seen")
	require.NoError(h.t, err)
	assert.False(h.t, ok)
	no := h.op("bad", nil, SetCardState{"c1", "nope"})
	refused := h.refused(no, `card state "nope" is not one of waiting, ready, working, review, merging, landed, left`)
	no.Writes = nil
	r, err = h.s.Apply(h.ctx, no)
	require.NoError(h.t, err)
	assert.Equal(h.t, refused, r, "a refused id stays refused")
	c := h.clear()
	r, err = h.s.Clear(h.ctx, Op{ID: c.Op, Epoch: 2, Kind: "clear"})
	require.NoError(h.t, err)
	assert.Equal(h.t, c, r, "a clear replays by op id")
	assert.Len(h.t, h.events(), 4, "a replay logs nothing")
}

func clearCarriesRows(h *harness) {
	h.seed()
	h.ok(h.op("busy", nil, SetRowFlaps{"w1", 3}, SetCardHolder{"c1", "w1"}, PutRow{"k1", "kin", 1, "sess-k"}))
	h.ok(h.op("pol", nil, PutPolicy{"data", "p", "{}"}, SetActivePolicy{1}))
	r := h.clear(AtSeq{3})
	carried := map[string]Row{"w1": {Name: "w1", Kind: "worker", Width: 2, State: "down", Session: "sess-a"}, "k1": {Name: "k1", Kind: "kin", Width: 1, State: "down", Session: "sess-k"}}
	assert.Equal(h.t, Receipt{Op: r.Op, Kind: "clear", Actor: "tester", Epoch: 1, At: r.At, Note: r.Note, Accepted: true, Seq: 4, NewEpoch: 2, After: &State{Epoch: 2, Rows: carried}}, r)
	assert.Equal(h.t, Snapshot{Seq: 4, Epoch: 2, Policy: 1, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: carried},
		h.snap(), "rows are carried down with no flaps; streams and cards are not; policy is kept")
	ev := h.events()
	require.Len(h.t, ev, 4)
	assert.Equal(h.t, 1, ev[3].Epoch, "a clear is logged with the epoch it closed")
	assert.Equal(h.t, "clear", ev[3].Kind)
	assert.Equal(h.t, &State{Epoch: 2, Rows: carried}, ev[3].After)
	h.refused(h.op("old", nil, SetCardState{"c1", "left"}), "card c1 does not exist")
	o := h.op("stale", nil)
	o.Epoch = 1
	h.refused(o, "the op is for epoch 1, the store is at epoch 2")
	h.ok(h.op("again", []Expect{CardAbsent{"c1"}, RowIs{"w1", "down"}, RowUnder{"w1", 1}}, PutStream{Name: "s1"}, PutCard{ID: "c1", Stream: "s1"}, SetCardHolder{"c1", "w1"}))
	assert.Equal(h.t, map[string]Card{"c1": {ID: "c1", Stream: "s1", State: "waiting", Gen: 1, Holder: "w1"}}, h.snap().Cards)
	assert.Equal(h.t, 2, h.events()[6].Epoch)
}

func clearRefusals(h *harness) {
	h.seed()
	o := h.op("clear", []Expect{AtSeq{7}})
	r, err := h.s.Clear(h.ctx, o)
	require.NoError(h.t, err)
	assert.False(h.t, r.Accepted)
	assert.Equal(h.t, "the store is at seq 1, expected seq 7", r.Reason)
	assert.Zero(h.t, r.NewEpoch)
	o = h.op("clear", nil)
	o.Epoch = 5
	r, err = h.s.Clear(h.ctx, o)
	require.NoError(h.t, err)
	assert.Equal(h.t, "the op is for epoch 5, the store is at epoch 1", r.Reason)
	got := h.snap()
	assert.Equal(h.t, 1, got.Epoch)
	assert.Len(h.t, got.Cards, 3)
	ev := h.events()
	require.Len(h.t, ev, 3, "a refused clear is an event")
	assert.False(h.t, ev[2].Accepted)
	assert.Nil(h.t, ev[2].After)
}

func rebuild(h *harness) {
	h.seed()
	h.ok(h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"}, SetCardHolder{"c1", "w1"}))
	h.refused(h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"}), "card c1 is at gen 2, expected gen 0")
	h.ok(h.op("pol", nil, PutPolicy{"script", "p1", "x"}, PutPolicy{"data", "p2", "y"}, SetActivePolicy{2}))
	assert.Equal(h.t, Rebuild(h.events()), h.snap())
	h.clear()
	h.ok(h.op("seed2", nil, PutStream{Name: "s1"}, PutCard{ID: "c1", Stream: "s1"}, SetActivePolicy{1}, SetRowSession{"w1", "sess-z"}))
	h.ok(h.op("rows", nil, SetRowState{"w1", "up"}, SetCardReason{"c1", "r"}))
	got := h.snap()
	assert.Equal(h.t, Rebuild(h.events()), got, "the clear's after-image carries the rows")
	assert.Equal(h.t, 1, got.Policy)
	assert.Equal(h.t, 2, got.Epoch)
	assert.Equal(h.t, "sess-z", got.Rows["w1"].Session)
}

func eventsPaging(h *harness) {
	for i := range 5 {
		h.ok(h.op("s", nil, PutStream{Name: fmt.Sprint("s", i)}))
	}
	all := h.events()
	require.Len(h.t, all, 5)
	for i, e := range all {
		assert.Equal(h.t, int64(i+1), e.Seq, "seq is gapless and in order")
	}
	for _, c := range []struct {
		after int64
		max   int
		want  []Event
	}{{0, 2, all[:2]}, {2, 0, all[2:]}, {2, -1, all[2:]}, {4, 10, all[4:]}, {5, 10, nil}} {
		page, err := h.s.Events(h.ctx, c.after, c.max)
		require.NoError(h.t, err)
		assert.Equal(h.t, c.want, page, "after %d max %d", c.after, c.max)
	}
}

func policy(h *harness) {
	r := h.ok(h.op("pol", nil, PutPolicy{"script", "deal", "def deal(): pass"}, PutPolicy{"data", "limits", "{}"}))
	when := at(t0.Add(time.Second))
	p1 := PolicyVersion{Version: 1, Kind: "script", Name: "deal", Body: "def deal(): pass", Actor: "tester", At: when}
	p2 := PolicyVersion{Version: 2, Kind: "data", Name: "limits", Body: "{}", Actor: "tester", At: when}
	assert.Equal(h.t, &State{Policies: map[int]PolicyVersion{1: p1, 2: p2}}, r.After)
	assert.Equal(h.t, 0, h.snap().Policy)
	h.ok(h.op("on", nil, SetActivePolicy{1}))
	assert.Equal(h.t, 1, h.snap().Policy)
	r = h.ok(h.op("on", nil, SetActivePolicy{2}))
	p2.Active = true
	assert.Equal(h.t, &State{Policies: map[int]PolicyVersion{1: p1, 2: p2}}, r.After, "the old active version is switched off")
	assert.Equal(h.t, 2, h.snap().Policy)
	got, err := h.s.Policy(h.ctx, 2)
	require.NoError(h.t, err)
	assert.Equal(h.t, p2, got)
	_, err = h.s.Policy(h.ctx, 9)
	assert.ErrorIs(h.t, err, ErrNotFound)
	r = h.ok(h.op("pol", nil, PutPolicy{"data", "limits", `{"a":1}`}))
	assert.Equal(h.t, 3, r.After.Policies[3].Version)
	assert.Equal(h.t, 2, h.snap().Policy, "a new version is inactive")
}

func times(h *harness) {
	o := h.op("t", nil, PutStream{Name: "s1"})
	o.At = t0.In(time.FixedZone("x", 3600))
	r := h.ok(o)
	assert.Equal(h.t, t0.Truncate(time.Microsecond), r.At)
	assert.Equal(h.t, r.At, h.events()[0].At)
	assert.Equal(h.t, r.At, h.events()[0].Receipt.At)
}
