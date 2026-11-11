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
	t.Parallel()
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
	t.Parallel()
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

// round23Hash is the hash of round 23's schema.sql, kept in testdata: it
// has no commit identity on the epoch row, which this build's schema adds
// (round 24). Unit: reads a file in the package, no store.
func TestRound23Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round23.sql")
	require.NoError(t, err)
	assert.Equal(t, round23Hash, hashSchema(string(old)))
	assert.NotEqual(t, round23Hash, schemaHash)
	assert.NotContains(t, string(old), "commit_email")
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS commit_email text")
	assert.True(t, knownOlder(round23Hash))
}

// round24Hash is the hash of v2-stitch's round 24 schema.sql, kept in
// testdata: it has no cards.usage and no sprint.balances, which round 25
// adds. Unit: reads a file in the package, no store.
func TestRound24Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round24.sql")
	require.NoError(t, err)
	assert.Equal(t, round24Hash, hashSchema(string(old)))
	assert.NotEqual(t, round24Hash, schemaHash)
	assert.NotContains(t, string(old), "usage")
	assert.NotContains(t, string(old), "sprint.balances")
	assert.Contains(t, string(old), "commit_email")
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS usage")
	assert.True(t, knownOlder(round24Hash))
}

// round25Hash is the hash of round 25's schema.sql (the deployed line's,
// the live store's on 2026-11-07), kept in testdata: it has cards.usage and
// sprint.balances and no cards.model, no heartbeat and no commit identity,
// which this build's schema adds. Unit: reads a file in the package, no
// store.
func TestRound25Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round25.sql")
	require.NoError(t, err)
	assert.Equal(t, round25Hash, hashSchema(string(old)))
	assert.NotEqual(t, round25Hash, schemaHash)
	assert.Contains(t, string(old), "ADD COLUMN IF NOT EXISTS usage")
	assert.Contains(t, string(old), "sprint.balances")
	assert.NotContains(t, string(old), "model text")
	assert.NotContains(t, string(old), "commit_email")
	assert.NotContains(t, string(old), "load1_centi")
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS model text")
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS commit_email text")
	assert.True(t, knownOlder(round25Hash))
}

// round26Hash is the hash of round 26's schema.sql, kept in testdata: it has
// the row heartbeat without cpu_peak_deci, which this build's schema adds.
// Unit: reads a file in the package, no store.
func TestRound26Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round26.sql")
	require.NoError(t, err)
	assert.Equal(t, round26Hash, hashSchema(string(old)))
	assert.NotEqual(t, round26Hash, schemaHash)
	assert.Contains(t, string(old), "load1_centi")
	assert.NotContains(t, string(old), "cpu_peak_deci")
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS cpu_peak_deci")
	assert.True(t, knownOlder(round26Hash))
}

// round27Hash is the hash of round 27's schema.sql, kept in testdata: it has
// the row heartbeat without version, which this build's schema adds.
// Unit: reads a file in the package, no store.
func TestRound27Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round27.sql")
	require.NoError(t, err)
	assert.Equal(t, round27Hash, hashSchema(string(old)))
	assert.NotEqual(t, round27Hash, schemaHash)
	assert.Contains(t, string(old), "cpu_peak_deci")
	assert.NotContains(t, string(old), "ADD COLUMN IF NOT EXISTS version")
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS version")
	assert.True(t, knownOlder(round27Hash))
}

// round28Hash is the hash of round 28's schema.sql, kept in testdata: its
// cards admit the state blocked, which this build's schema drops (round 29;
// Glenn, 2026-11-07: blocked is no state). Unit: reads a file in the
// package, no store.
func TestRound28Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round28.sql")
	require.NoError(t, err)
	assert.Equal(t, round28Hash, hashSchema(string(old)))
	assert.NotEqual(t, round28Hash, schemaHash)
	assert.Contains(t, string(old), "'fail', 'blocked', 'done'")
	assert.NotContains(t, schemaSQL, "'blocked'")
	assert.NotContains(t, CardStates, "blocked")
	assert.True(t, knownOlder(round28Hash))
}

// round29Hash is the hash of round 29's schema.sql, kept in testdata: its
// cards have no bounce_base, which this build's schema adds (round 30: an
// unhold gives a producer a fresh bounce budget). Unit: reads a file in the
// package, no store.
func TestRound29Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round29.sql")
	require.NoError(t, err)
	assert.Equal(t, round29Hash, hashSchema(string(old)))
	assert.NotEqual(t, round29Hash, schemaHash)
	assert.NotContains(t, string(old), "bounce_base")
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS bounce_base")
	assert.True(t, knownOlder(round29Hash))
}

// round30Hash is the hash of round 30's schema.sql, kept in testdata: its
// streams have no gates, which this build's schema adds (round 31: a
// stream's gates). Unit: reads a file in the package, no store.
func TestRound30Hash(t *testing.T) {
	t.Parallel()
	old, err := os.ReadFile("testdata/schema-round30.sql")
	require.NoError(t, err)
	assert.Equal(t, round30Hash, hashSchema(string(old)))
	assert.NotEqual(t, round30Hash, schemaHash)
	assert.NotContains(t, string(old), "gates")
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS gates")
	assert.True(t, knownOlder(round30Hash))
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
		drop := "DROP COLUMN IF EXISTS " + col
		if col == "tiers" {
			drop = "DROP COLUMN tiers" // only as round 17's text[]: round 23's text tiers stays
		}
		assert.Contains(t, schemaSQL, drop, "round 22 drops %s", col)
	}
}
