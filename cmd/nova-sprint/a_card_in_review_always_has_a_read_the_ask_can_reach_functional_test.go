//go:build functional

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
)

// TestACardInReviewAlwaysHasAReadTheAskCanReachOnAStore is the store-level
// picture of the rule the sprint learned on 2026-10-12: a read that ends
// without a verdict, or is withdrawn by a hold or a return, was still asked, so
// `ask --another` must not say the card was never asked ("not asked yet") while
// `ask` says its reads are all out. The ask verb's two refusals and the
// no-stall rule's decisions decide "asked at this attempt" by one function
// (askedAtAttempt), so they never contradict each other (docs/SPEC-SPRINT.md
// section 6, the reads).
//
// The card is in review at attempt 2 with its attempt-1 read withdrawn by a
// hold and its attempt-2 read handed back with no verdict: no read of the
// attempt is out, so `ask` and `ask --another` agree (neither refusal is given)
// and the next tick's ask (TickAsk) re-asks the attempt of another reader,
// closing the gap. The gap itself is the consumers-out postcondition the tick
// reads before it asks (ConsumersOut/CheckConsumers, the tick's TickCheck), and
// TestACardInReviewAlwaysHasAReadTheAskCanReach (internal/sprint) asserts it
// directly on the same state. It runs on a private store (explicit --redis, a
// scrubbed env whose HOME and XDG_CONFIG_HOME are the test's own, no NOVA_*
// variable from the machine).
func TestACardInReviewAlwaysHasAReadTheAskCanReachOnAStore(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	ctx := context.Background()
	require.NoError(t, fn.Load(ctx, c))
	ta := newFunctionalTestApp(t, addr)
	run := func(args ...string) string {
		t.Helper()
		return ta.runVerb(args...)
	}
	// live reads are the attempt's reads really out: not handed back and not
	// withdrawn from a reader down (the same alive definition the ask and the
	// no-stall rule use, liveReadsAt).
	live := func() []*sprint.Card {
		t.Helper()
		var card cardView
		require.NoError(t, json.Unmarshal([]byte(run("card", "s1-1", "--json")), &card))
		var out []*sprint.Card
		for _, rc := range card.Reads {
			if (rc.Col == sprint.Asked || rc.Col == sprint.Reading) && rc.F(sprint.FieldReturned) == "" {
				out = append(out, rc)
			}
		}
		return out
	}
	regap := func() {
		for _, rc := range live() {
			run("read", "--as", rc.F("reader"), "--return", rc.ID, "--reason", "'no verdict'", "--epoch", "0")
		}
	}

	run("init", "--readers", "reader-a,reader-b,reader-c", "--members", "m1:8")
	run("set", "--read-cards", "off") // this regression returns and reasks legacy Readers records
	dir := t.TempDir()
	brief := filepath.Join(dir, "brief.md")
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief("s1-1: a flash card (s1) tier: flash\nTHE TASK.\nwork\n")), 0o600))
	run("add", "--stream", "s1", "s1-1", "--one", "--brief-file", brief)
	run("fleet", "beat", "m1")
	run("start")
	run("tick") // the fleet update, last in the tick, brings m1 up
	run("tick") // the pump deals to it
	require.Contains(t, run("card", "s1-1"), "working", "s1-1 must be dealt before the take")
	run("take", "--as", "m1", "--max", "1")
	run("finish", "--as", "m1", "s1-1.w1@1", "--epoch", "0", "--report", "done")
	run("queue", "--as", "reader-a") // the readers ask for their queues: they are up
	run("queue", "--as", "reader-b")
	run("queue", "--as", "reader-c")
	run("tick") // the primary moves to review and its read is asked

	// The attempt-1 read is withdrawn by a hold of its reader, which leaves the
	// next attempt's read to another reader (the bug's first move).
	require.Len(t, live(), 1, "one read of attempt 1")
	run("hold", live()[0].F("reader"), "--reason", "'the friend withdrew it'", "--return")

	// rework: the card's next attempt starts fresh, at attempt 2
	run("rework", "s1-1", "--fix", "'answer the finding'")
	run("tick") // the pump deals the next attempt
	run("take", "--as", "m1", "--max", "1")
	run("finish", "--as", "m1", "s1-1.w2@1", "--epoch", "0", "--report", "done")
	run("tick") // attempt 2 moves to review and its read is asked

	// The attempt-2 read ends without a verdict: no read of the attempt is out,
	// and both reads are history on the readers table.
	require.Len(t, live(), 1, "one read of attempt 2")
	run("read", "--as", live()[0].F("reader"), "--return", live()[0].ID, "--reason", "'no verdict'", "--epoch", "0")

	// ask and ask --another agree: a read was asked at this attempt, so neither
	// refusal is given; both reach a reader.
	_, code, errb := ta.runVerbOut("ask", "s1-1", "--another")
	require.Zero(t, code, "ask --another: exit %d, %s", code, errb)
	require.NotContains(t, errb, "not asked yet", "ask --another must not say it was never asked")
	regap()
	_, code, errb = ta.runVerbOut("ask", "s1-1")
	require.Zero(t, code, "ask: exit %d, %s", code, errb)
	require.NotContains(t, errb, "asked already: its reads are out", "ask must reach a reader")
	regap()

	// The next tick's ask re-asks the attempt: a read card goes out again.
	run("tick")
	require.NotEmpty(t, live(), "the tick asked a fresh read of attempt 2")
}
