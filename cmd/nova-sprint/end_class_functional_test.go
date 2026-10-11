//go:build functional

package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// A coordination failure is never the workers (docs/SPEC-SPRINT.md, "A coordination
// failure is never the workers"; the owner, 2026-10-11 ~20:30Z). A failed finish whose
// report classes coordination (sprint.ClassifyEnd) never counts against the worker: it is
// excluded from the member's and the friend's ok% (never the failed cell), never consumes an
// attempt toward the brief's bound (the primary's failed count does not move), and it
// accumulates on the coordination-failure counter by cause and by the member who suffered it.
// A failed finish whose report classes worker behaves as before: it counts as the worker's
// failure. This runs the real store and the real finish step, so the two paths are told apart
// end to end, not by calling ClassifyEnd directly.
func TestACoordinationEndNeverBlamesTheWorkerOnTheStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))

	coord := newApp(func(k string) string {
		return map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator"}[k]
	})
	t.Cleanup(coord.close)
	do := func(args ...string) {
		t.Helper()
		var out, errb bytes.Buffer
		require.Equal(t, 0, coord.run(args, &out, &errb), "%v: %s %s", args, out.String(), errb.String())
	}
	do("init", "--members", "m1:4", "--readers", "reader-a,reader-b")
	do("add", "--stream", "s1", "--count", "3", "--brief", passingBrief("coord-brief"))
	do("fleet", "beat", "m1")

	seed := &store.Store{B: &store.Redis{C: admin, Now: time.Now}, Actor: "coordinator"}
	dealt, err := seed.Run(ctx, dealStep(sprint.DealReq{}))
	require.NoError(t, err)
	require.Len(t, dealt.Moved, 3)

	// one card ends ok; its ok word counts in no ok% until a reader reads it, but it is
	// never a coordination end.
	do("take", "--as", "m1", "s1-1.w1@1", "--epoch", "0")
	do("finish", "--as", "m1", "s1-1.w1@1", "--epoch", "0", "--head", "abc123", "--report", "done")

	// one card ends failed on a coordination cause: TLC owed, no jar on the bench.
	do("take", "--as", "m1", "s1-2.w1@1", "--epoch", "0")
	do("finish", "--as", "m1", "s1-2.w1@1", "--epoch", "0", "--failed", "--report", "TLC owed: no jar on this bench")

	// one card ends failed on the worker's own work: the tests are red.
	do("take", "--as", "m1", "s1-3.w1@1", "--epoch", "0")
	do("finish", "--as", "m1", "s1-3.w1@1", "--epoch", "0", "--failed", "--report", "verdict not-done; pushed=deadbeef to sprint/x: the gate is red")

	snap, err := seed.Load(ctx, store.All, nil)
	require.NoError(t, err)

	// the coordination end is excluded from the failed cell, so the member's ok% is worker-ok
	// over worker-ended attempts only.
	require.Equal(t, 0, snap.Fleet.Count("m1", sprint.DoneFailed), "a coordination end never reaches the failed cell")

	// it accumulates on the counter, by cause and by the member who suffered it.
	counts := sprint.CoordinationCounts(snap)
	require.Equal(t, 1, counts.Total)
	require.Equal(t, 1, counts.ByCause[sprint.CauseMissingTool], "by cause: %+v", counts.ByCause)
	require.Equal(t, 1, counts.ByRow["m1"], "by member: %+v", counts.ByRow)

	// the primary whose end was coordination consumed no attempt toward the bound; the
	// worker's own failed end did.
	require.Equal(t, "0", snap.Primary("s1-2").F("failed"), "a coordination end never counts toward the bound")
	require.Equal(t, "1", snap.Primary("s1-3").F("failed"), "the worker's own failed end counts one attempt")
}
