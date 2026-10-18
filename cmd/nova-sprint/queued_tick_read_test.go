package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
)

// machineReads is the test store as a step sees it, counting the reads of the
// machine's state record: the read runStep makes to print the queued-tick NOTE.
type machineReads struct {
	*store.Mem
	n *machineReadCount
}

type machineReadCount struct {
	mu sync.Mutex
	n  int
}

func (b machineReads) GetKey(ctx context.Context, name string) (string, bool, error) {
	if name == "machine" {
		b.n.mu.Lock()
		b.n.n++
		b.n.mu.Unlock()
	}
	return b.Mem.GetKey(ctx, name)
}

func (b machineReads) AtEpoch(epoch uint64, old bool) store.Backend {
	return machineReads{Mem: b.Mem.AtEpoch(epoch, old).(*store.Mem), n: b.n}
}

func (c *machineReadCount) reset() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.n
	c.n = 0
	return n
}

// A step reads the machine only when it queued work-table changes: the
// queued-tick NOTE is "" for a step that queued none, so its one read was a
// store read for nothing (v17-verb-reads-the-machine-only-for-a-queued-step).
// A step that queues one still says the NOTE behind a failing tick.
func TestAStepThatQueuedNothingReadsNoMachine(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.now = time.Unix(1000, 0).UTC()
	reads := &machineReadCount{}
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		return machineReads{Mem: ta.m, n: reads}, nil
	}
	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --count 1 --one")

	// READY: the sprint line above the OK reads no machine of its own, so every
	// read counted here is the step's own queued-tick read
	reads.reset()
	out := ta.ok("stream set s1 --release v1.0.0")
	require.Contains(t, out, "STREAM-SET OK")
	require.Contains(t, out, "MOVED stream s1 release v1.0.0")
	require.Zero(t, reads.reset(), "a step that moved a merge row and queued no work-table change read no machine")

	ta.ok("start")
	failing := store.Heartbeat{Ticks: 1003, Failures: 4, Error: "tick drain: table work: 65 properties, over the bound of 64"}
	b, err := json.Marshal(failing)
	require.NoError(t, err)
	require.NoError(t, ta.m.SetKey(context.Background(), "heartbeat", string(b)))
	out = ta.ok("add --stream s1 --count 1 --one")
	require.Contains(t, out, "ADD OK")
	require.Contains(t, out, "NOTE queued:")
	require.Contains(t, out, failing.Error)
}
