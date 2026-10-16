package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
)

// warnings is the waiting-judgment warnings among the messages to the seat.
func warnings(ms []bus.Message) []bus.Message {
	var out []bus.Message
	for _, m := range ms {
		if m.Subject == "nova-sprint: judgments waiting on the coordinator" {
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
// more often than once a minute for the same set over ten minutes (the server's look, on
// the bus to the coordinator's name: coordinator_push.go).
func TestTheWaitingJudgmentWarningRepeatsAtABoundedRate(t *testing.T) {
	t.Parallel()
	const name = "hook-ready"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)

	// READY: the sprint as init and a clear leave it, with moves due
	ta.lined(func() { ta.ok("clear --confirm sprint") })
	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	var where string
	ta.lined(func() { where = ta.ok("where") })
	assert.Contains(t, where, "READY", "where says READY after a clear")
	assert.NotContains(t, where, "STOPPED")
	var pushed []bus.Message
	for range 600 {
		ta.step(time.Second)
		pushed = append(pushed, ta.pass(name)...)
	}
	assert.Empty(t, pushed, "READY with moves due: no judgment and no warning over ten minutes")

	var code int
	var errs string
	ta.lined(func() { code, _, errs = ta.do("stop --reason 'a test' --until 1h") })
	assert.NotEqual(t, 0, code, "stop on READY is refused")
	assert.Contains(t, errs, "the machine is READY: it has not started since the clear; nothing to stop")

	// STOPPED with moves due: the judgment, the warning at 10 s, then at most once every 30 minutes
	ta.lined(func() { ta.ok("start") })
	ta.step(time.Second)
	ta.lined(func() { ta.ok("stop --reason 'a test' --until 1h") })
	ta.lined(func() { where = ta.ok("where") })
	assert.Contains(t, where, "STOPPED")
	first := ta.pass(name)
	require.Len(t, first, 1, "%+v", first)
	require.Contains(t, first[0].Body, "machine:stopped")
	var at []time.Duration
	for i := 1; i <= 600; i++ {
		ta.step(time.Second)
		ws := warnings(ta.pass(name))
		require.LessOrEqual(t, len(ws), 1, "one warning a look at most")
		if len(ws) == 1 {
			at = append(at, time.Duration(i)*time.Second)
		}
	}
	require.NotEmpty(t, at)
	assert.Equal(t, sprint.JudgmentWarnAfter, at[0], "the first warning at 10 s, as before")
	for k := 1; k < len(at); k++ {
		assert.GreaterOrEqual(t, at[k]-at[k-1], sprint.JudgmentWarnEvery, "warnings %d and %d: at most once every 30 minutes", k-1, k)
	}
	assert.Len(t, at, 1, "ten minutes: only the first warning at 10 s: %v", at)
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
