package store

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unit tests: Fake only. No real time, no sockets, no network, no outside services.

func newHarness(t *testing.T) (*harness, *Fake) {
	f := NewFake()
	return &harness{t: t, s: f, ctx: context.Background(), epoch: 1}, f
}

func TestConformanceFake(t *testing.T) {
	t.Parallel()
	RunConformance(t, func(*testing.T) Store { return NewFake() })
}

func TestFakeDropNotLanded(t *testing.T) {
	t.Parallel()
	h, f := newHarness(t)
	h.seed()
	o := h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"})
	f.Drop(o.ID, false)
	_, err := f.Apply(h.ctx, o)
	require.ErrorIs(t, err, ErrUnknown)
	_, ok, err := f.Replay(h.ctx, o.ID)
	require.NoError(t, err)
	assert.False(t, ok, "it did not land")
	assert.Equal(t, "waiting", h.snap().Cards["c1"].State)
	r := h.ok(o)
	assert.Equal(t, 1, r.After.Cards["c1"].Gen, "applying again with the same id lands it once")
}

func TestFakeDropLanded(t *testing.T) {
	t.Parallel()
	h, f := newHarness(t)
	h.seed()
	o := h.op("take", nil, SetCardState{"c1", "working"})
	f.Drop(o.ID, true)
	_, err := f.Apply(h.ctx, o)
	require.ErrorIs(t, err, ErrUnknown)
	r, ok, err := f.Replay(h.ctx, o.ID)
	require.NoError(t, err)
	require.True(t, ok, "it landed")
	assert.True(t, r.Accepted)
	assert.Equal(t, r, h.ok(o))
	assert.Equal(t, 1, h.snap().Cards["c1"].Gen, "the gen moved once")

	c := h.op("clear", nil)
	f.Drop(c.ID, true)
	_, err = f.Clear(h.ctx, c)
	require.ErrorIs(t, err, ErrUnknown)
	r, err = f.Clear(h.ctx, c)
	require.NoError(t, err)
	assert.Equal(t, 2, r.NewEpoch)
	d := h.op("clear", nil)
	d.Epoch = 2
	f.Drop(d.ID, false)
	_, err = f.Clear(h.ctx, d)
	require.ErrorIs(t, err, ErrUnknown)
	assert.Equal(t, 2, h.snap().Epoch, "a dropped clear that did not land changed nothing")
}

// The fake serializes concurrent applies the way PG does.
func TestFakeRaces(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)
	h.seed()
	h.ok(h.op("ready", nil, PutRow{Name: "w1", Session: "sess-a", Role: "worker"}, SetCardState{"u1", "ready"}, SetCardState{"u2", "ready"}))
	for name, ops := range map[string][]Op{
		"take": {h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"}), h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"})},
		"deal": {h.op("deal", []Expect{RowGen{"w1", 1}}, SetCardState{"u1", "working"}, SetRowFlaps{"w1", 1}), h.op("deal", []Expect{RowGen{"w1", 1}}, SetCardState{"u2", "working"}, SetRowFlaps{"w1", 2})}, // the row gen fences a deal; width is the roster's (round 22)
	} {
		got := make([]Receipt, 2)
		var wg sync.WaitGroup
		for i := range ops {
			wg.Go(func() {
				r, err := h.s.Apply(h.ctx, ops[i])
				assert.NoError(t, err)
				got[i] = r
			})
		}
		wg.Wait()
		assert.NotEqual(t, got[0].Accepted, got[1].Accepted, "%s: exactly one is accepted", name)
	}
}

func TestEventsCap(t *testing.T) {
	t.Parallel()
	h, f := newHarness(t)
	for i := range MaxEvents + 5 {
		h.ok(h.op("s", nil, PutStream{Name: fmt.Sprint("s", i)}))
	}
	for _, max := range []int{0, -1, MaxEvents + 1, 5000} {
		page, err := f.Events(h.ctx, 0, max)
		require.NoError(t, err)
		assert.Len(t, page, MaxEvents, "max %d is capped", max)
	}
	page, err := f.Events(h.ctx, MaxEvents, 0)
	require.NoError(t, err)
	assert.Len(t, page, 5)
}

func TestRebuildCarriesClearImage(t *testing.T) {
	t.Parallel()
	card := Card{Tier: "pro", Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "ready"}
	row := Row{Name: "w1", State: "down"}
	events := []Event{
		{Seq: 1, Accepted: true, After: &State{Streams: map[string]Stream{"s1": {Name: "s1"}}, Cards: map[string]Card{"c1": card}}},
		{Seq: 2, Reason: "no"},
		{Seq: 3, Accepted: true, After: &State{Policies: map[int]PolicyVersion{1: {Version: 1, Active: true}}}},
		{Seq: 4, Accepted: true, After: &State{Epoch: 2, Rows: map[string]Row{"w1": row}}},
		{Seq: 5, Reason: "no"},
	}
	want := Snapshot{Seq: 5, Epoch: 2, Policy: 1, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: map[string]Row{"w1": row}, Judgements: map[string]Judgement{}, Cursors: map[string]int64{}, Stopped: true, StopReason: NeverStarted}
	assert.Equal(t, want, Rebuild(events), "a clear keeps the fresh stop")
	fresh := Snapshot{Epoch: 1, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: map[string]Row{}, Judgements: map[string]Judgement{}, Cursors: map[string]int64{}, Stopped: true, StopReason: "never started"}
	assert.Equal(t, fresh, Rebuild(nil), "no events: a fresh store, stopped, never started")
}

func TestSchemaIsEmbedded(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"CREATE SCHEMA IF NOT EXISTS sprint", fmt.Sprintf("INSERT INTO sprint.meta (id, schema) VALUES (1, %d)", SchemaVersion), "FOREIGN KEY (epoch, holder) REFERENCES sprint.rows (epoch, name)"} {
		assert.Contains(t, schemaSQL, want)
	}
}

// Fake applies PG's bound: a counter at the top of a PostgreSQL integer
// cannot rise, a plain error naming it, and nothing lands (PG's side is
// TestFunctionalClientErrorIsKnown).
func TestFakeCounterBound(t *testing.T) {
	f := NewFake()
	h := &harness{t: t, s: f, ctx: context.Background(), epoch: 1}
	h.seed()
	c := f.cards[key{1, "c1"}]
	c.Gen = math.MaxInt32
	f.cards[key{1, "c1"}] = c
	r := f.rows[key{1, "w1"}]
	r.Gen = math.MaxInt32
	f.rows[key{1, "w1"}] = r
	f.policies[math.MaxInt32] = PolicyVersion{Version: math.MaxInt32, Kind: "data"}
	before := h.snap()
	for w, want := range map[Write]string{
		SetCardReason{"c1", "r"}:   "store: card c1 gen is 2147483647, the most its column holds; it cannot rise",
		SetRowFlaps{"w1", 1}:       "store: row w1 gen is 2147483647, the most its column holds; it cannot rise",
		PutPolicy{"data", "q", ""}: "store: policy version is 2147483647, the most its column holds; it cannot rise",
	} {
		_, err := f.Apply(h.ctx, h.op("x", nil, w))
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrUnknown)
		assert.Equal(t, want, err.Error())
	}
	_, err := f.Clear(h.ctx, h.op("clear", nil))
	assert.EqualError(t, err, "store: row w1 gen is 2147483647, the most its column holds; it cannot rise", "a clear raises every row's gen")
	assert.Equal(t, before, h.snap(), "nothing landed")
}

// The schema hash ignores comments, indentation and blank lines, so a
// comment edit does not refuse every store; any change to the SQL does.
func TestSchemaHash(t *testing.T) {
	assert.Equal(t, schemaHash, hashSchema(schemaSQL))
	assert.Equal(t, schemaHash, hashSchema("-- a new comment\n\n"+strings.ReplaceAll(schemaSQL, "\n", "  -- note\n\n")))
	assert.Equal(t, schemaHash, hashSchema(strings.ReplaceAll(schemaSQL, "\n    ", "\n\t")))
	assert.NotEqual(t, schemaHash, hashSchema(strings.Replace(schemaSQL, "flaps >= 0", "flaps >= 1", 1)))
	assert.NotEqual(t, hashSchema("SELECT 'a -- b';\n"), hashSchema("SELECT 'a -- c';\n"), "a -- inside a string literal is hashed, not stripped")
	assert.Equal(t, hashSchema("SELECT 'it''s';\n"), hashSchema("SELECT 'it''s'; -- x\n"), "a doubled quote stays inside the literal")
	assert.Equal(t, hashSchema("SELECT 1;\nSELECT 2;\n"), hashSchema("SELECT 1; -- 'x\nSELECT 2;\n"), "a quote inside a comment opens nothing")
}

// round16Hash is the hash of round 16's schema.sql, kept in testdata, and
// not this build's: Migrate upgrades exactly that one earlier build. Unit:
// reads a file in the package, no store.
func TestRound16Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round16.sql")
	require.NoError(t, err)
	assert.Equal(t, round16Hash, hashSchema(string(old)))
	assert.NotEqual(t, round16Hash, schemaHash)
	assert.NotContains(t, string(old), " red ")
}

// round22Hash is the hash of round 22's schema.sql, kept in testdata: it
// has no cards.model, which this build's schema adds (round 23). Unit: reads
// a file in the package, no store.
func TestRound22Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round22.sql")
	require.NoError(t, err)
	assert.Equal(t, round22Hash, hashSchema(string(old)))
	assert.NotEqual(t, round22Hash, schemaHash)
	assert.NotContains(t, string(old), "model text")
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS model text")
}

// round21Hash is the hash of round 17's schema.sql, which every round to 21
// carried, kept in testdata: it has the five configuration columns of
// sprint.rows that round 22 drops, and this build's schema has none of them.
// Unit: reads a file in the package, no store.
func TestRound21Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round21.sql")
	require.NoError(t, err)
	assert.Equal(t, round21Hash, hashSchema(string(old)))
	assert.NotEqual(t, round21Hash, schemaHash)
	for _, col := range []string{"kind", "width", "tiers", "model", "harness"} {
		assert.Contains(t, string(old), "\n    "+col+" ", "round 21's rows table has %s", col)
		assert.Contains(t, schemaSQL, "DROP COLUMN IF EXISTS "+col, "round 22 drops %s", col)
	}
}

// checkEventIdentity: EventIdentity names the op and epoch Events holds at
// every seq, a refused op and a clear included, and finds nothing at 0, past
// the tail or below 0. Shared by the Fake (unit) and PG (functional) tests.
func checkEventIdentity(t *testing.T, h *harness) {
	h.seed()
	r, err := h.s.Apply(h.ctx, h.op("stale", []Expect{CardGen{"c1", 7}}, SetCardState{"c1", "working"}))
	require.NoError(t, err)
	require.False(t, r.Accepted, "the stale op is refused and logged")
	h.clear()
	events := h.events()
	require.GreaterOrEqual(t, len(events), 3)
	for _, e := range events {
		op, epoch, found, err := h.s.EventIdentity(h.ctx, e.Seq)
		require.NoError(t, err)
		assert.True(t, found, "seq %d", e.Seq)
		assert.Equal(t, e.Op, op, "seq %d", e.Seq)
		assert.Equal(t, e.Epoch, epoch, "seq %d", e.Seq)
	}
	for _, seq := range []int64{0, -1, events[len(events)-1].Seq + 1} {
		op, epoch, found, err := h.s.EventIdentity(h.ctx, seq)
		require.NoError(t, err)
		assert.False(t, found, "seq %d", seq)
		assert.Equal(t, "", op)
		assert.Equal(t, 0, epoch)
	}
}

func TestEventIdentityFake(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)
	checkEventIdentity(t, h)
}
