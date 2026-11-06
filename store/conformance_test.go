package store

import (
	"context"
	"fmt"
	"math"
	"sync"
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
		"gen once per op":         genOncePerOp,
		"row width":               rowWidth,
		"author and base sha":     authorAndBase,
		"width in one op":         widthInOneOp,
		"working count":           workingCount,
		"row gen race":            rowGenRace,
		"sentinels":               sentinels,
		"after":                   afterOrder,
		"blocked":                 blockedState,
		"row held":                rowHeld,
		"lesson columns":          lessonColumns,
		"int range":               intRange,
		"nul bytes":               nulBytes,
		"text and time":           textAndTime,
		"known id, invalid body":  knownIDInvalidBody,
		"snapshot is a copy":      snapshotIsCopy,
		"same id concurrent":      sameIDConcurrent,
		"clear races apply":       clearRacesApply,
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

// The seed: stream s1; row w1 (worker, width 2, up, gen 0: added in the op);
// producer c0 landed (gen 0: added and landed in one op); producer c1
// waiting, needs c0; producer c2 waiting, needs c1; consumers u1 (work of
// c1), u2 (read of c1) and u3 (work of c2), waiting with no holder.
var (
	s1 = Stream{Name: "s1", Repo: "mas-bandwidth/x", Base: "main", Pos: 1}
	w1 = Row{Name: "w1", Kind: "worker", Width: 2, State: "up", Session: "sess-a"}
	c0 = Card{Priority: "normal", ID: "c0", Kind: "producer", Stream: "s1", State: "landed"}
	c1 = Card{Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "waiting", Score: 10, Needs: []string{"c0"}, Brief: "do it"}
	c2 = Card{Priority: "normal", ID: "c2", Kind: "producer", Stream: "s1", State: "waiting", Needs: []string{"c1"}}
	u1 = Card{Priority: "normal", ID: "u1", Kind: "work", Of: "c1", Stream: "s1", State: "waiting"}
	u2 = Card{Priority: "normal", ID: "u2", Kind: "read", Of: "c1", Stream: "s1", State: "waiting"}
	u3 = Card{Priority: "normal", ID: "u3", Kind: "work", Of: "c2", Stream: "s1", State: "waiting"}
)

func (h *harness) seed() {
	h.t.Helper()
	h.ok(h.op("seed", nil, PutStream{"s1", s1.Repo, s1.Base, s1.Pos}, PutRow{"w1", "worker", 2, "sess-a"}, SetRowState{"w1", "up"},
		PutCard{Kind: "producer", ID: "c0", Stream: "s1"}, SetCardState{"c0", "landed"},
		PutCard{Kind: "producer", ID: "c1", Stream: "s1", Score: 10, Needs: []string{"c0"}, Brief: "do it"}, PutCard{Kind: "producer", ID: "c2", Stream: "s1", Needs: []string{"c1"}},
		PutCard{ID: "u1", Kind: "work", Of: "c1", Stream: "s1"}, PutCard{ID: "u2", Kind: "read", Of: "c1", Stream: "s1"}, PutCard{ID: "u3", Kind: "work", Of: "c2", Stream: "s1"}))
}

func (h *harness) seeded() Snapshot {
	return Snapshot{Epoch: 1, Streams: map[string]Stream{"s1": s1}, Rows: map[string]Row{"w1": w1}, Cards: map[string]Card{"c0": c0, "c1": c1, "c2": c2, "u1": u1, "u2": u2, "u3": u3}}
}

func conditions(h *harness) {
	h.seed()
	h.ok(h.op("take", nil, SetCardHolder{"u1", "w1"}, SetCardReason{"c1", "r"}, SetRowFlaps{"w1", 1}))
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
		{CardHolder{"u1", "w1"}, ""},
		{CardHolder{"u1", ""}, "card u1 has holder w1, expected none"},
		{CardHolder{"u2", "w1"}, "card u2 has holder none, expected w1"},
		{RowGen{"w1", 1}, ""},
		{RowGen{"w1", 0}, "row w1 is at gen 1, expected gen 0"},
		{RowGen{"w9", 0}, "row w9 does not exist"},
		{CardHolder{"c9", ""}, "card c9 does not exist"},
		{NeedsLanded{"c1"}, ""},
		{NeedsLanded{"c0"}, ""},
		{NeedsLanded{"c2"}, "need c1 of card c2 is waiting, not landed"},
		{NeedsLanded{"c9"}, "card c9 does not exist"},
		{RowIs{"w1", "up"}, ""},
		{RowIs{"w1", "down"}, "row w1 is up, expected down"},
		{RowIs{"w9", "up"}, "row w9 does not exist"},
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
	h.refused(h.op("check", []Expect{CardHolder{"u1", "w1"}, RowIs{"w1", "up"}, CardState{"c1", "fail"}}), "card c1 is waiting, expected fail")
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
	r := h.ok(h.op("add", nil, PutStream{"s1", s1.Repo, s1.Base, s1.Pos}, PutRow{"w1", "worker", 2, "sess-a"}, PutCard{Kind: "producer", ID: "c0", Stream: "s1"},
		PutCard{Kind: "producer", ID: "c1", Stream: "s1", Score: 10, Needs: []string{"c0"}, Brief: "do it"}))
	row := Row{Name: "w1", Kind: "worker", Width: 2, State: "down", Session: "sess-a"}
	card0 := Card{Priority: "normal", ID: "c0", Kind: "producer", Stream: "s1", State: "waiting"}
	assert.Equal(h.t, &State{Streams: map[string]Stream{"s1": s1}, Rows: map[string]Row{"w1": row}, Cards: map[string]Card{"c0": card0, "c1": c1}}, r.After, "a row starts down, a card waiting at gen 0")
	r = h.ok(h.op("move", nil, SetCardState{"c1", "working"}, SetCardAttempt{"c1", 2}, SetCardBranch{"c1", "b1"},
		SetCardHead{"c1", "abc123"}, SetCardPR{"c1", 42}, SetCardReason{"c1", "why"}, SetCardScore{"c1", 3}, SetCardNeeds{"c1", nil},
		SetCardAuthor{"c1", "w1"}, SetCardBaseSHA{"c1", "def456"},
		PutCard{ID: "k1", Kind: "work", Of: "c1", Stream: "s1", Holder: "w1"}))
	want := Card{Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "working", Gen: 1, Attempt: 2, Score: 3, Branch: "b1", Head: "abc123", PR: 42, Brief: "do it", Reason: "why",
		Author: "w1", BaseSHA: "def456"}
	k1 := Card{Priority: "normal", ID: "k1", Kind: "work", Of: "c1", Stream: "s1", State: "waiting", Holder: "w1"}
	assert.Equal(h.t, &State{Cards: map[string]Card{"c1": want, "k1": k1}}, r.After, "every card write raises gen by one; a consumer is added with its holder")
	h.ok(h.op("rows", nil, SetRowState{"w1", "up"}, SetRowSession{"w1", "sess-b"}, SetRowFlaps{"w1", 4}, SetStreamHeld{"s1", true}))
	h.ok(h.op("over", nil, PutRow{"w1", "kin", 1, "sess-c"}, PutStream{"s1", "other", "dev", 5}, SetCardHolder{"k1", ""}, SetCardNeeds{"c1", []string{"c0"}}, SetCardState{"k1", "done"}, SetCardResult{"k1", "ok"}))
	h.ok(h.op("width", []Expect{RowGen{"w1", 2}}, SetRowWidth{"w1", 3}))
	want.Needs, want.Gen = []string{"c0"}, 2
	k1.Holder, k1.State, k1.Result, k1.Gen = "", "done", "ok", 1
	got := h.snap()
	assert.Equal(h.t, Row{Name: "w1", Kind: "kin", Width: 3, State: "up", Session: "sess-c", Flaps: 4, Gen: 3}, got.Rows["w1"], "put row keeps state and flaps; gen rises once per op")
	assert.Equal(h.t, Stream{Name: "s1", Repo: "other", Base: "dev", Held: true, Pos: 5}, got.Streams["s1"], "put stream keeps held")
	assert.Equal(h.t, map[string]Card{"c0": card0, "c1": want, "k1": k1}, got.Cards)
	ev := h.events()
	require.Len(h.t, ev, 5)
	assert.Nil(h.t, ev[0].Before, "new rows have no before-image")
	row.State, row.Session, row.Flaps, row.Gen = "up", "sess-b", 4, 1
	assert.Equal(h.t, &State{Streams: map[string]Stream{"s1": {Name: "s1", Repo: s1.Repo, Base: s1.Base, Held: true, Pos: 1}}, Rows: map[string]Row{"w1": row},
		Cards: map[string]Card{"c1": ev[1].After.Cards["c1"], "k1": ev[1].After.Cards["k1"]}}, ev[3].Before)
}

// strangeWrite and strangeExpect are outside the closed sets: they carry the
// marker method only by embedding.
type strangeWrite struct{ SetCardState }
type strangeExpect struct{ AtSeq }

func writeRefusals(h *harness) {
	h.seed()
	all := "waiting, ready, working, review, merging, landed, fail, blocked, done"
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{PutCard{Kind: "producer", ID: "c1", Stream: "s1"}, "card c1 exists"},
		{PutCard{ID: "u1", Kind: "work", Of: "c2", Stream: "s1"}, "card u1 exists"}, // of never changes: there is no write that sets it
		{PutCard{Kind: "producer", ID: "c3", Stream: "s9"}, "stream s9 does not exist"},
		{PutCard{Kind: "producer", ID: "c3", Stream: "s1", Needs: []string{"c3"}}, "card c3 cannot need itself"},
		{PutCard{Kind: "producer", ID: "c3", Stream: "s1", Needs: []string{"c0", "c0"}}, "card c3 names need c0 twice"},
		{PutCard{Kind: "producer", ID: "c3", Stream: "s1", Needs: []string{"c9"}}, "need c9 of card c3 does not exist"},
		{PutCard{Stream: "s1"}, "a card needs an id"},
		{PutCard{ID: "c3", Kind: "job", Stream: "s1"}, `card kind "job" is not one of producer, sentinel, work, read`},
		{PutCard{ID: "c3", Kind: "producer", Of: "c1", Stream: "s1"}, "card c3 is a producer; a producer has no of"},
		{PutCard{ID: "c3", Kind: "producer", Stream: "s1", Holder: "w1"}, "card c3 is a producer; a producer never has a holder"},
		{PutCard{ID: "k3", Kind: "work", Stream: "s1"}, "card k3 is a consumer; a consumer names its producer in of"},
		{PutCard{ID: "k3", Kind: "read", Of: "c9", Stream: "s1"}, "card k3 is a consumer of c9, which does not exist"},
		{PutCard{ID: "k3", Kind: "read", Of: "u1", Stream: "s1"}, "card k3 is a consumer of u1, which is not a producer"},
		{PutCard{ID: "k3", Kind: "work", Of: "c1", Stream: "s1", Needs: []string{"c0"}}, "card k3 is a consumer; a consumer has no needs"},
		{PutCard{ID: "k3", Kind: "work", Of: "c1", Stream: "s1", Holder: "w9"}, "row w9 does not exist"},
		{SetCardState{"c1", "gone"}, `card state "gone" is not one of ` + all},
		{SetCardHolder{"c1", "w1"}, "card c1 is a producer; a producer never has a holder"},
		{SetCardHolder{"u1", "w9"}, "row w9 does not exist"},
		{SetCardNeeds{"u1", []string{"c0"}}, "card u1 is a consumer; a consumer has no needs"},
		{SetCardResult{"u1", "maybe"}, `card result "maybe" is not one of , ok, fix, fail, fault`},
		{SetRowWidth{"w9", 1}, "row w9 does not exist"},
		{SetCardNeeds{"c1", []string{"c2", "c2"}}, "card c1 names need c2 twice"},
		{SetCardNeeds{"c1", []string{"c1"}}, "card c1 cannot need itself"},
		{SetCardNeeds{"c1", []string{"c9"}}, "need c9 of card c1 does not exist"},
		{SetStreamHeld{"s9", true}, "stream s9 does not exist"},
		{PutStream{}, "a stream needs a name"},
		{PutRow{Name: "w2", Kind: "robot"}, `row kind "robot" is not one of worker, kin`},
		{PutRow{Kind: "kin"}, "a row needs a name"},
		{SetRowState{"w1", "gone"}, `row state "gone" is not one of up, down`},
		{SetRowState{"w9", "down"}, "row w9 does not exist"},
		{SetRowSession{"w9", "s"}, "row w9 does not exist"},
		{SetRowFlaps{"w9", 1}, "row w9 does not exist"},
		{PutPolicy{"yaml", "p", "b"}, `policy kind "yaml" is not one of script, data`},
		{SetActivePolicy{3}, "policy version 3 does not exist"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	for _, w := range []Write{SetCardState{"c9", "ready"}, SetCardHolder{"c9", ""}, SetCardAttempt{"c9", 1}, SetCardBranch{"c9", "b"}, SetCardHead{"c9", "h"},
		SetCardPR{"c9", 1}, SetCardReason{"c9", "r"}, SetCardScore{"c9", 1}, SetCardNeeds{"c9", nil}, SetCardAuthor{"c9", "w1"}, SetCardBaseSHA{"c9", "abc"},
		SetCardResult{"c9", "ok"}} {
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
		"add then score":    h.op("x", nil, PutCard{Kind: "producer", ID: "c3", Stream: "s1"}, SetCardScore{"c3", 2}),
		"stream twice":      h.op("x", nil, PutStream{Name: "s1"}, PutStream{Name: "s1"}),
		"row then session":  h.op("x", nil, PutRow{"w1", "kin", 1, ""}, SetRowSession{"w1", "s"}),
		"activate twice":    h.op("x", nil, SetActivePolicy{1}, SetActivePolicy{2}),
		"add then after":    h.op("x", nil, PutCard{Kind: "producer", ID: "c3", Stream: "s1"}, SetCardAfter{"c3", []string{"c0"}}),
		"held twice":        h.op("x", nil, SetRowHeld{"w1", true}, SetRowHeld{"w1", false}),
		"nul in after":      h.op("x", nil, SetCardAfter{"c1", []string{"c\x000"}}),
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
	h.ok(h.op("fine", nil, SetCardState{"u1", "ready"}, SetCardHolder{"u1", "w1"}))
}

func refusalChangesNothing(h *harness) {
	h.seed()
	before := h.snap()
	o := h.op("partial", []Expect{CardGen{"u1", 0}}, PutStream{Name: "s2"}, SetCardState{"u1", "working"}, SetCardHolder{"u1", "w9"})
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
	again.Writes, again.Expect, again.Actor, again.Kind = []Write{SetCardState{"c1", "fail"}}, []Expect{AtSeq{99}}, "someone", "other"
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
	refused := h.refused(no, `card state "nope" is not one of waiting, ready, working, review, merging, landed, fail, blocked, done`)
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
	h.ok(h.op("busy", nil, SetRowFlaps{"w1", 3}, SetCardHolder{"u1", "w1"}, PutRow{"k1", "kin", 1, "sess-k"}))
	h.ok(h.op("pol", nil, PutPolicy{"data", "p", "{}"}, SetActivePolicy{1}))
	r := h.clear(AtSeq{3})
	carried := map[string]Row{"w1": {Name: "w1", Kind: "worker", Width: 2, State: "down", Session: "sess-a", Gen: 2}, "k1": {Name: "k1", Kind: "kin", Width: 1, State: "down", Session: "sess-k", Gen: 1}}
	assert.Equal(h.t, Receipt{Op: r.Op, Kind: "clear", Actor: "tester", Epoch: 1, At: r.At, Note: r.Note, Accepted: true, Seq: 4, NewEpoch: 2, After: &State{Epoch: 2, Rows: carried}}, r)
	assert.Equal(h.t, Snapshot{Seq: 4, Epoch: 2, Policy: 1, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: carried},
		h.snap(), "rows are carried down with no flaps; streams and cards are not; policy is kept")
	ev := h.events()
	require.Len(h.t, ev, 4)
	assert.Equal(h.t, 1, ev[3].Epoch, "a clear is logged with the epoch it closed")
	assert.Equal(h.t, "clear", ev[3].Kind)
	assert.Equal(h.t, &State{Epoch: 2, Rows: carried}, ev[3].After)
	h.refused(h.op("old", nil, SetCardState{"c1", "fail"}), "card c1 does not exist")
	o := h.op("stale", nil)
	o.Epoch = 1
	h.refused(o, "the op is for epoch 1, the store is at epoch 2")
	h.ok(h.op("again", []Expect{CardAbsent{"c1"}, RowIs{"w1", "down"}}, PutStream{Name: "s1"}, PutCard{Kind: "producer", ID: "c1", Stream: "s1"},
		PutCard{ID: "u1", Kind: "work", Of: "c1", Stream: "s1", Holder: "w1"}))
	assert.Equal(h.t, map[string]Card{"c1": {Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "waiting"}, "u1": {Priority: "normal", ID: "u1", Kind: "work", Of: "c1", Stream: "s1", State: "waiting", Holder: "w1"}}, h.snap().Cards)
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
	assert.Len(h.t, got.Cards, 6)
	ev := h.events()
	require.Len(h.t, ev, 3, "a refused clear is an event")
	assert.False(h.t, ev[2].Accepted)
	assert.Nil(h.t, ev[2].After)
	first := h.op("clear", nil)
	retry := h.op("clear", nil) // a new id planned against the same epoch
	r, err = h.s.Clear(h.ctx, first)
	require.NoError(h.t, err)
	require.True(h.t, r.Accepted)
	r, err = h.s.Clear(h.ctx, retry)
	require.NoError(h.t, err)
	assert.False(h.t, r.Accepted, "a stale clear is refused, not applied twice")
	assert.Equal(h.t, "the op is for epoch 1, the store is at epoch 2", r.Reason)
	assert.Equal(h.t, 2, h.snap().Epoch)
}

// Gen counts the operations that changed a card, not the writes.
func genOncePerOp(h *harness) {
	h.seed()
	r := h.ok(h.op("take", []Expect{CardGen{"u1", 0}}, SetCardState{"u1", "working"}, SetCardHolder{"u1", "w1"}))
	assert.Equal(h.t, 1, r.After.Cards["u1"].Gen, "set state and set holder in one op raise gen by exactly 1")
	assert.Equal(h.t, 1, h.snap().Cards["u1"].Gen)
	r = h.ok(h.op("touch", nil, SetCardReason{"u1", "r"}, SetCardReason{"c2", "r"}))
	assert.Equal(h.t, 2, r.After.Cards["u1"].Gen)
	assert.Equal(h.t, 1, r.After.Cards["c2"].Gen, "each card the op writes moves once")
	r = h.ok(h.op("add", nil, PutCard{ID: "k3", Kind: "read", Of: "c1", Stream: "s1", Holder: "w1"}, SetCardState{"k3", "ready"}))
	assert.Equal(h.t, 0, r.After.Cards["k3"].Gen, "a card the op adds is at gen 0")
	assert.Equal(h.t, 0, h.snap().Cards["c0"].Gen)
	r = h.ok(h.op("rows", []Expect{RowGen{"w1", 0}}, SetRowState{"w1", "down"}, SetRowFlaps{"w1", 1}, SetRowSession{"w1", "s"}))
	assert.Equal(h.t, 1, r.After.Rows["w1"].Gen, "three row writes in one op raise its gen by exactly 1")
	r = h.ok(h.op("add", nil, PutRow{"w2", "kin", 1, ""}, SetRowState{"w2", "up"}))
	assert.Equal(h.t, 0, r.After.Rows["w2"].Gen, "a row the op adds is at gen 0")
}

// Working holders never exceed width: width caps work in progress, not what
// is queued. The count is the consumers a row holds in working, at the moment
// of the write, not counting the card written. A state write that takes a
// held card into working, and a holder write on a card already working, are
// the checks; put row and set row width refuse a width below the working
// count. There is no width condition.
func rowWidth(h *harness) {
	h.seed()
	h.ok(h.op("queue", nil, SetCardState{"u1", "ready"}, SetCardState{"u2", "ready"}, SetCardState{"u3", "ready"},
		SetCardHolder{"u1", "w1"}, SetCardHolder{"u2", "w1"}, SetCardHolder{"u3", "w1"})) // width 2, three ready: queued work takes no room
	h.ok(h.op("take", nil, SetCardState{"u1", "working"}))
	h.ok(h.op("take", nil, SetCardState{"u2", "working"}))
	h.refused(h.op("take", nil, SetCardState{"u3", "working"}), "row w1 holds 2 working cards, its width is 2")
	h.ok(h.op("again", nil, SetCardHolder{"u1", "w1"}, SetCardState{"u1", "working"})) // already working on the row: adds nothing
	h.refused(h.op("narrow", nil, PutRow{"w1", "worker", 1, "sess-a"}), "row w1 holds 2 working cards, more than width 1")
	h.refused(h.op("narrow", nil, SetRowWidth{"w1", 1}), "row w1 holds 2 working cards, more than width 1")
	h.ok(h.op("same", nil, PutRow{"w1", "worker", 2, "sess-a"}))
	h.ok(h.op("wide", nil, SetRowWidth{"w1", 3}))
	h.ok(h.op("take", nil, SetCardState{"u3", "working"}))
	assert.Equal(h.t, 3, h.snap().Rows["w1"].Width)
	h.ok(h.op("new", nil, PutRow{"w2", "kin", 0, ""}))
	h.ok(h.op("queue", nil, PutCard{ID: "k1", Kind: "work", Of: "c1", Stream: "s1", Holder: "w2"})) // width 0 still queues
	h.refused(h.op("take", nil, SetCardState{"k1", "working"}), "row w2 holds 0 working cards, its width is 0")
	h.refused(h.op("move", nil, SetCardHolder{"u3", "w2"}), "row w2 holds 0 working cards, its width is 0")
	h.ok(h.op("open", nil, SetRowWidth{"w2", 1}))
	h.ok(h.op("move", nil, SetCardHolder{"u3", "w2"}))
	h.refused(h.op("take", nil, SetCardState{"k1", "working"}), "row w2 holds 1 working cards, its width is 1")
	h.refused(h.op("move", nil, SetCardHolder{"u3", "w9"}), "row w9 does not exist")
}

// A done or fail consumer keeps its holder for the record and never counts,
// and neither does a ready one: a width-1 row at width still takes a ready
// consumer, and the take that would make it working is refused.
func workingCount(h *harness) {
	h.seed()
	h.ok(h.op("narrow", nil, PutRow{"w1", "worker", 1, "sess-a"}))
	h.ok(h.op("take", nil, SetCardHolder{"u1", "w1"}, SetCardState{"u1", "working"}))
	h.ok(h.op("queue", nil, SetCardHolder{"u2", "w1"}, SetCardState{"u2", "ready"})) // at width; ready takes no room
	h.refused(h.op("take", nil, SetCardState{"u2", "working"}), "row w1 holds 1 working cards, its width is 1")
	h.ok(h.op("report", nil, SetCardState{"u1", "done"}, SetCardResult{"u1", "ok"}))
	h.ok(h.op("take", nil, SetCardState{"u2", "working"}))
	s := h.snap()
	assert.Equal(h.t, "w1", s.Cards["u1"].Holder, "the done consumer keeps its holder")
	assert.Equal(h.t, "ok", s.Cards["u1"].Result)
	h.ok(h.op("queue", nil, SetCardHolder{"u3", "w1"}, SetCardState{"u3", "ready"}))
	h.refused(h.op("take", nil, SetCardState{"u3", "working"}), "row w1 holds 1 working cards, its width is 1")
	h.ok(h.op("fail", nil, SetCardState{"u2", "fail"}))
	h.ok(h.op("take", nil, SetCardState{"u3", "working"}))
	h.ok(h.op("width", nil, SetRowWidth{"w1", 1}))
	h.refused(h.op("narrow", nil, SetRowWidth{"w1", 0}), "row w1 holds 1 working cards, more than width 0")
	h.ok(h.op("back", nil, SetCardState{"u3", "ready"}, SetRowWidth{"w1", 0})) // ready again: the room is back within the op
}

// Stella's probe (l1-validation-20261106/width_review_test.go), moved to
// where the cap bites now: one op with two takes on a width-one row. The
// second counts the first and is refused; nothing lands.
func widthInOneOp(h *harness) {
	h.seed()
	h.ok(h.op("width", nil, PutRow{"w1", "worker", 1, "sess-a"}, SetCardHolder{"u1", "w1"}, SetCardHolder{"u2", "w1"},
		SetCardState{"u1", "ready"}, SetCardState{"u2", "ready"}))
	before := h.snap()
	h.refused(h.op("take", nil, SetCardState{"u1", "working"}, SetCardState{"u2", "working"}), "row w1 holds 1 working cards, its width is 1")
	after := h.snap()
	after.Seq--
	assert.Equal(h.t, before, after, "the first take is undone too")
}

// RowGen fences a row plan: two Gone plans made from one snapshot, one is
// accepted and the other refused.
func rowGenRace(h *harness) {
	h.seed()
	g := h.snap().Rows["w1"].Gen
	gone := func() Op {
		return h.op("gone", []Expect{RowGen{"w1", g}}, SetRowState{"w1", "down"}, SetRowFlaps{"w1", 1})
	}
	a, b := gone(), gone()
	var ra, rb Receipt
	var ea, eb error
	var wg sync.WaitGroup
	wg.Go(func() { ra, ea = h.s.Apply(h.ctx, a) })
	wg.Go(func() { rb, eb = h.s.Apply(h.ctx, b) })
	wg.Wait()
	require.NoError(h.t, ea)
	require.NoError(h.t, eb)
	require.NotEqual(h.t, ra.Accepted, rb.Accepted, "exactly one Gone is accepted")
	refused := cmpRefused(ra, rb)
	assert.Equal(h.t, fmt.Sprintf("row w1 is at gen %d, expected gen %d", g+1, g), refused.Reason)
	assert.Equal(h.t, 1, h.snap().Rows["w1"].Flaps, "the flap is counted once")
}

func cmpRefused(a, b Receipt) Receipt {
	if a.Accepted {
		return b
	}
	return a
}

// Every integer a write carries fits a PostgreSQL integer; outside it the op
// is ErrInvalid from both stores (Stella's probe score_range_test.go: Fake
// accepted 2^31 and PG reported ErrUnknown).
func intRange(h *harness) {
	h.seed()
	n := len(h.events())
	for name, c := range map[string]struct {
		w      Write
		reason string
	}{
		"score over":       {PutCard{Kind: "producer", ID: "c3", Stream: "s1", Score: math.MaxInt32 + 1}, "score 2147483648 is outside the range 0..2147483647"},
		"score negative":   {SetCardScore{"c1", -1}, "score -1 is outside the range 0..2147483647"},
		"width over":       {PutRow{"w2", "kin", math.MaxInt32 + 1, ""}, "width 2147483648 is outside the range 0..2147483647"},
		"width negative":   {PutRow{"w2", "kin", -1, ""}, "width -1 is outside the range 0..2147483647"},
		"width overwrite":  {PutRow{"w1", "worker", -1, "sess-a"}, "width -1 is outside"},
		"attempt":          {SetCardAttempt{"c1", math.MaxInt32 + 1}, "attempt 2147483648 is outside"},
		"attempt negative": {SetCardAttempt{"c1", -1}, "attempt -1 is outside"},
		"pr":               {SetCardPR{"c1", -1}, "pr -1 is outside"},
		"flaps":            {SetRowFlaps{"w1", math.MaxInt32 + 1}, "flaps 2147483648 is outside"},
		"flaps negative":   {SetRowFlaps{"w1", -1}, "flaps -1 is outside"},
		"pos":              {PutStream{"s2", "", "", -1}, "pos -1 is outside"},
		"policy version":   {SetActivePolicy{-1}, "version -1 is outside"},
	} {
		_, err := h.s.Apply(h.ctx, h.op("big", nil, c.w))
		require.ErrorIs(h.t, err, ErrInvalid, name)
		assert.Contains(h.t, err.Error(), c.reason, name)
	}
	o := h.op("big", nil)
	o.Epoch = math.MaxInt32 + 1
	_, err := h.s.Apply(h.ctx, o)
	require.ErrorIs(h.t, err, ErrInvalid)
	assert.Contains(h.t, err.Error(), "epoch 2147483648 is outside")
	assert.Len(h.t, h.events(), n, "nothing out of range is logged")
	h.ok(h.op("edge", nil, SetCardScore{"c1", math.MaxInt32}, PutRow{"w2", "kin", math.MaxInt32, ""}, SetCardAttempt{"c1", 0}))
	assert.Equal(h.t, math.MaxInt32, h.snap().Cards["c1"].Score)
}

// A NUL byte in any text is ErrInvalid before the transaction, from both
// stores: PostgreSQL text cannot hold one (Emma's read, 2026-11-06: it
// surfaced as ErrUnknown).
func nulBytes(h *harness) {
	h.seed()
	n := len(h.events())
	for name, o := range map[string]Op{
		"id":         {ID: "x\x00", Epoch: 1},
		"actor":      {ID: "x", Epoch: 1, At: t0, Actor: "a\x00"},
		"note":       {ID: "x", Epoch: 1, At: t0, Note: "\x00"},
		"card id":    h.op("x", nil, PutCard{Kind: "producer", ID: "c\x00", Stream: "s1"}),
		"need":       h.op("x", nil, SetCardNeeds{"c1", []string{"c0\x00"}}),
		"brief":      h.op("x", nil, PutCard{Kind: "producer", ID: "c3", Stream: "s1", Brief: "b\x00"}),
		"reason":     h.op("x", nil, SetCardReason{"c1", "r\x00"}),
		"head":       h.op("x", nil, SetCardHead{"c1", "\x00"}),
		"branch":     h.op("x", nil, SetCardBranch{"c1", "b\x00"}),
		"author":     h.op("x", nil, SetCardAuthor{"c1", "w\x00"}),
		"row name":   h.op("x", nil, PutRow{"w\x00", "kin", 1, ""}),
		"stream":     h.op("x", nil, PutStream{"s\x00", "", "", 0}),
		"policy":     h.op("x", nil, PutPolicy{"data", "p", "{\x00}"}),
		"condition":  h.op("x", []Expect{CardState{"c1", "ready\x00"}}),
		"row in cnd": h.op("x", []Expect{RowIs{"w\x00", "up"}}),
	} {
		_, err := h.s.Apply(h.ctx, o)
		require.ErrorIs(h.t, err, ErrInvalid, name)
		assert.Contains(h.t, err.Error(), "NUL byte", name)
	}
	o := h.op("clear", nil)
	o.Note = "\x00"
	_, err := h.s.Clear(h.ctx, o)
	assert.ErrorIs(h.t, err, ErrInvalid, "a clear too")
	assert.Len(h.t, h.events(), n, "nothing with a NUL is logged")
}

// Text must be valid UTF-8 and a time must fall in the years 0..9999, or the
// op is ErrInvalid from both stores (the cold read: Fake kept raw bytes its
// JSON image rewrote, so Rebuild diverged; a far At panicked inside encode).
func textAndTime(h *harness) {
	h.seed()
	n := len(h.events())
	bad := map[string]Op{
		"utf8 reason": h.op("x", nil, SetCardReason{"c1", "\xff\xfe"}),
		"utf8 id":     {ID: "\xff", Epoch: 1, At: t0},
		"utf8 kind":   {ID: "y", Epoch: 1, Kind: "\xff", At: t0},
		"zero at":     {ID: "z", Epoch: 1},
	}
	for name, at := range map[string]time.Time{"year 10000": time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "year -1": time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)} {
		o := h.op("far", nil, SetCardState{"c1", "ready"})
		o.At = at
		bad[name] = o
	}
	for name, o := range bad {
		_, err := h.s.Apply(h.ctx, o)
		require.ErrorIs(h.t, err, ErrInvalid, name)
	}
	assert.Len(h.t, h.events(), n, "nothing invalid is logged")
	o := h.op("edge", nil, SetCardReason{"c1", "naïve ✓"})
	o.At = time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC)
	h.ok(o)
	assert.Equal(h.t, Rebuild(h.events()), h.snap())
	assert.Equal(h.t, "waiting", h.snap().Cards["c1"].State)
}

// A known id returns its stored receipt whatever its body says, invalid
// included: the lookup comes before the body is checked; only a missing id
// is refused first (the cold read's TestColdKnownIDInvalidBody).
func knownIDInvalidBody(h *harness) {
	h.seed()
	o := h.op("take", nil, SetCardState{"c1", "ready"})
	first := h.ok(o)
	for name, mutate := range map[string]func(*Op){
		"epoch 0":          func(o *Op) { o.Epoch = 0 },
		"two state writes": func(o *Op) { o.Writes = []Write{SetCardState{"c1", "a"}, SetCardState{"c1", "b"}} },
		"nul reason":       func(o *Op) { o.Writes = []Write{SetCardReason{"c1", "\x00"}} },
		"zero at":          func(o *Op) { o.At = time.Time{} },
		"nil condition":    func(o *Op) { o.Expect = []Expect{nil} },
	} {
		again := o
		mutate(&again)
		r, err := h.s.Apply(h.ctx, again)
		require.NoError(h.t, err, name)
		assert.Equal(h.t, first, r, name)
	}
	_, err := h.s.Apply(h.ctx, Op{Epoch: 1, At: t0})
	assert.ErrorIs(h.t, err, ErrInvalid, "a missing id")
	assert.Len(h.t, h.events(), 2)
}

// A snapshot is the caller's own copy (Stella's probe
// snapshot_isolation_test.go: editing a Fake snapshot's needs changed the
// store without an event, and Rebuild then diverged).
func snapshotIsCopy(h *harness) {
	h.seed()
	s := h.snap()
	require.Equal(h.t, []string{"c0"}, s.Cards["c1"].Needs)
	s.Cards["c1"].Needs[0] = "changed-by-caller"
	s.Rows["w1"] = Row{}
	again := h.snap()
	assert.Equal(h.t, []string{"c0"}, again.Cards["c1"].Needs)
	assert.Equal(h.t, w1, again.Rows["w1"])
	assert.Equal(h.t, Rebuild(h.events()), again)
	r := h.ok(h.op("touch", nil, SetCardReason{"c1", "r"}))
	r.After.Cards["c1"].Needs[0] = "changed-by-caller"
	assert.Equal(h.t, []string{"c0"}, h.snap().Cards["c1"].Needs, "a receipt is a copy too")
	ev := h.events()
	ev[1].After.Cards["c1"].Needs[0] = "changed-by-caller"
	assert.Equal(h.t, []string{"c0"}, h.events()[1].After.Cards["c1"].Needs, "an event image is a copy too")
}

// Stella's probe (concurrency_review_test.go): sixteen concurrent applies of
// one op id give one receipt and one event.
func sameIDConcurrent(h *harness) {
	h.ok(h.op("stream", nil, PutStream{"s", "repo", "main", 0}))
	o := h.op("card", nil, PutCard{Kind: "producer", ID: "c", Stream: "s"})
	const n = 16
	receipts, errs := make([]Receipt, n), make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { <-start; receipts[i], errs[i] = h.s.Apply(h.ctx, o) })
	}
	close(start)
	wg.Wait()
	for i := range n {
		require.NoError(h.t, errs[i])
		assert.True(h.t, receipts[i].Accepted)
		assert.Equal(h.t, receipts[0], receipts[i])
	}
	assert.Len(h.t, h.events(), 2)
	assert.Equal(h.t, Rebuild(h.events()), h.snap())
}

// Stella's probe (concurrency_review_test.go): a clear racing an apply
// planned against the old epoch follows serial order: the apply lands only
// when it is logged before the clear.
func clearRacesApply(h *harness) {
	h.ok(h.op("stream", nil, PutStream{"s", "repo", "main", 0}))
	apply, clear := h.op("card", nil, PutCard{Kind: "producer", ID: "c", Stream: "s"}), h.op("clear", nil)
	var a, c Receipt
	var ae, ce error
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() { <-start; a, ae = h.s.Apply(h.ctx, apply) })
	wg.Go(func() { <-start; c, ce = h.s.Clear(h.ctx, clear) })
	close(start)
	wg.Wait()
	require.NoError(h.t, ae)
	require.NoError(h.t, ce)
	require.True(h.t, c.Accepted)
	assert.Equal(h.t, 2, c.NewEpoch)
	assert.Equal(h.t, a.Seq < c.Seq, a.Accepted, "the apply lands only before the clear")
	s := h.snap()
	assert.Equal(h.t, 2, s.Epoch)
	assert.Empty(h.t, s.Cards)
	assert.Equal(h.t, Rebuild(h.events()), s)
}

// Schema 2: author and base_sha are plain card writes. Each is accepted once,
// raises gen once per op, is refused on a missing card, and is in the
// snapshot, the event images and the rebuild.
func authorAndBase(h *harness) {
	h.seed()
	r := h.ok(h.op("deal", []Expect{CardGen{"c1", 0}}, SetCardBaseSHA{"c1", "b0"}))
	assert.Equal(h.t, Card{Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "waiting", Gen: 1, Score: 10, Needs: []string{"c0"}, Brief: "do it", BaseSHA: "b0"}, r.After.Cards["c1"])
	r = h.ok(h.op("finish", []Expect{CardGen{"c1", 1}}, SetCardAuthor{"c1", "w1"}))
	assert.Equal(h.t, 2, r.After.Cards["c1"].Gen)
	r = h.ok(h.op("both", nil, SetCardAuthor{"c1", "w2"}, SetCardBaseSHA{"c1", "b1"}))
	assert.Equal(h.t, 3, r.After.Cards["c1"].Gen, "both in one op raise gen once")
	h.refused(h.op("bad", nil, SetCardAuthor{"c9", "w1"}), "card c9 does not exist")
	h.refused(h.op("bad", nil, SetCardBaseSHA{"c9", "b"}), "card c9 does not exist")
	_, err := h.s.Apply(h.ctx, h.op("x", nil, SetCardAuthor{"c1", "a"}, SetCardAuthor{"c1", "b"}))
	assert.ErrorIs(h.t, err, ErrInvalid, "two author writes in one op")
	got := h.snap()
	assert.Equal(h.t, "w2", got.Cards["c1"].Author)
	assert.Equal(h.t, "b1", got.Cards["c1"].BaseSHA)
	ev := h.events()
	assert.Equal(h.t, "w1", ev[3].Before.Cards["c1"].Author, "the before-image carries the author")
	assert.Equal(h.t, "b0", ev[3].Before.Cards["c1"].BaseSHA, "the before-image carries the base")
	assert.Equal(h.t, Rebuild(ev), got)
}

func rebuild(h *harness) {
	h.seed()
	h.ok(h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"}, SetCardHolder{"u1", "w1"}, SetCardState{"u1", "working"}, SetCardResult{"u2", "fix"}))
	h.refused(h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"}), "card c1 is at gen 1, expected gen 0")
	h.ok(h.op("pol", nil, PutPolicy{"script", "p1", "x"}, PutPolicy{"data", "p2", "y"}, SetActivePolicy{2}, SetRowWidth{"w1", 4}, SetRowHeld{"w1", true},
		SetCardAfter{"c2", []string{"c0"}}, PutCard{Kind: "sentinel", ID: "g1", Stream: "s1", Needs: []string{"c2"}}, SetCardState{"c0", "blocked"}))
	assert.Equal(h.t, Rebuild(h.events()), h.snap())
	h.clear()
	h.ok(h.op("seed2", nil, PutStream{Name: "s1"}, PutCard{Kind: "producer", ID: "c1", Stream: "s1"}, SetActivePolicy{1}, SetRowSession{"w1", "sess-z"}))
	h.ok(h.op("rows", nil, SetRowState{"w1", "up"}, SetCardReason{"c1", "r"}))
	got := h.snap()
	assert.Equal(h.t, Rebuild(h.events()), got, "the clear's after-image carries the rows")
	assert.Equal(h.t, 1, got.Policy)
	assert.Equal(h.t, 2, got.Epoch)
	assert.Equal(h.t, "sess-z", got.Rows["w1"].Session)
	assert.Equal(h.t, 4, got.Rows["w1"].Gen, "row gen survives the clear and the rebuild")
	assert.True(h.t, got.Rows["w1"].Held, "the hold survives the clear and the rebuild")
}

// A sentinel is a producer with no brief: it has needs and after, and never
// an of, a holder or a consumer.
func sentinels(h *harness) {
	h.seed()
	h.ok(h.op("gate", nil, PutCard{Kind: "sentinel", ID: "g1", Stream: "s1", Needs: []string{"c1", "c2"}, After: []string{"c0"}}))
	assert.Equal(h.t, Card{Priority: "normal", ID: "g1", Kind: "sentinel", Stream: "s1", State: "waiting", Needs: []string{"c1", "c2"}, After: []string{"c0"}}, h.snap().Cards["g1"])
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{PutCard{Kind: "sentinel", ID: "g2", Stream: "s1", Brief: "x"}, "card g2 is a sentinel; a sentinel has no brief"},
		{PutCard{Kind: "sentinel", ID: "g2", Of: "c1", Stream: "s1"}, "card g2 is a sentinel; a sentinel has no of"},
		{PutCard{Kind: "sentinel", ID: "g2", Stream: "s1", Holder: "w1"}, "card g2 is a sentinel; a sentinel never has a holder"},
		{SetCardHolder{"g1", "w1"}, "card g1 is a sentinel; a sentinel never has a holder"},
		{PutCard{Kind: "read", ID: "k1", Of: "g1", Stream: "s1"}, "card k1 is a consumer of g1, which is not a producer"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	h.ok(h.op("reached", []Expect{CardGen{"g1", 0}}, SetCardState{"g1", "landed"}))
	assert.Equal(h.t, 1, h.snap().Cards["g1"].Gen)
}

// After is soft order, under the same rules as needs.
func afterOrder(h *harness) {
	h.seed()
	h.ok(h.op("add", nil, PutCard{Kind: "producer", ID: "c3", Stream: "s1", After: []string{"c1", "c2"}}, SetCardAfter{"c1", []string{"c0"}}))
	s := h.snap()
	assert.Equal(h.t, []string{"c1", "c2"}, s.Cards["c3"].After)
	assert.Equal(h.t, []string{"c0"}, s.Cards["c1"].After)
	assert.Equal(h.t, 1, s.Cards["c1"].Gen, "set after raises the gen")
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{SetCardAfter{"c1", []string{"c1"}}, "card c1 cannot come after itself"},
		{SetCardAfter{"c1", []string{"c0", "c0"}}, "card c1 names after c0 twice"},
		{SetCardAfter{"c1", []string{"c9"}}, "after c9 of card c1 does not exist"},
		{SetCardAfter{"c9", []string{"c0"}}, "card c9 does not exist"},
		{SetCardAfter{"u1", []string{"c0"}}, "card u1 is a consumer; a consumer has no after"},
		{PutCard{Kind: "work", ID: "k1", Of: "c1", Stream: "s1", After: []string{"c0"}}, "card k1 is a consumer; a consumer has no after"},
		{PutCard{Kind: "producer", ID: "c4", Stream: "s1", After: []string{"c4"}}, "card c4 cannot come after itself"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	h.ok(h.op("unorder", nil, SetCardAfter{"c3", nil}))
	assert.Nil(h.t, h.snap().Cards["c3"].After)
}

// blocked is in the set: the store admits it and leaves its moves to layer 2.
func blockedState(h *harness) {
	h.seed()
	h.ok(h.op("block", nil, SetCardState{"c1", "blocked"}))
	assert.Equal(h.t, "blocked", h.snap().Cards["c1"].State)
	h.ok(h.op("unblock", []Expect{CardState{"c1", "blocked"}}, SetCardState{"c1", "waiting"}))
	h.refused(h.op("bad", []Expect{CardState{"c1", "blocked"}}, SetCardState{"c1", "ready"}), "card c1 is waiting, expected blocked")
}

// held is the coordinator's hold on a row: its own write, kept by put row,
// carried by a clear, and it raises the row's gen.
func rowHeld(h *harness) {
	h.seed()
	h.ok(h.op("hold", []Expect{RowGen{"w1", 0}}, SetRowHeld{"w1", true}))
	h.ok(h.op("put", nil, PutRow{"w1", "worker", 3, "sess-b"}, PutRow{"w2", "kin", 1, ""}))
	s := h.snap()
	assert.Equal(h.t, Row{Name: "w1", Kind: "worker", Width: 3, State: "up", Session: "sess-b", Held: true, Gen: 2}, s.Rows["w1"], "put row keeps held")
	assert.False(h.t, s.Rows["w2"].Held, "a new row is not held")
	h.refused(h.op("stale", []Expect{RowGen{"w1", 0}}, SetRowHeld{"w1", false}), "row w1 is at gen 2, expected gen 0")
	h.refused(h.op("bad", nil, SetRowHeld{"w9", true}), "row w9 does not exist")
	h.clear()
	got := h.snap()
	assert.True(h.t, got.Rows["w1"].Held, "a clear carries the hold")
	assert.Equal(h.t, Rebuild(h.events()), got)
	h.ok(h.op("deal", []Expect{RowHeld{"w1", true}}, SetRowState{"w1", "up"}))
	h.refused(h.op("deal", []Expect{RowHeld{"w1", false}}, SetRowState{"w1", "down"}), "row w1 is held, expected not held")
	h.refused(h.op("deal", []Expect{RowHeld{"w9", false}}), "row w9 does not exist")
	h.ok(h.op("unhold", []Expect{RowGen{"w1", 4}}, SetRowHeld{"w1", false}))
	h.ok(h.op("deal", []Expect{RowHeld{"w1", false}}))
	h.refused(h.op("deal", []Expect{RowHeld{"w1", true}}), "row w1 is not held, expected held")
	assert.False(h.t, h.snap().Rows["w1"].Held)
}

// The columns the lessons of v1 placed: prior_head (a work consumer's
// continued head), bounces (a producer's count toward blocked), cost and
// priced (a consumer's run, one field), priority (a producer's or sentinel's
// level; a consumer's is always normal) and the result fault.
func lessonColumns(h *harness) {
	h.seed()
	h.ok(h.op("add", nil, PutCard{Kind: "producer", ID: "c3", Stream: "s1", Priority: "blocker"}, PutCard{Kind: "sentinel", ID: "g1", Stream: "s1", Priority: "low"},
		PutCard{Kind: "read", ID: "k1", Of: "c3", Stream: "s1", Priority: "normal"}))
	s := h.snap()
	assert.Equal(h.t, "blocker", s.Cards["c3"].Priority)
	assert.Equal(h.t, "low", s.Cards["g1"].Priority)
	assert.Equal(h.t, "normal", s.Cards["k1"].Priority, "a consumer's priority is normal, named or not")
	assert.Equal(h.t, "normal", s.Cards["c1"].Priority, "the default is normal")
	r := h.ok(h.op("report", []Expect{CardGen{"u1", 0}}, SetCardPriorHead{"u1", "abc"}, SetCardCost{"u1", math.MaxInt32 + 1, true}, SetCardResult{"u1", "fault"},
		SetCardBounces{"c1", 2}, SetCardPriority{"c1", "critical"}, SetCardCost{"u2", 0, false}))
	u1want := Card{Priority: "normal", ID: "u1", Kind: "work", Of: "c1", Stream: "s1", State: "waiting", Gen: 1, Prior: "abc", Cost: math.MaxInt32 + 1, Priced: true, Result: "fault"}
	assert.Equal(h.t, u1want, r.After.Cards["u1"], "cost is a bigint: it goes past the integer range")
	c1want := c1
	c1want.Gen, c1want.Bounces, c1want.Priority = 1, 2, "critical"
	assert.Equal(h.t, c1want, r.After.Cards["c1"])
	assert.False(h.t, r.After.Cards["u2"].Priced, "an unpriced run is unpriced, never $0")
	h.ok(h.op("level", nil, SetCardPriority{"g1", "high"}))
	r = h.ok(h.op("cut", nil, SetCardAttempt{"c1", 3}, PutCard{Kind: "work", ID: "k3", Of: "c1", Stream: "s1", Attempt: 3}))
	assert.Equal(h.t, 3, r.After.Cards["k3"].Attempt, "a consumer carries the attempt it belongs to, set at add")
	assert.Equal(h.t, 3, r.After.Cards["c1"].Attempt)
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{SetCardPriority{"c1", "urgent"}, `card priority "urgent" is not one of low, normal, high, critical, blocker`},
		{SetCardPriority{"c1", "read"}, `card priority "read" is not one of low, normal, high, critical, blocker`}, // read is a rung, not a level
		{SetCardPriority{"u1", "high"}, "card u1 is a consumer; a consumer has no priority of its own"},
		{PutCard{Kind: "work", ID: "k2", Of: "c1", Stream: "s1", Priority: "high"}, "card k2 is a consumer; a consumer has no priority of its own"},
		{PutCard{Kind: "producer", ID: "c4", Stream: "s1", Priority: "top"}, `card priority "top" is not one of low, normal, high, critical, blocker`},
		{SetCardResult{"u1", "faulty"}, `card result "faulty" is not one of , ok, fix, fail, fault`},
		{SetCardPriorHead{"u9", "x"}, "card u9 does not exist"},
		{SetCardBounces{"c9", 1}, "card c9 does not exist"},
		{SetCardCost{"u9", 1, true}, "card u9 does not exist"},
		// the kind-specific columns: the store enforces whose they are
		{SetCardPriorHead{"u2", "x"}, "card u2 is of kind read; SetCardPriorHead is only for a work consumer"},
		{SetCardPriorHead{"c1", "x"}, "card c1 is of kind producer; SetCardPriorHead is only for a work consumer"},
		{SetCardBounces{"u1", 1}, "card u1 is of kind work; SetCardBounces is only for a producer"},
		{SetCardBounces{"g1", 1}, "card g1 is of kind sentinel; SetCardBounces is only for a producer"},
		{SetCardCost{"c1", 1, true}, "card c1 is of kind producer; SetCardCost is only for a consumer"},
		{SetCardResult{"c1", "ok"}, "card c1 is of kind producer; SetCardResult is only for a consumer"},
		{SetCardResult{"g1", ""}, "card g1 is of kind sentinel; SetCardResult is only for a consumer"},
		{SetCardAttempt{"u1", 2}, "card u1 is of kind work; SetCardAttempt is only for a producer or sentinel (a consumer's attempt is set at add)"},
		{PutCard{Kind: "producer", ID: "c4", Stream: "s1", Attempt: 1}, "card c4 is a producer; its attempt starts at 0 and is raised by SetCardAttempt"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	for name, o := range map[string]Op{
		"negative cost":    h.op("x", nil, SetCardCost{"u1", -1, true}),
		"negative bounces": h.op("x", nil, SetCardBounces{"c1", -1}),
		"bounces too big":  h.op("x", nil, SetCardBounces{"c1", math.MaxInt32 + 1}),
		"cost twice":       h.op("x", nil, SetCardCost{"u1", 1, true}, SetCardCost{"u1", 2, true}),
		"priority twice":   h.op("x", nil, PutCard{Kind: "producer", ID: "c5", Stream: "s1"}, SetCardPriority{"c5", "high"}),
		"nul prior head":   h.op("x", nil, SetCardPriorHead{"u1", "a\x00"}),
	} {
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
	}
	assert.Equal(h.t, Rebuild(h.events()), h.snap(), "images carry every new column")
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
	_, err = h.s.Policy(h.ctx, 1<<40)
	assert.ErrorIs(h.t, err, ErrNotFound, "a version past int32 is not found on both (the cold read)")
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
