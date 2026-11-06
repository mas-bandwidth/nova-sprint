package store

import (
	"context"
	"fmt"
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
	h.ok(h.op("narrow", nil, PutRow{"w1", "worker", 1, "sess-a"}, SetCardState{"u1", "ready"}, SetCardState{"u2", "ready"}, SetCardHolder{"u1", "w1"}, SetCardHolder{"u2", "w1"}))
	for name, ops := range map[string][]Op{
		"take": {h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"}), h.op("take", []Expect{CardGen{"c1", 0}}, SetCardState{"c1", "working"})},
		"deal": {h.op("deal", nil, SetCardState{"u1", "working"}), h.op("deal", nil, SetCardState{"u2", "working"})}, // the state write into working alone is the width check
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
	card := Card{Priority: "normal", ID: "c1", Kind: "producer", Stream: "s1", State: "ready"}
	row := Row{Name: "w1", Kind: "worker", State: "down"}
	events := []Event{
		{Seq: 1, Accepted: true, After: &State{Streams: map[string]Stream{"s1": {Name: "s1"}}, Cards: map[string]Card{"c1": card}}},
		{Seq: 2, Reason: "no"},
		{Seq: 3, Accepted: true, After: &State{Policies: map[int]PolicyVersion{1: {Version: 1, Active: true}}}},
		{Seq: 4, Accepted: true, After: &State{Epoch: 2, Rows: map[string]Row{"w1": row}}},
		{Seq: 5, Reason: "no"},
	}
	want := Snapshot{Seq: 5, Epoch: 2, Policy: 1, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: map[string]Row{"w1": row}}
	assert.Equal(t, want, Rebuild(events))
	assert.Equal(t, emptySnapshot(1), Rebuild(nil))
}

func TestSchemaIsEmbedded(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"CREATE SCHEMA IF NOT EXISTS sprint", fmt.Sprintf("INSERT INTO sprint.meta (id, schema) VALUES (1, %d)", SchemaVersion), "FOREIGN KEY (epoch, holder) REFERENCES sprint.rows (epoch, name)"} {
		assert.Contains(t, schemaSQL, want)
	}
}
