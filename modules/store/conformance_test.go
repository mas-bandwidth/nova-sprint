package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
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
		"clear releases holds":    clearReleasesHolds,
		"rebuild":                 rebuild,
		"events":                  eventsPaging,
		"policy":                  policy,
		"times":                   times,
		"gen once per op":         genOncePerOp,
		"no width in the store":   noWidthInStore,
		"author and base sha":     authorAndBase,
		"working count":           workingCount,
		"row gen race":            rowGenRace,
		"row load":                rowLoad,
		"sentinels":               sentinels,
		"after":                   afterOrder,
		"no blocked state":        noBlockedState,
		"row held":                rowHeld,
		"hook":                    hook,
		"stop":                    stop,
		"identity":                identity,
		"stream settings":         streamSettings,
		"lesson columns":          lessonColumns,
		"review and row reason":   reviewAndRowReason,
		"finding":                 finding,
		"model":                   model,
		"red":                     red,
		"usage":                   usageColumn,
		"int range":               intRange,
		"nul bytes":               nulBytes,
		"text and time":           textAndTime,
		"known id, invalid body":  knownIDInvalidBody,
		"snapshot is a copy":      snapshotIsCopy,
		"same id concurrent":      sameIDConcurrent,
		"clear races apply":       clearRacesApply,
		"name lengths":            nameLengths,
		"check order":             checkOrder,
		"judgements":              judgements,
		"cursors":                 cursors,
		"tiers":                   tiers,
		"card contract":           cardContract,
		"event policy":            eventPolicy,
		"cold read 3":             coldRead3,
		"cold read 4":             coldRead4,
		"schema identity":         schemaIdentity,
		"fake from snapshot":      fakeFromSnapshot,
		"scoped reads":            scopedReads,
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
	s1 = Stream{Name: "s1", Repo: "mas-bandwidth/x", Base: "main", Pos: 1, Mode: "auto"}
	w1 = Row{Name: "w1", State: "up", Session: "sess-a", Role: "worker"}
	c0 = Card{Tier: "pro", Priority: "normal", ID: "c0", Kind: "producer", Stream: "s1", State: "landed"}
	c1 = Card{Tier: "pro", Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "waiting", Score: 10, Needs: []string{"c0"}, Brief: "do it"}
	c2 = Card{Tier: "pro", Priority: "normal", ID: "c2", Kind: "producer", Stream: "s1", State: "waiting", Needs: []string{"c1"}}
	u1 = Card{Tier: "pro", Priority: "normal", ID: "u1", Kind: "work", Of: "c1", Stream: "s1", State: "waiting", Holder: "w1"}
	u2 = Card{Tier: "pro", Priority: "normal", ID: "u2", Kind: "read", Of: "c1", Stream: "s1", State: "waiting", Holder: "w1"}
	u3 = Card{Tier: "pro", Priority: "normal", ID: "u3", Kind: "work", Of: "c2", Stream: "s1", State: "waiting", Holder: "w1"}
)

func (h *harness) seed() {
	h.t.Helper()
	h.ok(h.op("seed", nil, PutStream{"s1", s1.Repo, s1.Base, s1.Pos, "", ""}, PutRow{Name: "w1", Session: "sess-a", Role: "worker"}, SetRowState{"w1", "up"},
		PutCard{Tier: "pro", Kind: "producer", ID: "c0", Stream: "s1"}, SetCardState{"c0", "landed"},
		PutCard{Tier: "pro", Kind: "producer", ID: "c1", Stream: "s1", Score: 10, Needs: []string{"c0"}, Brief: "do it"}, PutCard{Tier: "pro", Kind: "producer", ID: "c2", Stream: "s1", Needs: []string{"c1"}},
		PutCard{Tier: "pro", ID: "u1", Kind: "work", Of: "c1", Stream: "s1", Holder: "w1"}, PutCard{Tier: "pro", ID: "u2", Kind: "read", Of: "c1", Stream: "s1", Holder: "w1"}, PutCard{Tier: "pro", ID: "u3", Kind: "work", Of: "c2", Stream: "s1", Holder: "w1"}))
}

func (h *harness) seeded() Snapshot {
	return Snapshot{Epoch: 1, Streams: map[string]Stream{"s1": s1}, Rows: map[string]Row{"w1": w1}, Cards: map[string]Card{"c0": c0, "c1": c1, "c2": c2, "u1": u1, "u2": u2, "u3": u3}, Judgements: map[string]Judgement{}, Cursors: map[string]int64{},
		Stopped: true, StopReason: NeverStarted}
}

func conditions(h *harness) {
	h.seed()
	h.ok(h.op("take", nil, SetCardReason{"c1", "r"}, SetRowFlaps{"w1", 1}))
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
		{CardHolder{"u2", "w2"}, "card u2 has holder w1, expected w2"},
		{CardHolder{"c1", ""}, ""},
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
	h.refused(o, "the op is for epoch 2, the store is at epoch 1: take a new snapshot")
	assert.Equal(h.t, "waiting", h.snap().Cards["c1"].State)
	assert.Equal(h.t, int64(4), h.snap().Seq, "refusals are events")
}

func writes(h *harness) {
	r := h.ok(h.op("add", nil, PutStream{"s1", s1.Repo, s1.Base, s1.Pos, "", ""}, PutRow{Name: "w1", Session: "sess-a", Role: "worker"}, PutCard{Tier: "pro", Kind: "producer", ID: "c0", Stream: "s1"},
		PutCard{Tier: "pro", Kind: "producer", ID: "c1", Stream: "s1", Score: 10, Needs: []string{"c0"}, Brief: "do it"}))
	row := Row{Name: "w1", State: "down", Session: "sess-a", Role: "worker"}
	card0 := Card{Tier: "pro", Priority: "normal", ID: "c0", Kind: "producer", Stream: "s1", State: "waiting"}
	assert.Equal(h.t, &State{Streams: map[string]Stream{"s1": s1}, Rows: map[string]Row{"w1": row}, Cards: map[string]Card{"c0": card0, "c1": c1}}, r.After, "a row starts down, a card waiting at gen 0")
	r = h.ok(h.op("move", nil, SetCardState{"c1", "working"}, SetCardAttempt{"c1", 2}, SetCardBranch{"c1", "b1"},
		SetCardHead{"c1", "abc123"}, SetCardPR{"c1", 42}, SetCardReason{"c1", "why"}, SetCardScore{"c1", 3}, SetCardNeeds{"c1", nil},
		SetCardAuthor{"c1", "w1"}, SetCardBaseSHA{"c1", "def456"},
		PutCard{Tier: "pro", ID: "k1", Kind: "work", Of: "c1", Stream: "s1", Holder: "w1"}))
	want := Card{Tier: "pro", Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "working", Gen: 1, Attempt: 2, Score: 3, Branch: "b1", Head: "abc123", PR: 42, Brief: "do it", Reason: "why",
		Author: "w1", BaseSHA: "def456"}
	k1 := Card{Tier: "pro", Priority: "normal", ID: "k1", Kind: "work", Of: "c1", Stream: "s1", State: "waiting", Holder: "w1"}
	assert.Equal(h.t, &State{Cards: map[string]Card{"c1": want, "k1": k1}}, r.After, "every card write raises gen by one; a consumer is added with its holder")
	h.ok(h.op("rows", nil, SetRowState{"w1", "up"}, SetRowFlaps{"w1", 4}, SetStreamHeld{"s1", true}))
	h.ok(h.op("over", nil, PutRow{Name: "w1", Session: "sess-a", Role: "coordinator"}, PutStream{"s1", "other", "dev", 5, "", ""}, SetCardNeeds{"c1", []string{"c0"}}, SetCardState{"k1", "done"}, SetCardResult{"k1", "ok"}))
	h.ok(h.op("flaps", []Expect{RowGen{"w1", 2}}, SetRowFlaps{"w1", 5}))
	want.Needs, want.Gen = []string{"c0"}, 2
	k1.State, k1.Result, k1.Gen = "done", "ok", 1
	got := h.snap()
	assert.Equal(h.t, Row{Name: "w1", State: "up", Session: "sess-a", Role: "coordinator", Flaps: 5, Gen: 3}, got.Rows["w1"], "put row keeps state and flaps, and the same session may change its role; gen rises once per op")
	assert.Equal(h.t, Stream{Name: "s1", Repo: "other", Base: "dev", Held: true, Pos: 5, Mode: "auto"}, got.Streams["s1"], "put stream keeps held")
	assert.Equal(h.t, map[string]Card{"c0": card0, "c1": want, "k1": k1}, got.Cards)
	ev := h.events()
	require.Len(h.t, ev, 5)
	assert.Nil(h.t, ev[0].Before, "new rows have no before-image")
	row.State, row.Session, row.Flaps, row.Gen = "up", "sess-a", 4, 1
	assert.Equal(h.t, &State{Streams: map[string]Stream{"s1": {Name: "s1", Repo: s1.Repo, Base: s1.Base, Held: true, Pos: 1, Mode: "auto"}}, Rows: map[string]Row{"w1": row},
		Cards: map[string]Card{"c1": ev[1].After.Cards["c1"], "k1": ev[1].After.Cards["k1"]}}, ev[3].Before)
}

// strangeWrite and strangeExpect are outside the closed sets: they carry the
// marker method only by embedding.
type strangeWrite struct{ SetCardState }
type strangeExpect struct{ AtSeq }

func writeRefusals(h *harness) {
	h.seed()
	all := "waiting, ready, working, review, merging, landed, fail, done"
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{PutCard{Tier: "pro", Kind: "producer", ID: "c1", Stream: "s1"}, "card c1 exists"},
		{PutCard{Tier: "pro", ID: "u1", Kind: "work", Of: "c2", Stream: "s1", Holder: "w1"}, "card u1 exists"}, // of never changes: there is no write that sets it
		{PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s9"}, "stream s9 does not exist"},
		{PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s1", Needs: []string{"c3"}}, "card c3 cannot need itself"},
		{PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s1", Needs: []string{"c0", "c0"}}, "card c3 names need c0 twice"},
		{PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s1", Needs: []string{"c9"}}, "need c9 of card c3 does not exist"},
		{PutCard{Tier: "pro", ID: "c3", Kind: "producer", Of: "c1", Stream: "s1"}, "card c3 is a producer; a producer has no of"},
		{PutCard{Tier: "pro", ID: "c3", Kind: "producer", Stream: "s1", Holder: "w1"}, "card c3 is a producer; a producer never has a holder"},
		{PutCard{Tier: "pro", ID: "k3", Kind: "work", Stream: "s1"}, "card k3 is a consumer; a consumer names its producer in of"},
		{PutCard{Tier: "pro", ID: "k3", Kind: "read", Of: "c9", Stream: "s1", Holder: "w1"}, "card k3 is a consumer of c9, which does not exist"},
		{PutCard{Tier: "pro", ID: "k3", Kind: "read", Of: "u1", Stream: "s1", Holder: "w1"}, "card k3 is a consumer of u1, which is not a producer"},
		{PutCard{Tier: "pro", ID: "k3", Kind: "work", Of: "c1", Stream: "s1", Holder: "w1", Needs: []string{"c0"}}, "card k3 is a consumer; a consumer has no needs"},
		{PutCard{Tier: "pro", ID: "k3", Kind: "work", Of: "c1", Stream: "s1", Holder: "w9"}, "row w9 does not exist"},
		{PutCard{Tier: "pro", ID: "k3", Kind: "work", Of: "c1", Stream: "s1"}, "card k3 is a consumer; a consumer names its holder row at add, and it never changes"},
		{SetCardNeeds{"u1", []string{"c0"}}, "card u1 is of kind work; SetCardNeeds is only for a producer or sentinel"},
		{SetCardNeeds{"c1", []string{"c2", "c2"}}, "card c1 names need c2 twice"},
		{SetCardNeeds{"c1", []string{"c1"}}, "card c1 cannot need itself"},
		{SetCardNeeds{"c1", []string{"c9"}}, "need c9 of card c1 does not exist"},
		{SetStreamHeld{"s9", true}, "stream s9 does not exist"},
		{SetRowState{"w9", "down"}, "row w9 does not exist"},
		{SetRowFlaps{"w9", 1}, "row w9 does not exist"},
		{SetActivePolicy{3}, "policy version 3 does not exist"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	for _, w := range []Write{SetCardState{"c9", "ready"}, SetCardAttempt{"c9", 1}, SetCardBranch{"c9", "b"}, SetCardHead{"c9", "h"},
		SetCardPR{"c9", 1}, SetCardReason{"c9", "r"}, SetCardScore{"c9", 1}, SetCardNeeds{"c9", nil}, SetCardAuthor{"c9", "w1"}, SetCardBaseSHA{"c9", "abc"},
		SetCardResult{"c9", "ok"}} {
		h.refused(h.op("bad", nil, w), "card c9 does not exist")
	}
	h.invalidAll([]refusal{
		{PutCard{Tier: "pro", Stream: "s1"}, "a card needs an id"},
		{PutCard{Tier: "pro", ID: "c3", Kind: "job", Stream: "s1"}, `card kind "job" is not one of producer, sentinel, work, read`},
		{SetCardState{"c1", "gone"}, `card state "gone" is not one of ` + all},
		{SetCardResult{"u1", "maybe"}, `card result "maybe" is not one of , ok, fix, fail, fault`},
		{PutStream{}, "a stream needs a name"},
		{PutRow{}, "a row needs a name"},
		{SetRowState{"w1", "gone"}, `row state "gone" is not one of up, down`},
		{PutPolicy{"yaml", "p", "b"}, `policy kind "yaml" is not one of script, data`},
	})
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
		"add then score":    h.op("x", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s1"}, SetCardScore{"c3", 2}),
		"stream twice":      h.op("x", nil, PutStream{Name: "s1"}, PutStream{Name: "s1"}),
		"session no role":   h.op("x", nil, PutRow{Name: "w1", Session: "s"}),
		"role no session":   h.op("x", nil, PutRow{Name: "w1", Role: "worker"}),
		"unknown role":      h.op("x", nil, PutRow{Name: "w1", Session: "s", Role: "boss"}),
		"activate twice":    h.op("x", nil, SetActivePolicy{1}, SetActivePolicy{2}),
		"add then after":    h.op("x", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s1"}, SetCardAfter{"c3", []string{"c0"}}),
		"held twice":        h.op("x", nil, SetRowHeld{"w1", true, ""}, SetRowHeld{"w1", false, ""}),
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
	h.ok(h.op("fine", nil, SetCardState{"u1", "ready"}))
}

func refusalChangesNothing(h *harness) {
	h.seed()
	before := h.snap()
	o := h.op("partial", []Expect{CardGen{"u1", 0}}, PutStream{Name: "s2"}, SetCardState{"u1", "working"}, PutCard{Tier: "pro", ID: "k9", Kind: "work", Of: "c1", Stream: "s1", Holder: "w9"})
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
	no := h.op("bad", nil, SetCardState{"c9", "ready"})
	refused := h.refused(no, "card c9 does not exist")
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
	h.ok(h.op("busy", nil, SetRowFlaps{"w1", 3}, PutRow{Name: "k1", Session: "sess-k", Role: "worker"}))
	h.ok(h.op("pol", nil, PutPolicy{"data", "p", "{}"}, SetActivePolicy{1}))
	r := h.clear(AtSeq{3})
	carried := map[string]Row{"w1": {Name: "w1", State: "down", Session: "sess-a", Role: "worker", Gen: 2}, "k1": {Name: "k1", State: "down", Session: "sess-k", Role: "worker", Gen: 1}}
	assert.Equal(h.t, Receipt{Op: r.Op, Kind: "clear", Actor: "tester", Epoch: 1, At: r.At, Note: r.Note, Accepted: true, Seq: 4, NewEpoch: 2, After: &State{Epoch: 2, Rows: carried}}, r)
	assert.Equal(h.t, Snapshot{Seq: 4, Epoch: 2, Policy: 1, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: carried, Judgements: map[string]Judgement{}, Cursors: map[string]int64{}, Stopped: true, StopReason: NeverStarted},
		h.snap(), "rows are carried down with no flaps; streams and cards are not; policy is kept")
	ev := h.events()
	require.Len(h.t, ev, 4)
	assert.Equal(h.t, 1, ev[3].Epoch, "a clear is logged with the epoch it closed")
	assert.Equal(h.t, "clear", ev[3].Kind)
	assert.Equal(h.t, &State{Epoch: 2, Rows: carried}, ev[3].After)
	h.refused(h.op("old", nil, SetCardState{"c1", "fail"}), "card c1 does not exist")
	o := h.op("stale", nil)
	o.Epoch = 1
	h.refused(o, "the op is for epoch 1, the store is at epoch 2: take a new snapshot")
	h.ok(h.op("again", []Expect{CardAbsent{"c1"}, RowIs{"w1", "down"}}, PutStream{Name: "s1"}, PutCard{Tier: "pro", Kind: "producer", ID: "c1", Stream: "s1"},
		PutCard{Tier: "pro", ID: "u1", Kind: "work", Of: "c1", Stream: "s1", Holder: "w1"}))
	assert.Equal(h.t, map[string]Card{"c1": {Tier: "pro", Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "waiting"}, "u1": {Tier: "pro", Priority: "normal", ID: "u1", Kind: "work", Of: "c1", Stream: "s1", State: "waiting", Holder: "w1"}}, h.snap().Cards)
	assert.Equal(h.t, 2, h.events()[6].Epoch)
}

// A hold is the coordinator's decision for one epoch: a clear opens the next
// epoch with every row unheld and its reason empty, the coordinator's own row
// too (2026-11-08: emma, johnny and stella came up in epoch 2 still held
// with epoch 1's reason, and sat idle). A deal fenced on the row not being
// held lands in the new epoch.
func clearReleasesHolds(h *harness) {
	h.seed()
	h.ok(h.op("hook", nil, PutRow{Name: "k1", Session: "sess-k", Role: "coordinator"}))
	h.ok(h.op("hold", nil, SetRowHeld{"w1", true, "dogfood of v2.0.13, released within the hour"}, SetRowHeld{"k1", true, "coordinating"}))
	r := h.clear()
	for _, name := range []string{"w1", "k1"} {
		row := h.snap().Rows[name]
		assert.Equal(h.t, []any{false, ""}, []any{row.Held, row.Reason}, "row %s starts epoch 2 unheld with no reason", name)
		assert.Equal(h.t, []any{false, ""}, []any{r.After.Rows[name].Held, r.After.Rows[name].Reason}, "the clear's after-image of %s is unheld", name)
	}
	assert.Equal(h.t, Rebuild(h.events()), h.snap(), "the rebuild agrees")
	h.ok(h.op("deal", []Expect{RowHeld{"w1", false}}, PutStream{Name: "s1"}, PutCard{Tier: "pro", Kind: "producer", ID: "c1", Stream: "s1"},
		PutCard{Tier: "pro", ID: "u1", Kind: "work", Of: "c1", Stream: "s1", Holder: "w1"}))
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
	assert.Equal(h.t, "the op is for epoch 5, the store is at epoch 1: take a new snapshot", r.Reason)
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
	assert.Equal(h.t, "the op is for epoch 1, the store is at epoch 2: take a new snapshot", r.Reason)
	assert.Equal(h.t, 2, h.snap().Epoch)
}

// Gen counts the operations that changed a card, not the writes.
func genOncePerOp(h *harness) {
	h.seed()
	r := h.ok(h.op("take", []Expect{CardGen{"u1", 0}}, SetCardState{"u1", "working"}, SetCardBranch{"u1", "b"}))
	assert.Equal(h.t, 1, r.After.Cards["u1"].Gen, "set state and set branch in one op raise gen by exactly 1")
	assert.Equal(h.t, 1, h.snap().Cards["u1"].Gen)
	r = h.ok(h.op("touch", nil, SetCardReason{"u1", "r"}, SetCardReason{"c2", "r"}))
	assert.Equal(h.t, 2, r.After.Cards["u1"].Gen)
	assert.Equal(h.t, 1, r.After.Cards["c2"].Gen, "each card the op writes moves once")
	r = h.ok(h.op("add", nil, PutCard{Tier: "pro", ID: "k3", Kind: "read", Of: "c1", Stream: "s1", Holder: "w1"}, SetCardState{"k3", "ready"}))
	assert.Equal(h.t, 0, r.After.Cards["k3"].Gen, "a card the op adds is at gen 0")
	assert.Equal(h.t, 0, h.snap().Cards["c0"].Gen)
	r = h.ok(h.op("rows", []Expect{RowGen{"w1", 0}}, SetRowState{"w1", "down"}, SetRowFlaps{"w1", 1}, SetRowHeld{"w1", true, ""}))
	assert.Equal(h.t, 1, r.After.Rows["w1"].Gen, "three row writes in one op raise its gen by exactly 1")
	r = h.ok(h.op("add", nil, PutRow{Name: "w2", Session: ""}, SetRowState{"w2", "up"}))
	assert.Equal(h.t, 0, r.After.Rows["w2"].Gen, "a row the op adds is at gen 0")
}

// hook: PutRow is the sprint hook's one write (round 22). A row is hooked
// by one session at a time: another session's hook is refused, naming the
// fix; the same session may hook again, with a new role; unhook (both "")
// frees it, and the state, flaps and hold are kept throughout.
func hook(h *harness) {
	h.seed()
	h.refused(h.op("steal", nil, PutRow{Name: "w1", Session: "sess-x", Role: "coordinator"}), "row w1 is hooked by session sess-a; unhook it first")
	h.ok(h.op("role", nil, PutRow{Name: "w1", Session: "sess-a", Role: "coordinator"}))
	h.ok(h.op("unhook", nil, PutRow{Name: "w1"}))
	assert.Equal(h.t, Row{Name: "w1", State: "up", Gen: 2}, h.snap().Rows["w1"], "unhooked: no session, no role, state kept")
	h.ok(h.op("hook", nil, PutRow{Name: "w1", Session: "sess-x", Role: "coordinator"}))
	assert.Equal(h.t, Row{Name: "w1", State: "up", Session: "sess-x", Role: "coordinator", Gen: 3}, h.snap().Rows["w1"])
	h.ok(h.op("new", nil, PutRow{Name: "w2", Session: "sess-y", Role: "worker"}))
	assert.Equal(h.t, Row{Name: "w2", State: "down", Session: "sess-y", Role: "worker"}, h.snap().Rows["w2"], "a hook puts a new row down")
}

// stop: the sprint's stop is one value for the store, set by SetStopped,
// refused when unchanged, kept by a clear, and in the log's images so a
// rebuild has it. A fresh store is stopped, never started, until its first
// start. The store refuses nothing on it (ops still land) except an op
// fenced on Running, which run's tick ops are.
func stop(h *harness) {
	assert.Equal(h.t, []any{true, NeverStarted}, []any{h.snap().Stopped, h.snap().StopReason}, "a fresh store is stopped, never started")
	h.seed()
	h.refused(h.op("fresh-tick", []Expect{Running{}}, SetCardReason{"c1", "a tick op"}), "the sprint is stopped (never started): nova-sprint start")
	h.refused(h.op("fresh-stop", nil, SetStopped{true, "again"}), "the sprint is already stopped")
	h.ok(h.op("first-start", nil, SetStopped{false, "go"}))
	r := h.ok(h.op("stop", nil, SetStopped{true, "glenn: pause"}))
	assert.Equal(h.t, &State{Stop: &Stop{true, "glenn: pause"}}, r.After)
	assert.Equal(h.t, []any{true, "glenn: pause"}, []any{h.snap().Stopped, h.snap().StopReason})
	h.refused(h.op("stop", nil, SetStopped{true, "again"}), "the sprint is already stopped")
	h.ok(h.op("people", nil, SetCardReason{"c1", "still lands"}))
	h.refused(h.op("tick", []Expect{Running{}}, SetCardReason{"c1", "a tick op"}), "the sprint is stopped (glenn: pause): nova-sprint start")
	h.clear()
	assert.True(h.t, h.snap().Stopped, "a clear keeps the stop")
	ev := h.events()
	assert.Equal(h.t, h.snap(), Rebuild(ev))
	h.ok(h.op("start", nil, SetStopped{false, "go"}))
	h.refused(h.op("start", nil, SetStopped{false, ""}), "the sprint is already running")
	h.ok(h.op("tick2", []Expect{Running{}}, SetCursor{Name: "reader", Seq: 1}))
	got := h.snap()
	assert.Equal(h.t, []any{false, "go"}, []any{got.Stopped, got.StopReason})
	assert.Equal(h.t, got, Rebuild(h.events()))
	_, err := h.s.Apply(h.ctx, h.op("twice", nil, SetStopped{true, "a"}, SetStopped{false, "b"}))
	assert.ErrorIs(h.t, err, ErrInvalid, "one stop write per op")
}

// identity (round 24): the git identity the sprint commits as is one value
// for the store, not set on a fresh store, set by SetIdentity with both
// parts, refused when unchanged, kept by a clear, and in the log's images so
// a rebuild has it.
func identity(h *harness) {
	assert.False(h.t, h.snap().Identity.Set(), "a fresh store has no identity")
	h.seed()
	_, err := h.s.Apply(h.ctx, h.op("no email", nil, SetIdentity{Name: "Bot"}))
	assert.ErrorIs(h.t, err, ErrInvalid, "an identity needs both parts")
	r := h.ok(h.op("set", nil, SetIdentity{"Bot", "1+bot@users.noreply.example"}))
	assert.Equal(h.t, &State{Identity: &Identity{"Bot", "1+bot@users.noreply.example"}}, r.After)
	h.refused(h.op("again", nil, SetIdentity{"Bot", "1+bot@users.noreply.example"}), "the sprint already commits as Bot <1+bot@users.noreply.example>")
	h.ok(h.op("change", nil, SetIdentity{"Bot", "2+bot@users.noreply.example"}))
	h.clear()
	assert.Equal(h.t, Identity{"Bot", "2+bot@users.noreply.example"}, h.snap().Identity, "a clear keeps the identity")
	assert.Equal(h.t, h.snap(), Rebuild(h.events()))
	_, err = h.s.Apply(h.ctx, h.op("twice", nil, SetIdentity{"a", "a@x"}, SetIdentity{"b", "b@x"}))
	assert.ErrorIs(h.t, err, ErrInvalid, "one identity write per op")
}

// Round 22: a stream's landing settings are stream state. A stream starts
// in mode auto with no forge; put stream takes both; set mode and set forge
// change one each, refused when unchanged or (mode) out of the closed set;
// the images carry them, and an image logged before round 22 reads as auto.
func streamSettings(h *harness) {
	h.seed()
	assert.Equal(h.t, Stream{Name: "s1", Repo: s1.Repo, Base: s1.Base, Pos: 1, Mode: "auto"}, h.snap().Streams["s1"], "auto, no forge, by default")
	r := h.ok(h.op("s2", nil, PutStream{"s2", "r", "main", 2, "group", "github"}))
	assert.Equal(h.t, Stream{Name: "s2", Repo: "r", Base: "main", Pos: 2, Mode: "group", Forge: "github"}, r.After.Streams["s2"])
	h.ok(h.op("hand", nil, SetStreamMode{"s1", "hand"}, SetStreamForge{"s1", "gitlab"}))
	h.refused(h.op("hand", nil, SetStreamMode{"s1", "hand"}), `stream s1's mode is already "hand"`)
	h.refused(h.op("forge", nil, SetStreamForge{"s1", "gitlab"}), `stream s1's forge is already "gitlab"`)
	h.refused(h.op("none", nil, SetStreamMode{"s9", "auto"}), "stream s9 does not exist")
	h.ok(h.op("unforge", nil, SetStreamForge{"s1", ""}, SetStreamMode{"s2", "auto"}))
	got := h.snap()
	assert.Equal(h.t, []string{"hand", "", "auto", "github"}, []string{got.Streams["s1"].Mode, got.Streams["s1"].Forge, got.Streams["s2"].Mode, got.Streams["s2"].Forge})
	assert.Equal(h.t, got, Rebuild(h.events()), "the images carry mode and forge")
	h.invalid(map[string]Op{
		"unknown mode":   h.op("x", nil, SetStreamMode{"s1", "fast"}),
		"empty mode":     h.op("x", nil, SetStreamMode{"s1", ""}),
		"put bad mode":   h.op("x", nil, PutStream{"s3", "r", "main", 3, "fast", ""}),
		"mode twice":     h.op("x", nil, SetStreamMode{"s1", "auto"}, PutStream{"s1", "r", "main", 1, "group", ""}),
		"forge twice":    h.op("x", nil, SetStreamForge{"s1", "a"}, SetStreamForge{"s1", "b"}),
		"forge not text": h.op("x", nil, SetStreamForge{"s1", "a\x00b"}),
	})
	var old Stream
	require.NoError(h.t, json.Unmarshal([]byte(`{"name":"s1","repo":"r","base":"main","held":false,"pos":1}`), &old))
	assert.Equal(h.t, Stream{Name: "s1", Repo: "r", Base: "main", Pos: 1, Mode: "auto"}, old, "an image from before round 22 reads as auto")
}

// Round 22: a row's width and tiers are configuration, the roster's, read
// from nova-config each tick; the store never holds them, so it takes any
// number of working consumers of any tier on a row that exists. The deal
// guards (card package) refuse a deal past the roster's width or outside its
// tiers; this is the store's half: nothing here caps or serves.
func noWidthInStore(h *harness) {
	h.seed()
	h.ok(h.op("queue", nil, SetCardState{"u1", "ready"}, SetCardState{"u2", "ready"}, SetCardState{"u3", "ready"}))
	h.ok(h.op("take all in one op", nil, SetCardState{"u1", "working"}, SetCardState{"u2", "working"}, SetCardState{"u3", "working"}))
	h.refused(h.op("again", nil, SetCardState{"u1", "working"}), "card u1 is already working") // nothing moves without a change
	h.ok(h.op("put row keeps them", nil, PutRow{Name: "w1", Session: "sess-a", Role: "worker"}))
	h.ok(h.op("heavy producer", nil, PutCard{Tier: "heavy", Kind: "producer", ID: "ph", Stream: "s1"}))
	h.ok(h.op("any tier on any row", nil, PutCard{Tier: "heavy", ID: "k1", Kind: "work", Of: "ph", Stream: "s1", Holder: "w1"}, SetCardState{"k1", "working"}))
	h.refused(h.op("no row", nil, PutCard{Tier: "heavy", ID: "k2", Kind: "work", Of: "ph", Stream: "s1", Holder: "w9"}), "row w9 does not exist")
	s := h.snap()
	for _, id := range []string{"u1", "u2", "u3", "k1"} {
		assert.Equal(h.t, "working", s.Cards[id].State, id)
	}
	assert.Equal(h.t, Row{Name: "w1", State: "up", Session: "sess-a", Role: "worker", Gen: 1}, s.Rows["w1"], "a row is sprint state only")
}

// A done or fail consumer keeps its holder for the record.
func workingCount(h *harness) {
	h.seed()
	h.ok(h.op("take", nil, SetCardState{"u1", "working"}))
	h.ok(h.op("report", nil, SetCardState{"u1", "done"}, SetCardResult{"u1", "ok"}))
	h.ok(h.op("take", nil, SetCardState{"u2", "working"}))
	h.ok(h.op("fail", nil, SetCardState{"u2", "fail"}))
	s := h.snap()
	assert.Equal(h.t, "w1", s.Cards["u1"].Holder, "the done consumer keeps its holder")
	assert.Equal(h.t, "w1", s.Cards["u2"].Holder, "the failed consumer keeps its holder")
	assert.Equal(h.t, "ok", s.Cards["u1"].Result)
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

// A worker's heartbeat (SetRowLoad) sets the row's load columns and nothing
// else, moves no gen, is refused when it changes nothing, and is carried by
// the log and by PutRow.
func rowLoad(h *harness) {
	h.seed()
	g := h.snap().Rows["w1"].Gen
	r := h.ok(h.op("beat", []Expect{RowGen{"w1", g}}, SetRowLoad{"w1", 250, 8, 2, 8, "", 0, ""}))
	row := r.After.Rows["w1"]
	assert.Equal(h.t, []int{250, 8, 2, 8, g}, []int{row.Load1Centi, row.Cores, row.SlotsUsed, row.SlotsTotal, row.Gen}, "the beat is stored and the gen did not move")
	h.refused(h.op("same", nil, SetRowLoad{"w1", 250, 8, 2, 8, "", 0, ""}), "row w1 already has the given load")
	h.ok(h.op("next", []Expect{RowGen{"w1", g}}, SetRowLoad{"w1", 300, 8, 3, 8, "", 0, ""}))
	h.ok(h.op("advertise", nil, SetRowLoad{"w1", 300, 8, 3, 8, "flash,pro", 873, "v2.0.6"}))
	assert.Equal(h.t, "flash,pro", h.snap().Rows["w1"].Tiers, "the beat carries the advertised tiers (defect 74)")
	h.ok(h.op("hook again", nil, PutRow{Name: "w1", Session: "sess-a", Role: "worker"}))
	assert.Equal(h.t, []any{300, "flash,pro", 873, "v2.0.6"}, []any{h.snap().Rows["w1"].Load1Centi, h.snap().Rows["w1"].Tiers, h.snap().Rows["w1"].CPUPeakDeci, h.snap().Rows["w1"].Version}, "PutRow keeps the beat")
	h.ok(h.op("unhook", nil, PutRow{Name: "w1"}))
	assert.Equal(h.t, []any{300, "", ""}, []any{h.snap().Rows["w1"].Load1Centi, h.snap().Rows["w1"].Tiers, h.snap().Rows["w1"].Version}, "an unhook keeps the load and clears the advertisement and the version")
	h.ok(h.op("hook", nil, PutRow{Name: "w1", Session: "sess-a", Role: "worker"}))
	h.ok(h.op("with a state", nil, SetRowState{"w1", "down"}, SetRowLoad{"w1", 100, 8, 0, 8, "", 0, ""}))
	assert.Equal(h.t, g+4, h.snap().Rows["w1"].Gen, "an op that also writes the row still moves its gen once (the hooks above moved it three times)")
	h.refusedAll([]refusal{
		{SetRowLoad{"w9", 100, 8, 0, 8, "", 0, ""}, "row w9 does not exist"},
	})
	h.invalidAll([]refusal{
		{SetRowLoad{"w1", -1, 8, 0, 8, "", 0, ""}, "load1_centi -1 is outside"},
		{SetRowLoad{"w1", 100, 0, 0, 8, "", 0, ""}, "cores of at least 1"},
		{SetRowLoad{"w1", 100, 8, 9, 8, "", 0, ""}, "slots_used no more than slots_total"},
		{SetRowLoad{"w1", 100, 8, math.MaxInt32 + 1, 8, "", 0, ""}, "slots_used 2147483648 is outside"},
		{SetRowLoad{"w1", 100, 8, 0, 8, "pro,flash", 0, ""}, "not a comma list of flash, pro, heavy, frontier in that order"},
		{SetRowLoad{"w1", 100, 8, 0, 8, "flash,turbo", 0, ""}, "not a comma list"},
		{SetRowLoad{"w1", 100, 8, 0, 8, "", 1001, ""}, "cpu_peak_deci of at most 1000"},
		{SetRowLoad{"w1", 100, 8, 0, 8, "", -1, ""}, "cpu_peak_deci -1 is outside"},
	})
	assert.Equal(h.t, Rebuild(h.events()), h.snap(), "images carry the beat")
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
		"score over":       {PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s1", Score: math.MaxInt32 + 1}, "score 2147483648 is outside the range 0..2147483647"},
		"score negative":   {SetCardScore{"c1", -1}, "score -1 is outside the range 0..2147483647"},
		"attempt":          {SetCardAttempt{"c1", math.MaxInt32 + 1}, "attempt 2147483648 is outside"},
		"attempt negative": {SetCardAttempt{"c1", -1}, "attempt -1 is outside"},
		"pr":               {SetCardPR{"c1", -1}, "pr -1 is outside"},
		"flaps":            {SetRowFlaps{"w1", math.MaxInt32 + 1}, "flaps 2147483648 is outside"},
		"flaps negative":   {SetRowFlaps{"w1", -1}, "flaps -1 is outside"},
		"pos":              {PutStream{"s2", "", "", -1, "", ""}, "pos -1 is outside"},
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
	h.ok(h.op("edge", nil, SetCardScore{"c1", math.MaxInt32}, PutRow{Name: "w2", Session: ""}, SetCardAttempt{"c1", math.MaxInt32}))
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
		"card id":    h.op("x", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c\x00", Stream: "s1"}),
		"need":       h.op("x", nil, SetCardNeeds{"c1", []string{"c0\x00"}}),
		"brief":      h.op("x", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s1", Brief: "b\x00"}),
		"reason":     h.op("x", nil, SetCardReason{"c1", "r\x00"}),
		"head":       h.op("x", nil, SetCardHead{"c1", "\x00"}),
		"branch":     h.op("x", nil, SetCardBranch{"c1", "b\x00"}),
		"author":     h.op("x", nil, SetCardAuthor{"c1", "w\x00"}),
		"row name":   h.op("x", nil, PutRow{Name: "w\x00", Session: ""}),
		"stream":     h.op("x", nil, PutStream{"s\x00", "", "", 0, "", ""}),
		"policy":     h.op("x", nil, PutPolicy{"data", "p", "{\x00}"}),
		"condition":  h.op("x", []Expect{CardState{"c\x00", "ready"}}),
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
	h.ok(h.op("stream", nil, PutStream{"s", "repo", "main", 0, "", ""}))
	o := h.op("card", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c", Stream: "s"})
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
	h.ok(h.op("stream", nil, PutStream{"s", "repo", "main", 0, "", ""}))
	apply, clear := h.op("card", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c", Stream: "s"}), h.op("clear", nil)
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
	assert.Equal(h.t, Card{Tier: "pro", Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "waiting", Gen: 1, Score: 10, Needs: []string{"c0"}, Brief: "do it", BaseSHA: "b0"}, r.After.Cards["c1"])
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
	h.ok(h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"}, SetCardState{"u1", "working"}, SetCardResult{"u2", "fix"}))
	h.refused(h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"}), "card c1 is at gen 1, expected gen 0")
	h.ok(h.op("pol", nil, PutPolicy{"script", "p1", "x"}, PutPolicy{"data", "p2", "y"}, SetActivePolicy{2}, SetRowFlaps{"w1", 4}, SetRowHeld{"w1", true, "epoch 1's reason"},
		SetCardAfter{"c2", []string{"c0"}}, PutCard{Tier: "pro", Kind: "sentinel", ID: "g1", Stream: "s1", Needs: []string{"c2"}}, SetCardState{"c0", "fail"}))
	assert.Equal(h.t, Rebuild(h.events()), h.snap())
	h.clear()
	h.ok(h.op("seed2", nil, PutStream{Name: "s1"}, PutCard{Tier: "pro", Kind: "producer", ID: "c1", Stream: "s1"}, SetActivePolicy{1}, PutRow{Name: "w1"}))
	h.ok(h.op("rows", nil, SetRowState{"w1", "up"}, SetCardReason{"c1", "r"}))
	got := h.snap()
	assert.Equal(h.t, Rebuild(h.events()), got, "the clear's after-image carries the rows")
	assert.Equal(h.t, 1, got.Policy)
	assert.Equal(h.t, 2, got.Epoch)
	assert.Equal(h.t, []string{"", ""}, []string{got.Rows["w1"].Session, got.Rows["w1"].Role}, "unhooked after the clear")
	assert.Equal(h.t, 4, got.Rows["w1"].Gen, "row gen survives the clear and the rebuild")
	assert.Equal(h.t, []any{false, ""}, []any{got.Rows["w1"].Held, got.Rows["w1"].Reason}, "a hold is epoch-scoped: the clear releases it and its reason, and the rebuild agrees")
}

// A sentinel is a producer with no brief: it has needs and after, and never
// an of, a holder or a consumer.
func sentinels(h *harness) {
	h.seed()
	h.ok(h.op("gate", nil, PutCard{Tier: "pro", Kind: "sentinel", ID: "g1", Stream: "s1", Needs: []string{"c1", "c2"}, After: []string{"c0"}}))
	assert.Equal(h.t, Card{Tier: "pro", Priority: "normal", ID: "g1", Kind: "sentinel", Stream: "s1", State: "waiting", Needs: []string{"c1", "c2"}, After: []string{"c0"}}, h.snap().Cards["g1"])
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{PutCard{Tier: "pro", Kind: "sentinel", ID: "g2", Stream: "s1", Brief: "x"}, "card g2 is a sentinel; a sentinel has no brief"},
		{PutCard{Tier: "pro", Kind: "sentinel", ID: "g2", Of: "c1", Stream: "s1"}, "card g2 is a sentinel; a sentinel has no of"},
		{PutCard{Tier: "pro", Kind: "sentinel", ID: "g2", Stream: "s1", Holder: "w1"}, "card g2 is a sentinel; a sentinel never has a holder"},
		{PutCard{Tier: "pro", Kind: "read", ID: "k1", Of: "g1", Stream: "s1", Holder: "w1"}, "card k1 is a consumer of g1, which is not a producer"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	h.ok(h.op("reached", []Expect{CardGen{"g1", 0}}, SetCardState{"g1", "landed"}))
	assert.Equal(h.t, 1, h.snap().Cards["g1"].Gen)
}

// After is soft order, under the same rules as needs.
func afterOrder(h *harness) {
	h.seed()
	h.ok(h.op("add", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s1", After: []string{"c1", "c2"}}, SetCardAfter{"c1", []string{"c0"}}))
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
		{SetCardAfter{"u1", []string{"c0"}}, "card u1 is of kind work; SetCardAfter is only for a producer or sentinel"},
		{PutCard{Tier: "pro", Kind: "work", ID: "k1", Of: "c1", Stream: "s1", Holder: "w1", After: []string{"c0"}}, "card k1 is a consumer; a consumer has no after"},
		{PutCard{Tier: "pro", Kind: "producer", ID: "c4", Stream: "s1", After: []string{"c4"}}, "card c4 cannot come after itself"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	h.ok(h.op("unorder", nil, SetCardAfter{"c3", nil}))
	assert.Nil(h.t, h.snap().Cards["c3"].After)
}

// blocked is no card state (Glenn, 2026-11-07: "waiting is where stuff goes
// where it's having problems"): the store refuses it as a write and as a
// condition, and a held card is waiting with its reason (round 29).
func noBlockedState(h *harness) {
	h.seed()
	h.invalidAll([]refusal{{SetCardState{"c1", "blocked"}, `card state "blocked" is not one of`}})
	h.invalid(map[string]Op{"blocked condition": h.op("x", []Expect{CardState{"c1", "blocked"}})})
	h.ok(h.op("hold", nil, SetCardReason{"c1", "held: looking at it"}))
	assert.Equal(h.t, []string{"waiting", "held: looking at it"}, []string{h.snap().Cards["c1"].State, h.snap().Cards["c1"].Reason}, "a held card is waiting with its reason")
}

// held is the coordinator's hold on a row: its own write, kept by put row,
// released by a clear (a hold is for one epoch), and it raises the row's gen.
func rowHeld(h *harness) {
	h.seed()
	h.ok(h.op("hold", []Expect{RowGen{"w1", 0}}, SetRowHeld{"w1", true, ""}))
	h.ok(h.op("put", nil, PutRow{Name: "w1", Session: "sess-a", Role: "worker"}, PutRow{Name: "w2", Session: ""}))
	s := h.snap()
	assert.Equal(h.t, Row{Name: "w1", State: "up", Session: "sess-a", Role: "worker", Held: true, Gen: 2}, s.Rows["w1"], "put row keeps held")
	assert.False(h.t, s.Rows["w2"].Held, "a new row is not held")
	h.refused(h.op("stale", []Expect{RowGen{"w1", 0}}, SetRowHeld{"w1", false, ""}), "row w1 is at gen 2, expected gen 0")
	h.refused(h.op("bad", nil, SetRowHeld{"w9", true, ""}), "row w9 does not exist")
	h.clear()
	got := h.snap()
	assert.False(h.t, got.Rows["w1"].Held, "a clear releases the hold")
	assert.Equal(h.t, Rebuild(h.events()), got)
	h.ok(h.op("hold", []Expect{RowGen{"w1", 3}, RowHeld{"w1", false}}, SetRowHeld{"w1", true, ""}))
	h.ok(h.op("deal", []Expect{RowHeld{"w1", true}}, SetRowState{"w1", "up"}))
	h.refused(h.op("deal", []Expect{RowHeld{"w1", false}}, SetRowState{"w1", "down"}), "row w1 is held, expected not held")
	h.refused(h.op("deal", []Expect{RowHeld{"w9", false}}), "row w9 does not exist")
	h.ok(h.op("unhold", []Expect{RowGen{"w1", 5}}, SetRowHeld{"w1", false, ""}))
	h.ok(h.op("deal", []Expect{RowHeld{"w1", false}}))
	h.refused(h.op("deal", []Expect{RowHeld{"w1", true}}), "row w1 is not held, expected held")
	assert.False(h.t, h.snap().Rows["w1"].Held)
}

// usage (round 25): a consumer's usage record, words the store keeps and
// never reads; refused on a producer or sentinel, "already has the given
// usage" when unchanged, and kept by rebuild.
func usageColumn(h *harness) {
	h.seed()
	words := "input=10 cache_read=2 output=5 model=deepseek/deepseek-flash billing=api charged=0.01"
	r := h.ok(h.op("report", nil, SetCardUsage{"u1", words}, SetCardCost{"u1", 7, true}))
	assert.Equal(h.t, words, r.After.Cards["u1"].Usage)
	assert.Equal(h.t, "", r.After.Cards["c1"].Usage)
	h.refusedAll([]refusal{
		{SetCardUsage{"c1", "x=1"}, "card c1 is of kind producer; SetCardUsage is only for a consumer"},
		{SetCardUsage{"u9", "x=1"}, "card u9 does not exist"},
		{SetCardUsage{"u1", words}, "card u1 already has the given usage"},
	})
	h.invalid(map[string]Op{
		"nul usage":   h.op("x", nil, SetCardUsage{"u1", "a\x00"}),
		"usage twice": h.op("x", nil, SetCardUsage{"u1", "a=1"}, SetCardUsage{"u1", "a=2"}),
	})
	assert.Equal(h.t, h.snap().Cards, Rebuild(h.events()).Cards, "the log carries the usage")
}

// The columns the lessons of v1 placed: prior_head (a work consumer's
// continued head), bounces (a producer's count toward the bounce hold), cost and
// priced (a consumer's run, one field), priority (a producer's or sentinel's
// level; a consumer's is always normal) and the result fault.
func lessonColumns(h *harness) {
	h.seed()
	h.ok(h.op("add", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c3", Stream: "s1", Priority: "blocker"}, PutCard{Tier: "pro", Kind: "sentinel", ID: "g1", Stream: "s1", Priority: "low"},
		PutCard{Tier: "pro", Kind: "read", ID: "k1", Of: "c3", Stream: "s1", Holder: "w1", Priority: "normal"}))
	s := h.snap()
	assert.Equal(h.t, "blocker", s.Cards["c3"].Priority)
	assert.Equal(h.t, "low", s.Cards["g1"].Priority)
	assert.Equal(h.t, "normal", s.Cards["k1"].Priority, "a consumer's priority is normal, named or not")
	assert.Equal(h.t, "normal", s.Cards["c1"].Priority, "the default is normal")
	r := h.ok(h.op("report", []Expect{CardGen{"u1", 0}}, SetCardPriorHead{"u1", "abc"}, SetCardCost{"u1", math.MaxInt32 + 1, true}, SetCardResult{"u1", "fault"},
		SetCardBounces{"c1", 2}, SetCardBounceBase{"c1", 2}, SetCardPriority{"c1", "critical"}, SetCardCost{"u2", 5, false}))
	u1want := Card{Tier: "pro", Priority: "normal", ID: "u1", Kind: "work", Of: "c1", Stream: "s1", State: "waiting", Holder: "w1", Gen: 1, Prior: "abc", Cost: math.MaxInt32 + 1, Priced: true, Result: "fault"}
	assert.Equal(h.t, u1want, r.After.Cards["u1"], "cost is a bigint: it goes past the integer range")
	c1want := c1
	c1want.Gen, c1want.Bounces, c1want.BounceBase, c1want.Priority = 1, 2, 2, "critical"
	assert.Equal(h.t, c1want, r.After.Cards["c1"])
	assert.False(h.t, r.After.Cards["u2"].Priced, "an unpriced run is unpriced, never $0")
	h.ok(h.op("level", nil, SetCardPriority{"g1", "high"}))
	r = h.ok(h.op("cut", nil, SetCardAttempt{"c1", 3}, PutCard{Tier: "pro", Kind: "work", ID: "k3", Of: "c1", Stream: "s1", Holder: "w1", Attempt: 3}))
	assert.Equal(h.t, 3, r.After.Cards["k3"].Attempt, "a consumer carries the attempt it belongs to, set at add")
	assert.Equal(h.t, 3, r.After.Cards["c1"].Attempt)
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{SetCardPriority{"u1", "high"}, "card u1 is of kind work; priority is a producer's"},
		{PutCard{Tier: "pro", Kind: "work", ID: "k2", Of: "c1", Stream: "s1", Holder: "w1", Priority: "high"}, "card k2 is of kind work; priority is a producer's"},
		{SetCardPriorHead{"u9", "x"}, "card u9 does not exist"},
		{SetCardBounces{"c9", 1}, "card c9 does not exist"},
		{SetCardCost{"u9", 1, true}, "card u9 does not exist"},
		// the kind-specific columns: the store enforces whose they are
		{SetCardPriorHead{"u2", "x"}, "card u2 is of kind read; SetCardPriorHead is only for a work consumer"},
		{SetCardPriorHead{"c1", "x"}, "card c1 is of kind producer; SetCardPriorHead is only for a work consumer"},
		{SetCardBounces{"u1", 1}, "card u1 is of kind work; SetCardBounces is only for a producer"},
		{SetCardBounces{"g1", 1}, "card g1 is of kind sentinel; SetCardBounces is only for a producer"},
		{SetCardBounceBase{"u1", 1}, "card u1 is of kind work; SetCardBounceBase is only for a producer"},
		{SetCardBounceBase{"g1", 0}, "card g1 is of kind sentinel; SetCardBounceBase is only for a producer"},
		{SetCardCost{"c1", 1, true}, "card c1 is of kind producer; SetCardCost is only for a consumer"},
		{SetCardResult{"c1", "ok"}, "card c1 is of kind producer; SetCardResult is only for a consumer"},
		{SetCardResult{"g1", ""}, "card g1 is of kind sentinel; SetCardResult is only for a consumer"},
		{SetCardAttempt{"u1", 2}, "card u1 is of kind work; SetCardAttempt is only for a producer or sentinel (a consumer's attempt is set at add)"},
		{PutCard{Tier: "pro", Kind: "producer", ID: "c4", Stream: "s1", Attempt: 1}, "card c4 is a producer; its attempt starts at 0 and is raised by SetCardAttempt"},
		// the kind decides, before the value is looked at: a no-op value on
		// the wrong kind is refused too (Emma's second read, defect 1)
		{SetCardPriority{"u1", "normal"}, "card u1 is of kind work; priority is a producer's"},
		{SetCardPriority{"u2", "normal"}, "card u2 is of kind read; priority is a producer's"},
		{SetCardNeeds{"u1", nil}, "card u1 is of kind work; SetCardNeeds is only for a producer or sentinel"},
		{SetCardNeeds{"u2", []string{}}, "card u2 is of kind read; SetCardNeeds is only for a producer or sentinel"},
		{SetCardAfter{"u1", nil}, "card u1 is of kind work; SetCardAfter is only for a producer or sentinel"},
		{SetCardAfter{"u2", []string{}}, "card u2 is of kind read; SetCardAfter is only for a producer or sentinel"},
		{SetCardPriorHead{"c1", ""}, "card c1 is of kind producer; SetCardPriorHead is only for a work consumer"},
		{SetCardBounces{"u1", 0}, "card u1 is of kind work; SetCardBounces is only for a producer"},
		{SetCardCost{"c1", 0, false}, "card c1 is of kind producer; SetCardCost is only for a consumer"},
		{SetCardResult{"c1", ""}, "card c1 is of kind producer; SetCardResult is only for a consumer"},
		{SetCardAttempt{"u1", 0}, "card u1 is of kind work; SetCardAttempt is only for a producer or sentinel (a consumer's attempt is set at add)"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	h.invalidAll([]refusal{
		{SetCardPriority{"c1", "urgent"}, `card priority "urgent" is not one of low, normal, high, critical, blocker`},
		{SetCardPriority{"c1", "read"}, `card priority "read" is not one of low, normal, high, critical, blocker`}, // read is a rung, not a level
		{PutCard{Tier: "pro", Kind: "producer", ID: "c4", Stream: "s1", Priority: "top"}, `card priority "top" is not one of low, normal, high, critical, blocker`},
		{SetCardResult{"u1", "faulty"}, `card result "faulty" is not one of , ok, fix, fail, fault`},
	})
	for name, o := range map[string]Op{
		"negative cost":    h.op("x", nil, SetCardCost{"u1", -1, true}),
		"negative bounces": h.op("x", nil, SetCardBounces{"c1", -1}),
		"bounces too big":  h.op("x", nil, SetCardBounces{"c1", math.MaxInt32 + 1}),
		"negative base":    h.op("x", nil, SetCardBounceBase{"c1", -1}),
		"cost twice":       h.op("x", nil, SetCardCost{"u1", 1, true}, SetCardCost{"u1", 2, true}),
		"priority twice":   h.op("x", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c5", Stream: "s1"}, SetCardPriority{"c5", "high"}),
		"nul prior head":   h.op("x", nil, SetCardPriorHead{"u1", "a\x00"}),
	} {
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
	}
	assert.Equal(h.t, Rebuild(h.events()), h.snap(), "images carry every new column")
}

// Round 13, from building layer 2: a producer's review counts its entries
// into review (SetCardReview, +1, producers only); a read consumer is cut at
// its producer's current review and keeps it; a row carries the reason it was
// held or released (SetRowHeld), kept by PutRow.
func reviewAndRowReason(h *harness) {
	h.seed()
	r := h.ok(h.op("review", []Expect{CardGen{"c1", 0}}, SetCardReview{"c1"}, SetRowHeld{"w1", true, "maintenance"}))
	assert.Equal(h.t, 1, r.After.Cards["c1"].Review)
	assert.Equal(h.t, Row{Name: "w1", State: "up", Session: "sess-a", Role: "worker", Held: true, Reason: "maintenance", Gen: 1}, r.After.Rows["w1"])
	h.ok(h.op("unhold", nil, SetRowHeld{"w1", false, "back"}, PutRow{Name: "w1", Session: "sess-a", Role: "worker"}))
	assert.Equal(h.t, "back", h.snap().Rows["w1"].Reason, "PutRow keeps the reason")
	r = h.ok(h.op("cut", nil, PutCard{Tier: "pro", Kind: "read", ID: "k1", Of: "c1", Stream: "s1", Holder: "w1", Review: 1}))
	assert.Equal(h.t, 1, r.After.Cards["k1"].Review, "a read is cut at its producer's review")
	h.ok(h.op("again", nil, SetCardReview{"c1"}, SetCardState{"k1", "ready"}))
	assert.Equal(h.t, []int{2, 1}, []int{h.snap().Cards["c1"].Review, h.snap().Cards["k1"].Review}, "the read keeps the review it was cut at")
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{SetCardReview{"c9"}, "card c9 does not exist"},
		{SetCardReview{"u2"}, "card u2 is of kind read; SetCardReview is only for a producer"},
		{SetCardReview{"u1"}, "card u1 is of kind work; SetCardReview is only for a producer"},
		{PutCard{Tier: "pro", Kind: "read", ID: "k2", Of: "c1", Stream: "s1", Holder: "w1", Review: 1}, "card k2 is cut at review 1, its producer c1 is at review 2; a read takes its producer's review"},
		{PutCard{Tier: "pro", Kind: "work", ID: "k2", Of: "c1", Stream: "s1", Holder: "w1", Review: 2}, "card k2 is a work; only a read consumer is cut at a review (a producer's starts at 0 and is raised by SetCardReview)"},
		{PutCard{Tier: "pro", Kind: "producer", ID: "c4", Stream: "s1", Review: 1}, "card c4 is a producer; only a read consumer is cut at a review (a producer's starts at 0 and is raised by SetCardReview)"},
		{SetRowHeld{"w1", false, "other reason"}, "row w1 is already not held"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	for name, o := range map[string]Op{
		"review twice":    h.op("x", nil, SetCardReview{"c1"}, SetCardReview{"c1"}),
		"negative review": h.op("x", nil, PutCard{Tier: "pro", Kind: "read", ID: "k3", Of: "c1", Stream: "s1", Holder: "w1", Review: -1}),
		"nul row reason":  h.op("x", nil, SetRowHeld{"w1", true, "a\x00"}),
	} {
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
	}
	assert.Equal(h.t, Rebuild(h.events()), h.snap(), "images carry review and the row reason")
}

// Round 14, from the first cold read of layer 2: a producer's finding is
// set by SetCardFinding (producers only); a work consumer is cut with its
// producer's finding and keeps it.
func finding(h *harness) {
	h.seed()
	r := h.ok(h.op("fix", []Expect{CardGen{"c1", 0}}, SetCardFinding{"c1", "rename x"}))
	assert.Equal(h.t, "rename x", r.After.Cards["c1"].Finding)
	r = h.ok(h.op("cut", nil, PutCard{Tier: "pro", Kind: "work", ID: "k1", Of: "c1", Stream: "s1", Holder: "w1", Finding: "rename x"}))
	assert.Equal(h.t, "rename x", r.After.Cards["k1"].Finding, "a work consumer is cut with its producer's finding")
	h.ok(h.op("again", nil, SetCardFinding{"c1", "and y"}, SetCardState{"k1", "ready"}))
	assert.Equal(h.t, []string{"and y", "rename x"}, []string{h.snap().Cards["c1"].Finding, h.snap().Cards["k1"].Finding}, "the consumer keeps its copy")
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{SetCardFinding{"c9", "x"}, "card c9 does not exist"},
		{SetCardFinding{"u1", "x"}, "card u1 is of kind work; SetCardFinding is only for a producer"},
		{SetCardFinding{"u2", "x"}, "card u2 is of kind read; SetCardFinding is only for a producer"},
		{SetCardFinding{"c1", "and y"}, "card c1 already has the given finding"},
		{PutCard{Tier: "pro", Kind: "work", ID: "k2", Of: "c1", Stream: "s1", Holder: "w1", Finding: "rename x"}, `card k2 is cut with finding "rename x", its producer c1 has "and y"; a work consumer takes its producer's finding`},
		{PutCard{Tier: "pro", Kind: "read", ID: "k2", Of: "c1", Stream: "s1", Holder: "w1", Finding: "x"}, "card k2 is a read; only a work consumer is cut with a finding (a producer's is set by SetCardFinding)"},
		{PutCard{Tier: "pro", Kind: "producer", ID: "c4", Stream: "s1", Finding: "x"}, "card c4 is a producer; only a work consumer is cut with a finding (a producer's is set by SetCardFinding)"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	for name, o := range map[string]Op{
		"finding twice": h.op("x", nil, SetCardFinding{"c1", "a"}, SetCardFinding{"c1", "b"}),
		"nul finding":   h.op("x", nil, SetCardFinding{"c1", "a\x00"}),
	} {
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
	}
	assert.Equal(h.t, Rebuild(h.events()), h.snap(), "images carry the finding")
}

// Round 23: a consumer is cut with the model its deal named and keeps it
// (no write changes it); a producer or sentinel carries none.
func model(h *harness) {
	h.seed()
	r := h.ok(h.op("cut", nil, PutCard{Tier: "pro", Kind: "work", ID: "k1", Of: "c1", Stream: "s1", Holder: "w1", Model: "deepseek/deepseek-flash"}))
	assert.Equal(h.t, "deepseek/deepseek-flash", r.After.Cards["k1"].Model, "a consumer is cut with its deal's model")
	assert.Equal(h.t, "", h.snap().Cards["c1"].Model, "a producer has none")
	h.refused(h.op("bad", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c4", Stream: "s1", Model: "x/y"}),
		"card c4 is a producer; only a consumer is cut with a model (the model its deal named)")
	_, err := h.s.Apply(h.ctx, h.op("x", nil, PutCard{Tier: "pro", Kind: "work", ID: "k2", Of: "c1", Stream: "s1", Holder: "w1", Model: "a\x00"}))
	assert.ErrorIs(h.t, err, ErrInvalid, "nul model")
	assert.Equal(h.t, Rebuild(h.events()), h.snap(), "images carry the model")
}

// Round 17: a producer records a red read verdict (SetCardRed) so a later
// waiver can require it, and CardRed fences a plan on it. Every outcome:
// accepted (in the receipt and the event's images), refused as changing
// nothing, refused on a consumer and a sentinel naming the fix, refused on a
// missing card, ErrInvalid for a malformed op, and rebuild equal to the
// snapshot.
func red(h *harness) {
	h.seed()
	h.ok(h.op("gate", nil, PutCard{Tier: "pro", Kind: "sentinel", ID: "t1", Stream: "s1"}))
	assert.False(h.t, h.snap().Cards["c1"].Red, "a card is added not red")
	h.ok(h.op("check", []Expect{CardRed{"c1", false}}))
	h.refused(h.op("check", []Expect{CardRed{"c1", true}}), "card c1 is not red, expected red")
	r := h.ok(h.op("red", []Expect{CardRed{"c1", false}, CardGen{"c1", 0}}, SetCardRed{"c1", true}))
	assert.True(h.t, r.After.Cards["c1"].Red)
	assert.Equal(h.t, 1, r.After.Cards["c1"].Gen, "the write moves gen")
	ev := h.events()
	last := ev[len(ev)-1]
	require.True(h.t, last.Accepted)
	assert.False(h.t, last.Before.Cards["c1"].Red, "the before-image")
	assert.True(h.t, last.After.Cards["c1"].Red, "the after-image carries red")
	h.ok(h.op("check", []Expect{CardRed{"c1", true}}))
	h.refused(h.op("check", []Expect{CardRed{"c1", false}}), "card c1 is red, expected not red")
	h.refused(h.op("check", []Expect{CardRed{"c9", true}}), "card c9 does not exist")
	for _, c := range []struct {
		w      Write
		reason string
	}{
		{SetCardRed{"c1", true}, "card c1 already has the given red"},
		{SetCardRed{"c9", true}, "card c9 does not exist"},
		{SetCardRed{"u1", true}, "card u1 is of kind work; SetCardRed is only for a producer: record the verdict on its producer c1"},
		{SetCardRed{"u2", false}, "card u2 is of kind read; SetCardRed is only for a producer: record the verdict on its producer c1"},
		{SetCardRed{"t1", true}, "card t1 is of kind sentinel; SetCardRed is only for a producer, and a sentinel is never read"},
	} {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
	assert.Equal(h.t, 1, h.snap().Cards["c1"].Gen, "no refusal moved gen")
	n := len(h.events())
	for name, o := range map[string]Op{
		"red twice":      h.op("x", nil, SetCardRed{"c1", false}, SetCardRed{"c1", true}),
		"empty card":     h.op("x", nil, SetCardRed{"", true}),
		"empty cond key": h.op("x", []Expect{CardRed{"", true}}),
	} {
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
	}
	assert.Len(h.t, h.events(), n, "nothing invalid is logged")
	h.ok(h.op("waive", []Expect{CardRed{"c1", true}}, SetCardRed{"c1", false}, SetCardState{"c1", "ready"}))
	assert.False(h.t, h.snap().Cards["c1"].Red)
	assert.Equal(h.t, Rebuild(h.events()), h.snap(), "images carry red")
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

// Ids and names are at most MaxName (128) bytes, ErrInvalid above, so no
// index can refuse one; brief and reason are unbounded. From the second cold
// read's cr2LongIDs, which found an 8000-byte id or name unknown on PG and
// accepted on Fake.
func nameLengths(h *harness) {
	h.seed()
	n := func(c string, size int) string { return strings.Repeat(c, size) }
	cases := func(size int) map[string]Op {
		withID, withActor, withKind := h.op("x", nil, SetCardReason{"c1", "r1"}), h.op("x", nil, SetCardReason{"c1", "r2"}), h.op("x", nil, SetCardReason{"c1", "r3"})
		withID.ID, withActor.Actor, withKind.Kind = n("o", size), n("a", size), n("k", size)
		return map[string]Op{
			"op id":       withID,
			"actor":       withActor,
			"kind label":  withKind,
			"card id":     h.op("x", nil, PutCard{Tier: "pro", ID: n("c", size), Kind: "producer", Stream: "s1"}),
			"row name":    h.op("x", nil, PutRow{Name: n("r", size)}),
			"stream name": h.op("x", nil, PutStream{Name: n("s", size)}),
			"policy name": h.op("x", nil, PutPolicy{"data", n("p", size), "b"}),
		}
	}
	for _, o := range cases(MaxName) {
		h.ok(o)
	}
	logged := len(h.events())
	for name, o := range cases(MaxName + 1) {
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
		assert.ErrorContains(h.t, err, "over 128 bytes", name)
	}
	for name, o := range map[string]Op{
		"a condition naming a long card": h.op("x", []Expect{CardGen{n("c", MaxName+1), 0}}),
		"a holder naming a long row":     h.op("x", nil, PutCard{Tier: "pro", ID: "k9", Kind: "work", Of: "c1", Stream: "s1", Holder: n("r", MaxName+1)}),
		"a need naming a long card":      h.op("x", nil, SetCardNeeds{"c1", []string{n("c", MaxName+1)}}),
	} {
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
	}
	_, found, err := h.s.Replay(h.ctx, n("o", MaxName+1))
	require.NoError(h.t, err)
	assert.False(h.t, found, "an id no op can have is unknown to Replay")
	assert.Len(h.t, h.events(), logged, "nothing is logged for a long name")
	r := h.ok(h.op("big", nil, PutCard{Tier: "pro", ID: "big", Kind: "producer", Stream: "s1", Brief: n("b", 8000)}, SetCardReason{"c1", n("x", 8000)}))
	assert.Len(h.t, r.After.Cards["big"].Brief, 8000, "a brief has no limit")
	assert.Len(h.t, r.After.Cards["c1"].Reason, 8000, "a reason has no limit")
}

// The order of checks: after the known-id lookup the whole body is checked,
// before the epoch and before any condition, so an op outside the closed sets
// is ErrInvalid whatever its epoch or its other conditions say, and nothing is
// logged. From the second cold read's cr2UnknownCondition, which found a nil
// condition refused for its epoch rather than invalid.
func checkOrder(h *harness) {
	h.seed()
	logged := len(h.events())
	for name, c := range map[string]struct {
		expect []Expect
		epoch  int
	}{
		"nil":                           {[]Expect{nil}, 1},
		"nil, wrong epoch":              {[]Expect{nil}, 7},
		"nil after a failing condition": {[]Expect{CardGen{"c1", 99}, nil}, 1},
		"pointer":                       {[]Expect{&CardGen{"c1", 0}}, 1},
		"pointer, wrong epoch":          {[]Expect{&CardGen{"c1", 0}}, 7},
		"embedding type":                {[]Expect{strangeExpect{}}, 1},
	} {
		o := h.op("cond", c.expect, SetCardReason{"c1", "r"})
		o.Epoch = c.epoch
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
		assert.ErrorContains(h.t, err, "not a condition of the closed set", name)
	}
	bad := h.op("write", []Expect{CardGen{"c1", 99}}, SetCardReason{"c1", "r"}, SetCardBranch{"c1", "\x00"})
	bad.Epoch = 7
	_, err := h.s.Apply(h.ctx, bad)
	assert.ErrorIs(h.t, err, ErrInvalid, "a bad write behind a wrong epoch and a failing condition")
	assert.Len(h.t, h.events(), logged, "nothing is logged for an op outside the closed sets")
}

// invalid is each op ErrInvalid, and nothing logged.
func (h *harness) invalid(ops map[string]Op) {
	h.t.Helper()
	n := len(h.events())
	for name, o := range ops {
		_, err := h.s.Apply(h.ctx, o)
		assert.ErrorIs(h.t, err, ErrInvalid, name)
	}
	assert.Len(h.t, h.events(), n, "nothing invalid is logged")
}

// invalidAll applies each write alone and expects ErrInvalid naming reason;
// nothing is logged.
func (h *harness) invalidAll(cases []refusal) {
	h.t.Helper()
	n := len(h.events())
	for _, c := range cases {
		_, err := h.s.Apply(h.ctx, h.op("bad", nil, c.w))
		if assert.ErrorIs(h.t, err, ErrInvalid, c.reason) {
			assert.ErrorContains(h.t, err, c.reason)
		}
	}
	assert.Len(h.t, h.events(), n, "nothing invalid is logged")
}

// refusedAll applies each write alone and expects its reason.
func (h *harness) refusedAll(cases []refusal) {
	h.t.Helper()
	for _, c := range cases {
		h.refused(h.op("bad", nil, c.w), c.reason)
	}
}

type refusal struct {
	w      Write
	reason string
}

// A judgement is a stored question: put opens it, answer and expire close
// it once, JudgementOpen fences it (it has no gen), the images carry it, and
// a clear drops it with the epoch (promise reviews 2026-11-06).
func judgements(h *harness) {
	h.seed()
	deadline := t0.Add(time.Hour)
	o := h.op("open", nil, PutJudgement{ID: "j1", Kind: "red", About: "c1", Question: "main is red; revert?", Command: "nova-sprint answer j1", Group: "red-main", Deadline: deadline},
		PutJudgement{ID: "j2", Kind: "stuck", About: "u1", Group: "red-main"}, PutJudgement{ID: "j3", Kind: "fix"})
	r := h.ok(o)
	j1 := Judgement{ID: "j1", Kind: "red", About: "c1", Question: "main is red; revert?", Command: "nova-sprint answer j1", Group: "red-main", State: "open", Deadline: at(deadline), OpenedAt: at(o.At)}
	assert.Equal(h.t, j1, r.After.Judgements["j1"])
	assert.True(h.t, r.After.Judgements["j2"].Deadline.IsZero(), "no deadline is none")
	h.ok(h.op("check", []Expect{JudgementOpen{"j1"}}))
	h.refused(h.op("check", []Expect{JudgementOpen{"j9"}}), "judgement j9 does not exist")
	h.refusedAll([]refusal{
		{PutJudgement{ID: "j1", Kind: "red"}, "judgement j1 exists"},
		{AnswerJudgement{"j9", "x", ""}, "judgement j9 does not exist"},
		{ExpireJudgement{"j9", "x"}, "judgement j9 does not exist"},
	})
	h.invalidAll([]refusal{{PutJudgement{Kind: "red"}, "a judgement needs an id"}})
	a := h.op("answer", []Expect{JudgementOpen{"j1"}}, AnswerJudgement{"j1", "yes, revert", "glenn"})
	r = h.ok(a)
	j1.State, j1.Answer, j1.Actor, j1.ClosedAt = "answered", "yes, revert", "glenn", at(a.At)
	assert.Equal(h.t, j1, r.After.Judgements["j1"])
	h.refused(h.op("again", []Expect{JudgementOpen{"j1"}}, AnswerJudgement{"j1", "no", ""}), "judgement j1 is answered, expected open")
	h.refusedAll([]refusal{
		{AnswerJudgement{"j1", "no", ""}, "judgement j1 is answered; only an open judgement can be answered"},
		{ExpireJudgement{"j1", "late"}, "judgement j1 is answered; only an open judgement can be expired"},
	})
	e := h.op("expire", nil, ExpireJudgement{"j2", "the deadline passed"}, AnswerJudgement{"j3", "done", ""})
	r = h.ok(e)
	assert.Equal(h.t, Judgement{ID: "j2", Kind: "stuck", About: "u1", Group: "red-main", State: "expired", Answer: "the deadline passed", Actor: "tester", OpenedAt: at(o.At), ClosedAt: at(e.At)}, r.After.Judgements["j2"])
	assert.Equal(h.t, "tester", r.After.Judgements["j3"].Actor, "an answer with no actor is the op's actor")
	h.refused(h.op("check", []Expect{JudgementOpen{"j2"}}), "judgement j2 is expired, expected open")
	h.invalid(map[string]Op{
		"open and answer in one op": h.op("x", nil, PutJudgement{ID: "j4", Kind: "red"}, AnswerJudgement{"j4", "y", ""}),
		"long id":                   h.op("x", nil, PutJudgement{ID: strings.Repeat("j", MaxName+1), Kind: "red"}),
		"long kind":                 h.op("x", nil, PutJudgement{ID: "j4", Kind: strings.Repeat("k", MaxName+1)}),
		"nul question":              h.op("x", nil, PutJudgement{ID: "j4", Kind: "red", Question: "a\x00"}),
		"deadline past 9999":        h.op("x", nil, PutJudgement{ID: "j4", Kind: "red", Deadline: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}),
	})
	s := h.snap()
	assert.Len(h.t, s.Judgements, 3)
	assert.Equal(h.t, j1, s.Judgements["j1"])
	assert.Equal(h.t, Rebuild(h.events()), s, "the images carry judgements")
	assert.Equal(h.t, 0, s.Cards["c1"].Gen, "a judgement about a card is not a write to it")
	h.clear()
	s = h.snap()
	assert.Empty(h.t, s.Judgements, "a clear drops the epoch's judgements")
	assert.Equal(h.t, Rebuild(h.events()), s)
	h.ok(h.op("reopen", nil, PutJudgement{ID: "j1", Kind: "red"}))
}

// A cursor is a subscriber's place in the log (the last seq it read): set
// inserts it, it never moves back or past the log, the images carry it, and
// a clear keeps it, since seq is global (a subscriber resumes where it was).
func cursors(h *harness) {
	h.seed() // seq 1
	r := h.ok(h.op("push", nil, SetCursor{"push-w1", 1}, SetCursor{"feed", 1}))
	assert.Equal(h.t, map[string]int64{"push-w1": 1, "feed": 1}, r.After.Cursors)
	assert.Nil(h.t, r.After.Cards, "a cursor is not a card")
	h.refused(h.op("same", nil, SetCursor{"push-w1", 1}), "cursor push-w1 is already at 1")
	h.ok(h.op("on", nil, SetCursor{"push-w1", 3})) // the last event before it is seq 3
	h.refused(h.op("back", nil, SetCursor{"push-w1", 2}), "cursor push-w1 is at 3, cannot move back to 2")
	h.refused(h.op("ahead", nil, SetCursor{"feed", 99}), "cursor feed cannot pass the log at 5") // seq 5 is the refusal before it
	h.refused(h.op("absent", nil, SetCursor{"new", 7}), "cursor new cannot pass the log at 6")   // an absent cursor reads as 0
	h.refused(h.op("zero", nil, SetCursor{"new", 0}), "cursor new is already at 0")              // so 0 on it changes nothing (store-cursor-zero)
	h.invalid(map[string]Op{
		"negative":       h.op("x", nil, SetCursor{"feed", -1}),
		"twice in an op": h.op("x", nil, SetCursor{"feed", 1}, SetCursor{"feed", 2}),
		"long name":      h.op("x", nil, SetCursor{strings.Repeat("c", MaxName+1), 1}),
	})
	h.invalidAll([]refusal{{SetCursor{"", 1}, "a cursor needs a name"}})
	ev := h.events()
	assert.Equal(h.t, &State{Cursors: map[string]int64{"push-w1": 1}}, ev[3].Before, "the before-image holds the old seq")
	want := map[string]int64{"push-w1": 3, "feed": 1}
	s := h.snap()
	assert.Equal(h.t, want, s.Cursors)
	assert.Equal(h.t, Rebuild(h.events()), s, "the images carry cursors")
	h.clear()
	s = h.snap()
	assert.Equal(h.t, want, s.Cursors, "a clear keeps the cursors: seq is global")
	assert.Equal(h.t, Rebuild(h.events()), s)
	h.refused(h.op("restart", nil, SetCursor{"push-w1", 1}), "cursor push-w1 is at 3, cannot move back to 1")
	h.ok(h.op("resume", nil, SetCursor{"push-w1", s.Seq}))
}

// Tiers: a card names one (a consumer its producer's), one of the four.
// Which tiers a row serves is the roster's since round 22 (nova-config, read
// each tick); the deal guards check it, the store does not, so a consumer of
// any tier is added to any row of the epoch.
func tiers(h *harness) {
	h.seed()
	h.ok(h.op("rows", nil, PutRow{Name: "w2"}))
	assert.Equal(h.t, Row{Name: "w2", State: "down"}, h.snap().Rows["w2"])
	h.ok(h.op("heavy", nil, PutCard{Tier: "heavy", Kind: "producer", ID: "c5", Stream: "s1"}))
	all := "flash, pro, heavy, frontier"
	k5 := func(holder string) PutCard {
		return PutCard{Tier: "heavy", Kind: "read", ID: "k5", Of: "c5", Stream: "s1", Holder: holder}
	}
	h.refusedAll([]refusal{
		{k5("w9"), "row w9 does not exist"},
		{PutCard{Tier: "pro", Kind: "work", ID: "k6", Of: "c5", Stream: "s1", Holder: "w1"}, "card k6 is tier pro, its producer c5 is tier heavy; a consumer takes its producer's tier"},
	})
	h.invalidAll([]refusal{
		{PutCard{Kind: "producer", ID: "c6", Stream: "s1"}, `card tier "" is not one of ` + all},
		{PutCard{Tier: "mega", Kind: "sentinel", ID: "g6", Stream: "s1"}, `card tier "mega" is not one of ` + all},
	})
	r := h.ok(h.op("any row", nil, k5("w2"), SetCardState{"k5", "working"}))
	assert.Equal(h.t, Card{Tier: "heavy", Priority: "normal", ID: "k5", Kind: "read", Of: "c5", Stream: "s1", State: "working", Holder: "w2"}, r.After.Cards["k5"], "any row takes the consumer")
	h.ok(h.op("finish", nil, SetCardState{"k5", "done"}))
	assert.Equal(h.t, "w2", h.snap().Cards["k5"].Holder, "a consumer's holder never changes")
	assert.Equal(h.t, Rebuild(h.events()), h.snap())
}

// The card contract's structured fields: a producer's or sentinel's to set,
// a consumer's copies fixed at add.
func cardContract(h *harness) {
	h.seed()
	r := h.ok(h.op("add", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c5", Stream: "s1", Brief: "the whole brief", Paths: []string{"store/"}, DoneWhen: []string{"tests green"}, Prose: "make it so"},
		PutCard{Tier: "pro", Kind: "work", ID: "k5", Of: "c5", Stream: "s1", Holder: "w1", Paths: []string{"store/"}, DoneWhen: []string{"tests green"}, Prose: "make it so"}))
	c5 := r.After.Cards["c5"]
	assert.Equal(h.t, Card{Tier: "pro", Priority: "normal", ID: "c5", Kind: "producer", Stream: "s1", State: "waiting", Brief: "the whole brief", Paths: []string{"store/"}, DoneWhen: []string{"tests green"}, Prose: "make it so"}, c5)
	assert.Equal(h.t, []string{"store/"}, r.After.Cards["k5"].Paths, "a consumer carries its copy")
	r = h.ok(h.op("edit", nil, SetCardPaths{"c5", []string{"store/", "docs/"}}, SetCardDoneWhen{"c5", nil}, SetCardProse{"c5", "make it so, carefully"}))
	c5.Gen, c5.Paths, c5.DoneWhen, c5.Prose = 1, []string{"store/", "docs/"}, nil, "make it so, carefully"
	assert.Equal(h.t, c5, r.After.Cards["c5"], "three writes, one gen")
	h.ok(h.op("gate", nil, PutCard{Tier: "pro", Kind: "sentinel", ID: "g5", Stream: "s1"}, SetCardProse{"c0", "landed already"}))
	h.ok(h.op("gate", nil, SetCardDoneWhen{"g5", []string{"every card landed"}}))
	h.refusedAll([]refusal{
		{SetCardPaths{"k5", nil}, "card k5 is of kind work; SetCardPaths is only for a producer or sentinel (a consumer's copy is set at add)"},
		{SetCardDoneWhen{"u2", []string{"x"}}, "card u2 is of kind read; SetCardDoneWhen is only for a producer or sentinel (a consumer's copy is set at add)"},
		{SetCardProse{"k5", ""}, "card k5 is of kind work; SetCardProse is only for a producer or sentinel (a consumer's copy is set at add)"},
		{SetCardPaths{"c9", nil}, "card c9 does not exist"},
	})
	h.invalid(map[string]Op{
		"nul path":         h.op("x", nil, SetCardPaths{"c5", []string{"a\x00"}}),
		"add then prose":   h.op("x", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c7", Stream: "s1"}, SetCardProse{"c7", "p"}),
		"nul done when":    h.op("x", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c7", Stream: "s1", DoneWhen: []string{"\x00"}}),
		"paths twice":      h.op("x", nil, SetCardPaths{"c5", nil}, SetCardPaths{"c5", []string{"a"}}),
		"nul prose on add": h.op("x", nil, PutCard{Tier: "pro", Kind: "producer", ID: "c7", Stream: "s1", Prose: "\x00"}),
	})
	assert.Equal(h.t, Rebuild(h.events()), h.snap(), "images carry the contract columns")
}

// Every event records the policy version active when its op was checked, 0
// when none; Rebuild ignores it.
func eventPolicy(h *harness) {
	h.seed()
	h.ok(h.op("put", nil, PutPolicy{"data", "p1", "{}"}, PutPolicy{"data", "p2", "{}"}))
	h.ok(h.op("on", nil, SetActivePolicy{1}))
	h.ok(h.op("work", nil, SetCardReason{"c1", "r"}))
	h.refused(h.op("bad", nil, SetCardReason{"c9", "r"}), "card c9 does not exist")
	h.ok(h.op("switch", nil, SetActivePolicy{2}))
	h.ok(h.op("work", nil, SetCardReason{"c1", "s"}))
	c := h.clear()
	var got []int
	for _, e := range h.events() {
		got = append(got, e.Policy)
	}
	assert.Equal(h.t, []int{0, 0, 0, 1, 1, 1, 2, 2}, got, "the version active when each op was checked")
	assert.Equal(h.t, 2, h.events()[len(got)-1].Policy, "a clear records it too: %s", c.Op)
	assert.Equal(h.t, Rebuild(h.events()), h.snap())
}

// Round 10, from the third cold read (cr3Kinds, cr3Order, cr3Judgements,
// cr3Width and cr3Names in coldread3_test.go): a consumer takes its
// producer's stream; a condition on a value outside its closed set is
// invalid; a reason quotes at most MaxName bytes of a caller's value; a
// judgement never closes before it opened; nothing moves without a change;
// Clear with an earlier op's id returns that op's receipt.
func coldRead3(h *harness) {
	h.seed()
	h.ok(h.op("s2", nil, PutStream{Name: "s2"}))
	h.refused(h.op("add", nil, PutCard{Tier: "pro", Kind: "read", ID: "k2", Of: "c1", Stream: "s2", Holder: "w1"}),
		"card k2 is in stream s2, its producer c1 is in stream s1; a consumer takes its producer's stream")
	h.refused(h.op("again", nil, SetCardState{"c1", "waiting"}), "card c1 is already waiting")
	long := strings.Repeat("x", 1000)
	_, err := h.s.Apply(h.ctx, h.op("long", nil, SetCardState{"c1", long}))
	require.ErrorIs(h.t, err, ErrInvalid)
	assert.ErrorContains(h.t, err, `card state "`+long[:MaxName]+`..." is not one of `+strings.Join(CardStates, ", "))
	assert.NotContains(h.t, err.Error(), long[:MaxName+1])
	h.invalid(map[string]Op{
		"card state": h.op("x", []Expect{CardState{"c1", "flying"}}),
		"row state":  h.op("x", []Expect{RowIs{"w1", "asleep"}}),
		"long state": h.op("x", []Expect{CardState{"c1", long}}),
	})
	_, err = h.s.Apply(h.ctx, h.op("x", []Expect{CardState{"c1", long}}))
	assert.NotContains(h.t, err.Error(), long[:MaxName+1], "an error quotes at most MaxName bytes too")

	open := h.op("open", nil, PutJudgement{ID: "j1", Kind: "red"})
	h.ok(open)
	early := h.op("answer", nil, AnswerJudgement{"j1", "yes", ""})
	early.At = open.At.Add(-time.Second)
	h.refused(early, "judgement j1 opened at "+at(open.At).Format(time.RFC3339Nano)+", cannot close at "+at(early.At).Format(time.RFC3339Nano))
	same := h.op("answer", nil, AnswerJudgement{"j1", "yes", ""})
	same.At = open.At
	h.ok(same) // closing at the instant it opened is not before it

	first := h.op("reason", nil, SetCardReason{"c1", "r"})
	r := h.ok(first)
	c, err := h.s.Clear(h.ctx, Op{ID: first.ID, Epoch: 1, Kind: "clear", Actor: "tester", At: first.At})
	require.NoError(h.t, err)
	assert.Equal(h.t, r, c, "Clear with an earlier op's id returns that op's receipt")
	assert.Zero(h.t, c.NewEpoch)
	assert.Equal(h.t, 1, h.snap().Epoch, "and opens nothing")
}

// Round 11, from the fourth cold read (coldread4_test.go): one rule for a
// value outside its closed set and for an empty key, in a write exactly as
// in a condition (ErrInvalid, nothing logged, and no reason that names
// nothing); and every set write that would change nothing is refused, so no
// gen moves and the refusal carries no image.
func coldRead4(h *harness) {
	h.seed()
	h.ok(h.op("setup", nil, PutJudgement{ID: "j1", Kind: "red"}, PutPolicy{"data", "p", ""}, SetActivePolicy{1}, SetCursor{"feed", 1}))
	h.invalidAll([]refusal{
		{PutCard{Tier: "pro", Kind: "producer", Stream: "s1"}, "a card needs an id"},
		{PutStream{}, "a stream needs a name"},
		{PutRow{}, "a row needs a name"},
		{PutJudgement{Kind: "red"}, "a judgement needs an id"},
		{AnswerJudgement{Answer: "y"}, "a judgement needs an id"},
		{ExpireJudgement{Reason: "late"}, "a judgement needs an id"},
		{SetCursor{"", 1}, "a cursor needs a name"},
		{SetStreamHeld{"", true}, "a stream needs a name"},
		{SetCardState{"", "ready"}, "a card needs an id"},
		{SetCardReason{"", "r"}, "a card needs an id"},
		{SetRowState{"", "up"}, "a row needs a name"},
		{SetRowHeld{"", true, ""}, "a row needs a name"},
		{PutCard{Tier: "pro", ID: "c3", Kind: "job", Stream: "s1"}, `card kind "job" is not one of`},
		{PutCard{Tier: "top", ID: "c3", Kind: "producer", Stream: "s1"}, `card tier "top" is not one of`},
		{PutCard{Tier: "pro", ID: "c3", Kind: "producer", Stream: "s1", Priority: "top"}, `card priority "top" is not one of`},
		{SetCardState{"c1", "gone"}, `card state "gone" is not one of`},
		{SetCardResult{"u1", "maybe"}, `card result "maybe" is not one of`},
		{SetRowState{"w1", "gone"}, `row state "gone" is not one of`},
		{PutPolicy{"yaml", "p", "b"}, `policy kind "yaml" is not one of`},
	})
	conds := map[string]Expect{
		"card gen": CardGen{"", 0}, "card state": CardState{"", "ready"}, "card holder": CardHolder{"", ""}, "needs landed": NeedsLanded{""},
		"row is": RowIs{"", "up"}, "row gen": RowGen{"", 0}, "row held": RowHeld{"", false}, "stream held": StreamHeld{"", false},
		"card absent": CardAbsent{""}, "judgement open": JudgementOpen{""}, "card red": CardRed{"", true},
	}
	n := len(h.events())
	for name, e := range conds {
		_, err := h.s.Apply(h.ctx, h.op("x", []Expect{e}))
		if assert.ErrorIs(h.t, err, ErrInvalid, name) {
			assert.Contains(h.t, err.Error(), " needs ", name)
			assert.NotContains(h.t, err.Error(), "  ", name)
		}
	}
	assert.Len(h.t, h.events(), n, "nothing invalid is logged")
	h.ok(h.op("no holder", []Expect{CardHolder{"c1", ""}})) // an empty holder is none, not an empty key

	before := h.snap()
	n = len(h.events())
	h.refusedAll([]refusal{
		{SetCardState{"c1", "waiting"}, "card c1 is already waiting"},
		{SetRowState{"w1", "up"}, "row w1 is already up"},
		{SetRowHeld{"w1", false, ""}, "row w1 is already not held"},
		{SetStreamHeld{"s1", false}, "stream s1 is already not held"},
		{SetActivePolicy{1}, "policy version 1 is already active"},
		{SetCursor{"feed", 1}, "cursor feed is already at 1"},
		{SetCardScore{"c1", 10}, "card c1 already has the given score"},
		{SetCardReason{"c1", ""}, "card c1 already has the given reason"},
		{SetCardBranch{"c1", ""}, "card c1 already has the given branch"},
		{SetCardHead{"c1", ""}, "card c1 already has the given head"},
		{SetCardPR{"c1", 0}, "card c1 already has the given pr"},
		{SetCardAuthor{"c1", ""}, "card c1 already has the given author"},
		{SetCardBaseSHA{"c1", ""}, "card c1 already has the given base sha"},
		{SetCardAttempt{"c1", 0}, "card c1 already has the given attempt"},
		{SetCardBounces{"c1", 0}, "card c1 already has the given bounces"},
		{SetCardBounceBase{"c1", 0}, "card c1 already has the given bounce base"},
		{SetCardPriority{"c1", "normal"}, "card c1 already has the given priority"},
		{SetCardPriority{"c1", ""}, "card c1 already has the given priority"}, // "" is normal, as at put card
		{SetCardNeeds{"c1", []string{"c0"}}, "card c1 already has the given needs"},
		{SetCardAfter{"c1", nil}, "card c1 already has the given after"},
		{SetCardPaths{"c1", []string{}}, "card c1 already has the given paths"}, // empty and none are one value
		{SetCardDoneWhen{"c1", nil}, "card c1 already has the given done when"},
		{SetCardProse{"c1", ""}, "card c1 already has the given prose"},
		{SetCardResult{"u1", ""}, "card u1 already has the given result"},
		{SetCardPriorHead{"u1", ""}, "card u1 already has the given prior head"},
		{SetCardCost{"u1", 0, false}, "card u1 already has the given cost"},
		{SetRowFlaps{"w1", 0}, "row w1 already has the given flaps"},
		{SetCardPriority{"u1", "normal"}, "card u1 is of kind work; priority is a producer's"}, // the kind rule first
	})
	after := h.snap()
	after.Seq = before.Seq
	assert.Equal(h.t, before, after, "a write that changes nothing moves no gen")
	for _, e := range h.events()[n:] {
		assert.False(h.t, e.Accepted, e.Op)
		assert.Nil(h.t, e.Before, e.Op)
		assert.Nil(h.t, e.After, e.Op)
	}
	r := h.ok(h.op("change", nil, SetCardScore{"c1", 11}, SetCardCost{"u1", 0, true}, SetRowHeld{"w1", true, ""}, SetStreamHeld{"s1", true}))
	assert.Equal(h.t, 1, r.After.Cards["c1"].Gen)
	assert.True(h.t, r.After.Cards["u1"].Priced, "a priced run at $0 is a change from unpriced")
	assert.Equal(h.t, 1, r.After.Rows["w1"].Gen)
	h.refusedAll([]refusal{
		{SetRowHeld{"w1", true, ""}, "row w1 is already held"},
		{SetStreamHeld{"s1", true}, "stream s1 is already held"},
	})
	assert.Equal(h.t, Rebuild(h.events()), h.snap())
}
