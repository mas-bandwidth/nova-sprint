package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestQueueIsReadOnlyUnlessBeatIsAsked pins the read-only queue: `queue --as
// <reader>` lists the reader's cards and writes nothing — the store's reader
// record is byte-identical after it — while `queue --as <reader> --beat` writes
// a fresh heartbeat (docs/SPEC-SPRINT.md section 6, the reader presence rule: a
// reader says it is there by beating; its queue is no longer the beat).
func TestQueueIsReadOnlyUnlessBeatIsAsked(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.quiet = map[string]bool{"r1": true} // the harness does not beat r1 for us
	ta.ok("init --readers r1 --members m1")

	ctx := context.Background()
	const beatKey = "reader-beat:r1"
	get := func() string {
		t.Helper()
		raw, ok, err := ta.m.GetKey(ctx, beatKey)
		require.NoError(t, err)
		require.True(t, ok)
		return raw
	}

	// the beat is --beat's alone: it writes the heartbeat at the clock
	ta.ok("queue --as r1 --beat")
	before := get()

	// the clock moves; a plain queue reads only and writes nothing with it
	ta.a.sleep(10 * time.Second)
	ta.ok("queue --as r1")
	require.Equal(t, before, get(), "queue --as r1 reads only: the reader record is byte-identical")

	// a queue with --beat writes a fresh heartbeat at the new clock
	ta.ok("queue --as r1 --beat")
	fresh := get()
	require.NotEqual(t, before, fresh, "queue --as r1 --beat writes a fresh heartbeat")
	var b sprint.Beat
	require.NoError(t, json.Unmarshal([]byte(fresh), &b))
	require.Equal(t, ta.a.now().UTC().Truncate(time.Second), b.At, "the heartbeat is fresh at the store's clock")
}
