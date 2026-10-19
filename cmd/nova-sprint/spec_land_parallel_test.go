package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The standard's R-LAND-PARALLEL (standard/sprint/s6.md, NS-6.1; the owner, 2026-10-18: "We've
// already talked about parallel merges for a month. You keep forgetting."). This test pins the
// part the land pass holds today: each stream queued in a pass is a batch of its own, on its own
// fork, holding only its own stream's cards on one base. The rest of the row is a divergence: the
// land loop runs one pass at a time, a lane's cuts chain, a base commit's gate has one writer,
// a built batch waits landGrace for another stream's gate, the width is capped at
// landParallelDefault, --land-parallel 1 is accepted, and one stream's batches run one after
// another, its second base waiting on its first (standard/pins/s6.tsv, every divergence
// owed by sprint-spec-fix-land-parallel).

// TestEachStreamLandsAsItsOwnBatchOnItsOwnFork: two streams queued in one pass are two batches,
// each holding only its own stream's cards on its own fork, and a card on another base starts a
// batch of its own. It fails if two streams are folded into one batch or share one fork.
func TestEachStreamLandsAsItsOwnBatchOnItsOwnFork(t *testing.T) {
	t.Parallel()
	l := &lander{parallel: landParallelDefault}
	queues := map[string][]landCard{
		"a": {{id: "a-1", repo: "mas-bandwidth/r", base: "main"}, {id: "a-2", repo: "mas-bandwidth/r", base: "main"}, {id: "a-3", repo: "mas-bandwidth/r", base: "dev"}},
		"b": {{id: "b-1", repo: "mas-bandwidth/r", base: "main"}},
	}
	ja, jb := nextBatch(l, queues, "a"), nextBatch(l, queues, "b")
	require.NotNil(t, ja)
	require.NotNil(t, jb)
	require.Equal(t, []string{"a-1", "a-2"}, ja.ids, "stream a's batch is its own run on its base, never the card of another base")
	require.Equal(t, []string{"b-1"}, jb.ids, "stream b's batch is its own, though it lands on the same base")
	require.Equal(t, "a", ja.b.Stream)
	require.Equal(t, "b", jb.b.Stream)
	require.NotSame(t, ja.f, jb.f, "each stream merges on its own lander fork")
	require.NotSame(t, l, ja.f, "a stream's fork is not the pass's lander")
	require.Equal(t, 1, batchOf(queues["a"][2:]), "a card on another base starts a batch of its own")
}
