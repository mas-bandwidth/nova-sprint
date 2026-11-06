package store

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unit tests: Fake only. No real time, no sockets, no network, no outside services.

func TestConformanceFake(t *testing.T) {
	t.Parallel()
	RunConformance(t, func(*testing.T) Store { return NewFake() })
}

func TestFakeDropNotLanded(t *testing.T) {
	t.Parallel()
	h := &harness{t: t, s: NewFake(), ctx: context.Background()}
	h.seed()
	f := h.s.(*Fake)
	o := h.op("take", []Expect{CardIs{"c1", "ready", 0}}, SetCardState{"c1", "working"})
	f.Drop(o.ID, false)
	_, err := f.Apply(h.ctx, o)
	require.ErrorIs(t, err, ErrUnknown)
	_, ok, err := f.Replay(h.ctx, o.ID)
	require.NoError(t, err)
	assert.False(t, ok, "it did not land")
	assert.Equal(t, "ready", h.snap().Cards["c1"].State)
	r := h.ok(o)
	assert.Equal(t, 1, r.After.Cards["c1"].Gen, "applying again with the same id lands it once")
}

func TestFakeDropLanded(t *testing.T) {
	t.Parallel()
	h := &harness{t: t, s: NewFake(), ctx: context.Background()}
	h.seed()
	f := h.s.(*Fake)
	o := h.op("take", nil, SetCardState{"c1", "working"})
	f.Drop(o.ID, true)
	_, err := f.Apply(h.ctx, o)
	require.ErrorIs(t, err, ErrUnknown)
	r, ok, err := f.Replay(h.ctx, o.ID)
	require.NoError(t, err)
	require.True(t, ok, "it landed")
	assert.True(t, r.Accepted)
	again := h.ok(o)
	assert.Equal(t, r, again)
	assert.Equal(t, 1, h.snap().Cards["c1"].Gen, "the gen moved once")

	c := h.op("clear", nil)
	f.Drop(c.ID, true)
	_, err = f.Clear(h.ctx, c)
	require.ErrorIs(t, err, ErrUnknown)
	n, err := f.Clear(h.ctx, c)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	d := h.op("clear", nil)
	f.Drop(d.ID, false)
	_, err = f.Clear(h.ctx, d)
	require.ErrorIs(t, err, ErrUnknown)
	assert.Equal(t, 1, h.snap().Epoch, "a dropped clear that did not land changed nothing")
}

// The fake serializes concurrent applies the way PG does: one of two takes on
// the same expectation wins, the other is refused with the gen named.
func TestFakeConcurrentTakes(t *testing.T) {
	t.Parallel()
	h := &harness{t: t, s: NewFake(), ctx: context.Background()}
	h.seed()
	ops := []Op{
		h.op("take", []Expect{CardIs{"c1", "ready", 0}}, SetCardState{"c1", "working"}, SetCardHolder{"c1", "w1"}),
		h.op("take", []Expect{CardIs{"c1", "ready", 0}}, SetCardState{"c1", "working"}, SetCardHolder{"c1", "w1"}),
	}
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
	assert.NotEqual(t, got[0].Accepted, got[1].Accepted, "exactly one is accepted")
	for _, r := range got {
		if !r.Accepted {
			assert.Equal(t, "card c1 is at gen 1, expected gen 0", r.Reason)
		}
	}
}

func TestRebuildSkipsRefusedAndClears(t *testing.T) {
	t.Parallel()
	card := Card{ID: "c1", Stream: "s1", State: "ready"}
	events := []Event{
		{Accepted: true, After: &State{Streams: map[string]Stream{"s1": {Name: "s1"}}, Cards: map[string]Card{"c1": card}}},
		{Accepted: false, Reason: "no"},
		{Accepted: true, After: &State{Policies: map[int]Policy{1: {Version: 1, Active: true}}}},
		{Accepted: true, After: &State{Epoch: 1}},
		{Accepted: true, After: &State{Rows: map[string]Row{"w1": {Name: "w1"}}}},
	}
	assert.Equal(t, Snapshot{Epoch: 1, Policy: 1, Streams: map[string]Stream{}, Cards: map[string]Card{}, Rows: map[string]Row{"w1": {Name: "w1"}}}, Rebuild(events))
	assert.Equal(t, emptySnapshot(0), Rebuild(nil))
}

func TestSchemaIsEmbedded(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"CREATE SCHEMA IF NOT EXISTS sprint", "op       text        NOT NULL UNIQUE", "FOREIGN KEY (epoch, holder) REFERENCES sprint.rows (epoch, name)"} {
		assert.Contains(t, Schema(), want)
	}
}
