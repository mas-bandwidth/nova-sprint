package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// lookAndCollect runs one look of the session, then a barrier over the hook (a line the
// server refuses, answered in order after every line the look wrote), and returns the
// pushes the look wrote. Nothing waits on a clock: the barrier's answer is the end.
func lookAndCollect(t *testing.T, s *hookSession, c *hookClient) []hookMsg {
	t.Helper()
	s.look(context.Background())
	return barrier(t, c)
}

// barrier writes one line the server refuses and reads the hook up to its answer,
// returning the pushes before it.
func barrier(t *testing.T, c *hookClient) []hookMsg {
	t.Helper()
	c.n++
	req := "barrier-" + string(rune('a'+c.n%26)) + strings.Repeat("x", c.n/26)
	b, err := json.Marshal(hookMsg{T: "barrier", Req: req})
	require.NoError(t, err)
	_, err = c.conn.Write(append(b, '\n'))
	require.NoError(t, err)
	// the server answers every line in order, so the barrier's answer always comes after
	// what the look wrote (the test's own -timeout is the only bound; no clock here)
	var pushes []hookMsg
	for m := range c.lines {
		if m.Req == req {
			return pushes
		}
		if m.T == "push" {
			pushes = append(pushes, m)
		}
	}
	t.Fatalf("the hook closed before the barrier's answer %s", req)
	return nil
}

// keepalive is the hook client's keepalive, held to the server's read by a barrier, so
// the silent bound (sprint.HookSilentBound) never lapses the hook over simulated hours.
func keepalive(t *testing.T, c *hookClient) {
	t.Helper()
	b, err := json.Marshal(hookMsg{T: "alive"})
	require.NoError(t, err)
	_, err = c.conn.Write(append(b, '\n'))
	require.NoError(t, err)
	require.Empty(t, barrier(t, c), "a keepalive is answered by nothing")
}

func warnings(ms []hookMsg) []hookMsg {
	var out []hookMsg
	for _, m := range ms {
		if m.What == hookWarnWhat {
			out = append(out, m)
		}
	}
	return out
}

// The waiting-judgment warning repeats at a bounded rate, and a READY machine raises
// nothing (docs/SPEC-SPRINT.md section 14, "READY", and "The waiting-judgment warning"):
// on a fake clock, a cleared sprint with moves due is READY, `where` says READY, and ten
// simulated minutes of looks push nothing; `stop` on it is refused with the reason; a
// STOPPED machine with moves due pushes its judgment, then the warning at 10 s, then no
// more often than once a minute for the same set over ten minutes, an ack of a warning
// holding until the minute passes.
func TestTheWaitingJudgmentWarningRepeatsAtABoundedRate(t *testing.T) {
	t.Parallel()
	const name = "hook-ready"
	ta, _ := hookSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	ta.busProven(name)
	s := ta.hookSession()
	require.NotNil(t, s)

	// READY: the sprint as init and a clear leave it, with moves due
	ta.lined(func() { ta.ok("clear --confirm sprint") })
	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	var where string
	ta.lined(func() { where = ta.ok("where") })
	assert.Contains(t, where, "READY", "where says READY after a clear")
	assert.NotContains(t, where, "STOPPED")
	var pushed []hookMsg
	for i := range 600 {
		ta.step(time.Second)
		if i%60 == 0 {
			keepalive(t, c)
		}
		pushed = append(pushed, lookAndCollect(t, s, c)...)
	}
	assert.Empty(t, pushed, "READY with moves due: no judgment and no warning over ten minutes")

	var code int
	var errs string
	ta.lined(func() { code, _, errs = ta.do("stop --reason 'a test' --until 1h") })
	assert.NotEqual(t, 0, code, "stop on READY is refused")
	assert.Contains(t, errs, "the machine is READY: it has not started since the clear; nothing to stop")

	// STOPPED with moves due: the judgment, the warning at 10 s, then at most once a minute
	ta.lined(func() { ta.ok("start") })
	ta.step(time.Second)
	ta.lined(func() { ta.ok("stop --reason 'a test' --until 1h") })
	ta.lined(func() { where = ta.ok("where") })
	assert.Contains(t, where, "STOPPED")
	first := lookAndCollect(t, s, c)
	require.Len(t, first, 1, "%+v", first)
	require.Equal(t, "machine:stopped", first[0].What)
	var at []time.Duration
	var acked bool
	for i := 1; i <= 600; i++ {
		ta.step(time.Second)
		if i%60 == 0 {
			keepalive(t, c)
		}
		ws := warnings(lookAndCollect(t, s, c))
		require.LessOrEqual(t, len(ws), 1, "one warning a look at most")
		if len(ws) == 1 {
			at = append(at, time.Duration(i)*time.Second)
			if !acked {
				// an ack of the warning holds until the minute passes (it never silences it)
				require.Equal(t, "ok", c.say(hookMsg{T: "ack", ID: ws[0].ID}).T)
				acked = true
			}
		}
	}
	require.NotEmpty(t, at)
	assert.Equal(t, sprint.JudgmentWarnAfter, at[0], "the first warning at 10 s, as before")
	for k := 1; k < len(at); k++ {
		assert.GreaterOrEqual(t, at[k]-at[k-1], sprint.JudgmentWarnEvery, "warnings %d and %d: at most once a minute", k-1, k)
	}
	assert.Len(t, at, 10, "ten minutes: at 10 s, then once a minute: %v", at)
}

// A stop of a READY machine that holds a live owner lease (a take before the first start)
// is not refused: it captures the lease as the stop's debt, the machine stays READY, and
// the verb's line says so rather than STOPPED (docs/SPEC-SPRINT.md section 14, "READY").
func TestAStopOfAReadyMachineWithALiveLeaseCapturesItAndStaysReady(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	ta.ok("take --as m1 --limit 1")
	out := ta.ok("stop --reason r --until 9999h")
	assert.Contains(t, out, "STOP OK before=READY after=READY captured: the machine stays READY (not started since the clear); 1 live lease(s) captured as the stop's debt", out)
	assert.NotContains(t, out, "after=STOPPED", out)
	assert.Contains(t, ta.ok("where"), "\n\nREADY", "the machine reads READY after the capture")
}
