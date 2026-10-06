package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The run loop's repeat count (docs/SPEC-SPRINT.md section 8, "A repeated refusal is an
// alarm"), on the in-memory store with the sprint's clock: a friend the server cannot
// reach, skipped every tick and said once on the log, is a judgment to the coordinator by
// the third tick, and closes itself five minutes after she is reached.
func TestRunRaisesARepeatedSkipAsAJudgmentAndClosesItWhenItStops(t *testing.T) {
	t.Parallel()
	ta, root := reconcileApp(t, 1)
	homeIs(ta, t.TempDir()) // no amy-working under it
	out := runTicks(t, ta, 3)
	assert.Equal(t, 1, strings.Count(out, "FRIEND-RECONCILE SKIPPED friend=amy: "), out)
	assert.Contains(t, out, "REPEATED friend reconcile amy: ", out)
	assert.Contains(t, out, "it happened 3 times", out)
	assert.Contains(t, ta.ok("inbox"), sprint.NRepeated, "pushed to the coordinator's inbox")

	homeIs(ta, root)
	ta.mu.Lock()
	ta.now = ta.now.Add(sprint.RepeatWindow)
	ta.mu.Unlock()
	out = runTicks(t, ta, 1)
	assert.Contains(t, out, "REPEAT STOPPED", out)
	assert.Contains(t, ta.ok("inbox"), sprint.NRepeatStopped)
}

// A tick part's refusals of one reason are one occurrence: a step refused whole names
// every card it held with the one reason, and is one cause holding them up, not a cause
// a card.
func TestATickRefusingManyCardsForOneReasonIsOneCause(t *testing.T) {
	t.Parallel()
	ta, _ := reconcileApp(t, 1)
	st, _, code := ta.a.machineVerb("run", nil, &bytes.Buffer{})
	require.NotNil(t, st, "run: %d", code)
	reps := ta.a.countRepeats()
	why := "the step cannot be written, nothing was written: card s1-9: field brief is 65552 bytes, over the bound of 65536 bytes"
	res := store.TickResult{Parts: []store.PartResult{{Name: sprint.PartDrain, Result: store.Result{Refused: []sprint.Refusal{
		{Key: "s1-7", Why: why}, {Key: "s1-8", Why: why}, {Key: "s1-9", Why: why}}}}}}
	var out, errb bytes.Buffer
	for range 3 {
		observeTick(reps, res, nil, ta.a.now())
	}
	ta.a.flushRepeats(context.Background(), st, reps, &out, &errb)
	require.Empty(t, errb.String())
	assert.Equal(t, 1, strings.Count(out.String(), "REPEATED "), out.String())
	assert.Contains(t, out.String(), "REPEATED tick "+sprint.PartDrain+": "+why, out.String())
	assert.Contains(t, out.String(), "holds up s1-7, s1-8, s1-9", out.String())

	// a land batch refused round after round is its own cause, by its stream
	for range 3 {
		ta.a.observeRepeat(sprint.RepeatCause{Verb: "land refused", Subject: "stream s2", Reason: "the batch's history is not the base's"}, []string{"s2-1"})
	}
	out.Reset()
	ta.a.flushRepeats(context.Background(), st, reps, &out, &errb)
	assert.Contains(t, out.String(), "REPEATED land refused stream s2: the batch's history is not the base's", out.String())
	assert.NotContains(t, out.String(), "tick drain", "the drain's is open already: its count did not move")

	ta.mu.Lock()
	ta.now = ta.now.Add(time.Minute)
	ta.mu.Unlock()
	out.Reset()
	assert.False(t, reps.Due(ta.a.now()))
	ta.a.flushRepeats(context.Background(), st, reps, &out, &errb)
	assert.Empty(t, out.String(), "nothing due: nothing read or written")
}
