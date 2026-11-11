package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// eventCounter is a store that counts the events read through it.
type eventCounter struct {
	store.Store
	read atomic.Int64
}

func (c *eventCounter) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	events, err := c.Store.Events(ctx, after, max)
	c.read.Add(int64(len(events)))
	return events, err
}

// up's reads of its own records on a live store (a long event log): the
// continuation write and its validation, and a setup answer saved and read
// back, read no event of the log. A live sprint's log is 96,000 events and
// grows with the fleet; reading it whole timed the continuation out
// (writeContinuationChild's 10 seconds). The bound is in rows read, not
// time: 200,000 events seeded by one generate_series, and zero read.
func TestFunctionalContinuationReadsNoEvents(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("initdb"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("initdb required")
		}
		t.Skip("initdb required")
	}
	ctx := context.Background()
	dsn := e2ePG(t)
	pg, err := store.Open(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err = conn.Exec(ctx, `INSERT INTO sprint.events (seq, op, actor, kind, accepted, receipt, at, epoch)
		SELECT g, 'seed-' || g, 'seed', 'seed', true, '{}'::jsonb, timestamptz '2026-01-01 00:00:00+00', 1 FROM generate_series(1, 200000) g`); err != nil {
		t.Fatal(err)
	}
	st := &eventCounter{Store: pg}
	in := verbs.SetupInput{Actor: "alice", Dash: "tailnet", Services: "none"}
	digest, err := handoffDigest(in)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("b", 64)
	request, _ := json.Marshal(handoffEnvelope{ID: id, Actor: in.Actor, Digest: digest})
	if written, err := writeSetupContinuation(ctx, st, bytes.NewReader(request), time.Unix(1000, 0)); err != nil || written != id {
		t.Fatal(written, err)
	}
	if err = validateHandoffRecord(ctx, st, id, in.Actor, digest); err != nil {
		t.Fatal(err)
	}
	choices := verbs.Choices{Store: st, Actor: "alice", Now: func() time.Time { return time.Unix(1000, 0) }}
	if err = choices.Save(ctx, "dashboard", "tailnet"); err != nil {
		t.Fatal(err)
	}
	if value, found, err := choices.Get(ctx, "dashboard"); err != nil || !found || value != "tailnet" {
		t.Fatal(value, found, err)
	}
	if n := st.read.Load(); n != 0 {
		t.Fatalf("up's own records read %d events of the log, want 0", n)
	}
}
