package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

func TestStartAndStopSayTheStateBeforeAndAfter(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 3")
	out := ta.ok("where")
	require.Contains(t, out, "SPRINT TABLE  coordinator coordinator\n\nREADY\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n", "where")
	require.NotContains(t, out, "machine:", "where")
	// stop on READY is refused with its reason (docs/SPEC-SPRINT.md section 14, "READY")
	code, _, errs := ta.do("stop --reason r --until 9999h")
	require.NotEqual(t, 0, code, "stop when READY")
	require.Contains(t, errs, "the machine is READY: it has not started since the clear; nothing to stop", "stop when READY")
	out = ta.ok("start")
	require.Contains(t, out, "START OK before=READY after=RUNNING changed", "start")
	require.Contains(t, out, "nothing is ticking: run: nova-sprint install server --listen", "start: the server, which ticks and lands")
	require.Contains(t, out, "0/3 0.0% -> ETA -  machine: running", "start")
	require.Contains(t, ta.ok("start"), "unchanged: the machine is RUNNING already", "start when running")
	out = ta.ok("tick")
	require.Contains(t, out, "MOVED deal: s1-1 work ready -> working", "tick")
	require.Contains(t, out, "TICK OK state=RUNNING idle=no moved=4", "tick") // three deals and the status record's first sight of m1
	ta.a.sleep(store.MachineSilence + time.Second)
	out = ta.ok("inbox")
	require.Contains(t, out, "machine: running (tick late 16s)\n", "inbox with a late tick: running, never STOPPED")
	require.NotContains(t, out, "(no tick", "inbox with no tick")
	out = ta.ok("take --as m1 --limit 1")
	require.Contains(t, out, "0/3 0.0% -> ETA -  machine: running (tick late 16s)\n", "a verb's line with a late tick")
	require.NotContains(t, out, "(no tick", "a verb's line with no tick")
	require.NotContains(t, out, "STOPPED", "a verb's line with a late tick")
	ta.ok("stop --reason r --until 9999h")
	out = ta.ok("stop --reason r --until 9999h")
	require.Contains(t, out, "STOP OK before=STOPPED after=STOPPED unchanged: the machine is STOPPED already", "stop when stopped")
	require.Contains(t, out, "\nSTOPPED, expected back by ", "stop when stopped")
	require.Contains(t, out, "  0/3 0.0%", "stop when stopped")
	require.NotContains(t, out, "-> ETA", "stop when stopped")
	require.Contains(t, ta.ok("tick"), "TICK OK state=STOPPED nothing done", "a tick while stopped")
}

func TestRunTicksOnlyWhileRunning(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 3")
	st, _, code := ta.a.machineVerb("run", nil, &bytes.Buffer{})
	require.NotNil(t, st, "run: %d", code)
	// the loop's server record says it lands, as run with no flag does since v1.2.17
	// (start refuses on a server that lands nothing: start-refuses-a-non-working-setting)
	ta.a.serverLands = true
	var out, errb bytes.Buffer
	ta.a.runLoop(context.Background(), st, 20, 3, &out, &errb)
	require.NotContains(t, out.String(), "MOVED", "run while stopped:\n%s", out.String())
	require.Contains(t, out.String(), "machine STOPPED", "run while stopped:\n%s", out.String())
	ta.ok("start")
	out.Reset()
	ta.a.runLoop(context.Background(), st, 20, 3, &out, &errb)
	require.Contains(t, out.String(), "machine RUNNING", "run while running:\n%s", out.String())
	require.Contains(t, out.String(), "MOVED deal: s1-1 work ready -> working", "run while running:\n%s", out.String())
	require.Contains(t, ta.ok("where"), "SPRINT TABLE  coordinator coordinator\n\n0/3 0.0% -> ETA -\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n", "where after run")
}

// whereHead is what the where view says under its title: the lines between
// SPRINT TABLE and the first table, with the clock line before the title cut.
func whereHead(t *testing.T, out string) string {
	t.Helper()
	i := strings.Index(out, "SPRINT TABLE  coordinator coordinator\n")
	require.GreaterOrEqual(t, i, 0, "where has no title:\n%s", out)
	return out[i:]
}

func TestWhereHeaderIsTheStateOrTheProgressLine(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	// never started, READY: the title, a blank line, the word, a blank line, byte for byte
	got := whereHead(t, ta.ok("where"))
	require.True(t, strings.HasPrefix(got, "SPRINT TABLE  coordinator coordinator\n\nREADY\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n"), "ready:\n%q", got)
	require.False(t, strings.HasPrefix(got, "SPRINT TABLE  coordinator coordinator\n\nREADY\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n\n"), "ready:\n%q", got)
	// running with no cards
	ta.ok("start")
	got = whereHead(t, ta.ok("where"))
	require.True(t, strings.HasPrefix(got, "SPRINT TABLE  coordinator coordinator\n\n0/0 0.0% -> ETA -\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n"), "running with no cards:\n%q", got)
	// running with cards
	ta.ok("add --stream s1 --count 3")
	ta.ok("tick")
	out := ta.ok("where")
	got = whereHead(t, out)
	require.True(t, strings.HasPrefix(got, "SPRINT TABLE  coordinator coordinator\n\n0/3 0.0% -> ETA -\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n"), "running with cards:\n%q", out)
	require.NotContains(t, out, "machine:", "running with cards:\n%q", out)
	require.NotContains(t, out, "coordinator:", "running with cards:\n%q", out)
	// running but silent: never hidden, and never STOPPED: the progress line
	// and the late tick (docs/SPEC-SPRINT.md section 1)
	ta.a.sleep(store.MachineSilence + time.Second)
	got = whereHead(t, ta.ok("where"))
	require.True(t, strings.HasPrefix(got, "SPRINT TABLE  coordinator coordinator\n\n0/3 0.0% -> ETA -  running (tick late 16s)\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n"), "running but silent:\n%q", got)
	require.NotContains(t, got, "(no tick", "running but silent:\n%q", got)
	// stopped: the title, a blank line, the stop's line alone (no progress, no ETA), a
	// blank line, byte for byte
	ta.ok("stop --reason r --until 9999h")
	now := ta.a.now()
	stopped := "SPRINT TABLE  coordinator coordinator\n\n" + sprint.StoppedText("coordinator", "r", now.Add(9999*time.Hour), now) + "\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n"
	got = whereHead(t, ta.ok("where"))
	require.True(t, strings.HasPrefix(got, stopped), "stopped:\n%q\nwant the prefix\n%q", got, stopped)
	require.False(t, strings.HasPrefix(got, stopped+"\n"), "stopped:\n%q", got)
}

func TestWhereHeaderReadyOrStoppedIsExactlyTheView(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	out := ta.ok("where")
	i := strings.Index(out, "work")
	require.GreaterOrEqual(t, i, 0, "ready view:\n%q", out)
	require.Equal(t, "store mem:0 (from NOVA_SPRINT_REDIS)\nSPRINT TABLE  coordinator coordinator\n\nREADY\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n", out[:i], "ready view (never started):\n%q", out)
	ta.ok("start")
	ta.ok("stop --reason r --until 9999h")
	now := ta.a.now()
	out = ta.ok("where")
	i = strings.Index(out, "work")
	require.GreaterOrEqual(t, i, 0, "stopped view:\n%q", out)
	require.Equal(t, "store mem:0 (from NOVA_SPRINT_REDIS)\nSPRINT TABLE  coordinator coordinator\n\n"+sprint.StoppedText("coordinator", "r", now.Add(9999*time.Hour), now)+"\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\n", out[:i], "stopped view:\n%q", out)
}

// TestWhereDoesNotPrintTheRoutesLine pins the view going from the machine state line
// to the work table with one blank line between, on a store that holds routes: no
// `routes: flash=n pro=m` line in the text, no `routes` in the JSON (`nova-sprint
// routes` is where the tiers are read).
func TestWhereDoesNotPrintTheRoutesLine(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetRoutes([]sprint.Route{
		{Name: "flash-a", Tier: "flash", Provider: "p", Model: "m", Enabled: true},
		{Name: "pro-a", Tier: "pro", Provider: "p", Model: "m", Enabled: true},
	})
	out := ta.ok("where")
	require.NotContains(t, out, "routes:")
	require.Contains(t, out, "SPRINT TABLE  coordinator coordinator\n\nREADY\nproviders: none applied WARN no provider rows applied (nova-config apply --kind provider)\n\nwork") // never started: READY (section 14)
	var object map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(ta.ok("where --json")), &object))
	require.NotContains(t, object, "routes", "coverage gaps have routes, but the top-level route list remains absent")
	require.Contains(t, ta.ok("routes"), "flash=1 pro=1")
}
