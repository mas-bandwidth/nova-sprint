package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStopByHandSaysWhoWhyAndWhenEverywhere pins stop's --reason and --until
// at the command (docs/SPEC-SPRINT.md section 14): a stop missing --reason is
// refused, where's header and its JSON (the dashboard's machine line) say
// "STOPPED, expected back by <time> (<reason>); start ends it"; the time does
// not restart it, and an explicit start does.
func TestStopByHandSaysWhoWhyAndWhenEverywhere(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1 --readers reader-a,reader-b")
	ta.ok("start")

	code, out, errs := ta.do("stop")
	assert.Equal(t, 2, code, "a bare stop: %s%s", out, errs)
	assert.Contains(t, errs, "--reason <text>")
	assert.NotContains(t, errs, "--until")
	code, _, errs = ta.do("stop --reason 'a bench' --until yesterday")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "found yesterday")

	stop := ta.ok("stop --reason 'the bench is rebooting' --until 1h")
	want := "STOPPED, expected back by " + t0.Add(time.Hour).Format("3:04 PM") + " (the bench is rebooting); start ends it"
	assert.Contains(t, stop, "STOP OK before=RUNNING after=STOPPED changed")
	assert.Contains(t, ta.ok("where"), "\n"+want+"\n", "where's header")
	var v struct {
		Machine string `json:"machine"`
	}
	ta.json("where --json", &v)
	assert.Equal(t, "machine: "+want, v.Machine, "the dashboard's machine line")

	// stop with no --until says STOPPED until the next start (<reason>)
	ta.ok("stop --reason 'the bench is rebooting'")
	wantNoUntil := "STOPPED until the next start (the bench is rebooting)"
	assert.Contains(t, ta.ok("where"), "\n"+wantNoUntil+"\n", "where's header with no until")
	ta.json("where --json", &v)
	assert.Equal(t, "machine: "+wantNoUntil, v.Machine, "the dashboard's machine line with no until")

	ta.mu.Lock()
	ta.now = ta.now.Add(time.Hour)
	ta.mu.Unlock()
	tick := ta.ok("tick")
	assert.Contains(t, tick, "state=STOPPED", "the tick at --until leaves the machine stopped:\n%s", tick)
	ta.ok("start")
	inbox := ta.ok("inbox")
	require.True(t, strings.HasSuffix(inbox, "machine: running\n"), "inbox:\n%s", inbox)
}
