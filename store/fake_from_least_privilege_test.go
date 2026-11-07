package store

// NewFakeFrom, a Fake whose state is a snapshot (a dry-run seam), and Migrate
// by a schema owner with no CREATE on the database.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFromSnapshot is the conformance scenario: a Fake made from the store's
// snapshot (epoch 2, policy versions, judgements, cursors) reads back the
// same snapshot, has no events before it, and gives the same receipts,
// events and snapshot as the store for the same ops, accepted and refused.
func fakeFromSnapshot(h *harness) {
	h.ok(h.op("pol", nil, PutPolicy{"data", "p", "{}"}, PutPolicy{"script", "deal", "def deal(): pass"}, SetActivePolicy{1}))
	h.clear()
	h.seed()
	h.ok(h.op("open", nil, PutJudgement{ID: "j1", Kind: "red", About: "c1"}, SetCursor{"feed", 1}, SetCardReason{"c1", "r"}))
	snap := h.snap()
	var versions []PolicyVersion
	for v := 1; ; v++ {
		p, err := h.s.Policy(h.ctx, v)
		if errors.Is(err, ErrNotFound) {
			break
		}
		require.NoError(h.t, err)
		versions = append(versions, p)
	}
	f, err := NewFakeFrom(snap, versions...)
	require.NoError(h.t, err)
	got, err := f.Snapshot(h.ctx)
	require.NoError(h.t, err)
	assert.Equal(h.t, snap, got, "the Fake's snapshot is the input")
	ev, err := f.Events(h.ctx, 0, 0)
	require.NoError(h.t, err)
	assert.Empty(h.t, ev, "the log before the snapshot is not carried")
	for _, o := range []Op{
		h.op("take", []Expect{CardGen{"c1", snap.Cards["c1"].Gen}}, SetCardState{"u1", "working"}),
		h.op("stale", []Expect{CardGen{"c1", 0}}, SetCardReason{"c1", "x"}),
		h.op("pol", nil, PutPolicy{"data", "q", "{}"}, SetActivePolicy{2}),
		h.op("answer", nil, AnswerJudgement{"j1", "yes", "coord"}, SetCursor{"feed", snap.Seq}),
	} {
		want, err := h.s.Apply(h.ctx, o)
		require.NoError(h.t, err)
		r, err := f.Apply(h.ctx, o)
		require.NoError(h.t, err)
		assert.Equal(h.t, want, r, o.ID)
	}
	want, err := h.s.Events(h.ctx, snap.Seq, 0)
	require.NoError(h.t, err)
	ev, err = f.Events(h.ctx, snap.Seq, 0)
	require.NoError(h.t, err)
	require.Len(h.t, ev, 4)
	assert.Equal(h.t, snap.Seq+1, ev[0].Seq, "events after it start at the snapshot's seq")
	assert.Equal(h.t, want, ev)
	got, err = f.Snapshot(h.ctx)
	require.NoError(h.t, err)
	assert.Equal(h.t, h.snap(), got)
	p, err := f.Policy(h.ctx, 3)
	require.NoError(h.t, err)
	q, err := h.s.Policy(h.ctx, 3)
	require.NoError(h.t, err)
	assert.Equal(h.t, q, p)
}

func TestNewFakeFromEmptyAndCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := NewFake().Snapshot(ctx)
	require.NoError(t, err)
	f, err := NewFakeFrom(s)
	require.NoError(t, err)
	got, err := f.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, s, got)
	in := emptySnapshot(3)
	in.Seq = 9
	in.Cards["c1"] = Card{ID: "c1", Needs: []string{"c0"}}
	f, err = NewFakeFrom(in)
	require.NoError(t, err)
	in.Cards["c1"].Needs[0] = "zz"
	got, err = f.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"c0"}, got.Cards["c1"].Needs, "editing the input never changes the Fake")
	assert.Equal(t, int64(9), got.Seq)
}

func TestNewFakeFromInvalid(t *testing.T) {
	t.Parallel()
	bad := func(edit func(*Snapshot)) Snapshot {
		s := emptySnapshot(1)
		edit(&s)
		return s
	}
	for name, c := range map[string]struct {
		s   Snapshot
		ps  []PolicyVersion
		msg string
	}{
		"epoch 0":       {bad(func(s *Snapshot) { s.Epoch = 0 }), nil, "store: invalid op: a snapshot at epoch 0 and seq 0 (epochs start at 1, seqs at 0)"},
		"negative seq":  {bad(func(s *Snapshot) { s.Seq = -1 }), nil, "store: invalid op: a snapshot at epoch 1 and seq -1 (epochs start at 1, seqs at 0)"},
		"card key":      {bad(func(s *Snapshot) { s.Cards["c1"] = Card{ID: "c2"} }), nil, `store: invalid op: the snapshot's card "c2" is filed under "c1"`},
		"row key":       {bad(func(s *Snapshot) { s.Rows["w1"] = Row{Name: "w2"} }), nil, `store: invalid op: the snapshot's row "w2" is filed under "w1"`},
		"active absent": {bad(func(s *Snapshot) { s.Policy = 2 }), []PolicyVersion{{Version: 1}}, "store: invalid op: the snapshot's active policy version 2 is not among the versions given"},
		"twice":         {emptySnapshot(1), []PolicyVersion{{Version: 1}, {Version: 1}}, "store: invalid op: policy version 1 is below 1 or given twice"},
		"version 0":     {emptySnapshot(1), []PolicyVersion{{Version: 0}}, "store: invalid op: policy version 0 is below 1 or given twice"},
	} {
		t.Run(name, func(t *testing.T) {
			f, err := NewFakeFrom(c.s, c.ps...)
			require.ErrorIs(t, err, ErrInvalid)
			assert.Equal(t, c.msg, err.Error())
			assert.Nil(t, f)
		})
	}
}

// The statement Migrate skips on an existing schema is in schema.sql once,
// and is the only CREATE SCHEMA in it. The row heartbeat's columns are in
// schema.sql, so the hash covers them.
func TestSchemaObjects(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1, strings.Count(schemaSQL, createSchema))
	assert.NotContains(t, schemaObjects, "CREATE SCHEMA")
	assert.Equal(t, len(schemaSQL)-len(createSchema), len(schemaObjects))
	assert.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS load1_centi integer")
}

// The least-privilege setup: an admin role with CREATE on the database makes
// schema sprint owned by nova_sprint, which has no CREATE on the database.
// CREATE SCHEMA IF NOT EXISTS is refused for nova_sprint (the bug), and
// Migrate as nova_sprint succeeds, twice, with the store its own.
func TestFunctionalMigrateSchemaOwnerWithoutCreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := startPG(t)
	for _, q := range []string{"CREATE ROLE nova_sprint LOGIN", "CREATE ROLE nova_admin LOGIN", "GRANT nova_sprint TO nova_admin", "CREATE DATABASE s19"} {
		_, err := connect(t, dsn).Exec(ctx, q)
		require.NoError(t, err, q)
	}
	db := strings.Replace(dsn, "/postgres?", "/s19?", 1)
	for _, q := range []string{"REVOKE CREATE ON DATABASE s19 FROM PUBLIC", "GRANT CREATE ON DATABASE s19 TO nova_admin"} {
		_, err := connect(t, db).Exec(ctx, q)
		require.NoError(t, err, q)
	}
	_, err := connect(t, strings.Replace(db, "nova@", "nova_admin@", 1)).Exec(ctx, "CREATE SCHEMA sprint AUTHORIZATION nova_sprint")
	require.NoError(t, err)
	as := strings.Replace(db, "nova@", "nova_sprint@", 1)
	_, err = connect(t, as).Exec(ctx, createSchema)
	require.ErrorContains(t, err, "SQLSTATE 42501", "the role may not run CREATE SCHEMA IF NOT EXISTS on its own schema")
	p, err := Open(ctx, as, "")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	require.NoError(t, p.Migrate(ctx))
	require.NoError(t, p.Migrate(ctx), "a second Migrate is a no-op")
	id, err := CheckSchema(ctx, as)
	require.NoError(t, err)
	assert.Equal(t, SchemaIdentity{Present: true, Hash: schemaHash, Expected: schemaHash, Current: true, Owner: "nova_sprint", Generation: schemaGeneration}, id)
	h := &harness{t: t, s: p, ctx: ctx, epoch: 1}
	h.seed()
	assert.Len(t, h.snap().Cards, 6, "the store works for its owner")
}
