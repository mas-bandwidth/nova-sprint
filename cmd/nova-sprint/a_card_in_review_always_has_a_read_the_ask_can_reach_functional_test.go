//go:build functional

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
)

// checkReport is check --json on a real store: its rule violations.
type checkReport struct {
	Violations []struct {
		Rule   int    `json:"rule"`
		Detail string `json:"detail"`
	} `json:"violations"`
}

// TestACardInReviewAlwaysHasAReadTheAskCanReachOnAStore is the store-level
// picture of the rule the sprint learned on 2026-10-12: a read that ends
// without a verdict, or is withdrawn by a hold or a return, was still asked, so
// `ask --another` must not say the card was never asked ("not asked yet") while
// `ask` says its reads are all out. The ask verb's two refusals and the
// no-stall rule's decisions decide "asked at this attempt" by one function
// (askedAtAttempt), so they never contradict each other (docs/SPEC-SPRINT.md
// section 6, the reads). The card is in review at attempt 2 with one read
// withdrawn by a hold and another handed back with no verdict: no read is out,
// so check (rule 13, the every-producer-has-its-consumer-out rule) reports the
// gap before the tick, and the next tick's ask (TickAsk) asks a fresh read of
// the attempt. It runs on a private store (explicit --redis, a scrubbed env
// whose HOME and XDG_CONFIG_HOME are the test's own, no NOVA_* variable from
// the machine).
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

	run("init", "--readers", "reader-a,reader-b", "--members", "m1:8")
	run("set", "--reads", "2")
	dir := t.TempDir()
	brief := filepath.Join(dir, "brief.md")
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief("s1-1: a flash card (s1) tier: flash\nTHE TASK.\nwork\n")), 0o600))
	run("add", "--stream", "s1", "s1-1", "--one", "--brief-file", brief)
	run("fleet", "beat", "m1")
	run("start")
	run("tick") // the fleet update, last in the tick, brings m1 up
	run("tick") // the pump deals to it
	run("take", "--as", "m1", "--max", "1")
	run("finish", "--as", "m1", "s1-1.w1@1", "--epoch", "0", "--report", "done")
	run("tick") // the primary moves to review and its reads are asked

	// The two reads the tick asked at attempt 1: withdraw one by a hold and end
	// the other without a verdict, so no read of the attempt is out and both
	// are history on the readers table.
	var card cardView
	require.NoError(t, json.Unmarshal([]byte(run("card", "s1-1", "--json")), &card))
	var heldRead, returnedRead string
	for _, r := range card.Reads {
		if r.Col == sprint.Asked || r.Col == sprint.Reading {
			if heldRead == "" {
				heldRead = r.ID
			} else if returnedRead == "" {
				returnedRead = r.ID
			}
		}
	}
	require.NotEmpty(t, heldRead, "a read is out to withdraw")
	require.NotEmpty(t, returnedRead, "a read is out to return")
	run("read", "--as", "reader-b", "--begin", returnedRead, "--epoch", "0")
	run("read", "--as", "reader-b", "--return", returnedRead, "--reason", "'no verdict'", "--epoch", "0")
	run("hold", "reader-a", "--reason", "'the friend withdrew it'", "--return")

	check := func() checkReport {
		t.Helper()
		var rep checkReport
		require.NoError(t, json.Unmarshal([]byte(run("check", "--json")), &rep))
		return rep
	}
	violatesS1 := func(rep checkReport) bool {
		for _, v := range rep.Violations {
			if v.Rule == 13 && strings.Contains(v.Detail, "s1-1") {
				return true
			}
		}
		return false
	}

	// The check reports the gap before the tick: a card in review with no read
	// out is a producer whose consumer is missing.
	require.True(t, violatesS1(check()), "check reports the gap before the tick")

	// ask and ask --another agree: a read was asked at this attempt, so neither
	// refusal is given; both reach a reader.
	_, code, errb := ta.runVerbOut("ask", "s1-1", "--another")
	require.Zero(t, code, "ask --another: exit %d, %s", code, errb)
	require.NotContains(t, errb, "not asked yet", "ask --another must not say it was never asked")
	_, code, errb = ta.runVerbOut("ask", "s1-1")
	require.Zero(t, code, "ask: exit %d, %s", code, errb)
	require.NotContains(t, errb, "asked already: its reads are out", "ask must reach a reader")

	// The next tick's ask re-asks the attempt: a read card goes out again, and
	// the gap the check named is closed.
	run("tick")
	require.False(t, violatesS1(check()), "the read the tick asked closes the check's gap")
}
