package member

import (
	"encoding/json"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The ghost reads of 2026-10-11 (v1.2.14 BLOCKER, a-member-hands-back-a-read-it-is-not-
// running): b2's log said "read schema-c-t04-versions.r8.b2: ... no verdict recorded; run
// again in 15s" at 21:55Z and nothing more until "reaped ... no longer in the queue" at
// 22:50Z, when the server's 60-minute ReadCardDeadline took it back. A member's queue sends
// no packet for a card it names in --have, so the retry waited for a packet that never came:
// the launch stayed in the running map with its result, the live set named it held, the
// server's reconcile left it working, and it held half a lane. The retry runs from the
// launch's own packet.

// queueSprint answers the member's verbs: its queue lists the cards given, working, with no
// packet (a member's --have cards), a take hands nothing, every other verb is answered.
type queueSprint struct {
	mu    sync.Mutex
	cards []queueCard
	verbs [][]string
}

func (s *queueSprint) Run(args ...string) (int, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.verbs = append(s.verbs, slices.Clone(args))
	switch args[0] {
	case "queue":
		out, _ := json.Marshal(queueOut{As: "m2", Epoch: 1, Width: 4, Cards: s.cards, Machine: "RUNNING"})
		return 0, out
	case "take":
		return 0, []byte(`{"packets":[]}`)
	}
	return 0, nil
}

// stagingChild is a child whose stage failed: it ended, no verdict.
type stagingChild struct{}

func (stagingChild) Done() bool { return true }
func (stagingChild) Result() Result {
	return Result{End: EndStaging, Staging: "the wall refused a write outside the job"}
}

// runningChild is a child that runs.
type runningChild struct{}

func (runningChild) Done() bool     { return false }
func (runningChild) Result() Result { return Result{} }

// countingRunner starts every packet as a running child and counts the starts by card.
type countingRunner struct {
	mu     sync.Mutex
	starts map[string]int
}

func (r *countingRunner) Start(p Packet) (Child, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts[p.Card]++
	return runningChild{}, nil
}

func TestAReadWhoseStageFailedIsRunAgainNotHeldAsAGhost(t *testing.T) {
	t.Parallel()
	const id = "s1-1.r1.m2"
	now := time.Date(2026, 10, 11, 21, 55, 0, 0, time.UTC)
	sp := &queueSprint{cards: []queueCard{{ID: id, Table: "fleet", Row: "m2", Col: "working", Gen: 1, Attempt: 1}}}
	run := &countingRunner{starts: map[string]int{}}
	m := New(Config{As: "m2", Width: 4, Now: func() time.Time { return now }}, sp, run, nil, io.Discard)
	m.epoch = 1
	m.running[id] = launch{gen: 1, attempt: 1, epoch: 1, child: stagingChild{},
		packet: Packet{Card: id, Kind: "read", Gen: 1, Attempt: 1, Epoch: 1}}

	_, err := m.Tick(now)
	require.NoError(t, err)
	require.Equal(t, id+"@1:held", *m.live.Load(), "the stage failure waits ReadStageRetry, its result held")

	now = now.Add(ReadStageRetry + time.Second)
	_, err = m.Tick(now)
	require.NoError(t, err)
	require.Equal(t, 1, run.starts[id], "the read is run again from the launch's own packet: the queue sends none for a card in --have")
	require.Equal(t, id+"@1", *m.live.Load(), "the live set names a running child, never a held result with no child")

	// a pass later still the beat names what runs, never a ghost
	now = now.Add(time.Minute)
	_, err = m.Tick(now)
	require.NoError(t, err)
	require.Equal(t, id+"@1", *m.live.Load())
	require.Equal(t, 1, run.starts[id])
}
