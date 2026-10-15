package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/hook/hooktest"
)

// coordSprint is a sprint whose coordinator is name, armed for the gate, its owner glenn,
// the server's store the twin, and the nova-hook record a hooktest rig (miniredis over
// net.Pipe, on a clock the test moves): the record's one truth, as nova-hook keeps it.
// Every bus message the app sends is kept on ta.sent.
func coordSprint(t *testing.T, name string) (*testApp, *hooktest.Rig, map[string]string) {
	t.Helper()
	ta := newTestApp(t)
	env := map[string]string{"NOVA_SPRINT_REDIS": "mem:0", "NOVA_SPRINT_ACTOR": name, busRedisEnv: "bus.test:6379"}
	var mu sync.Mutex
	ta.a.getenv = func(k string) string { mu.Lock(); defer mu.Unlock(); return env[k] }
	rig := hooktest.New(t)
	ta.a.hookStoreFn = func(context.Context) (hook.Store, error) { return rig.Store, nil }
	hookTests.Store(name, true)
	t.Cleanup(func() { hookTests.Delete(name) })
	ta.ok("init --readers reader-a,reader-b --members m1,m2 --owner glenn")
	ta.a.serveAddr = "mem:0"
	return ta, rig, env
}

// hookBusOnly hooks name in as the coordinator as `nova-hook hook --as <name> --role
// coordinator` does, and answers its challenge: the bus push live and proven, the
// heartbeat's evidence standing, and the sprint push held by no one yet (the server
// holds it: holdSprintPush).
func hookBusOnly(t *testing.T, rig *hooktest.Rig, name string) {
	t.Helper()
	ctx := context.Background()
	_, _, err := rig.Store.HookAs(ctx, name, hook.Coordinator, "nova-hook")
	require.NoError(t, err)
	h := &hook.Holder{Store: rig.Store, Name: name, Role: hook.Coordinator, Pushes: []hook.Push{hook.Bus}, By: "nova-hook"}
	require.NoError(t, h.Start(ctx))
	require.NoError(t, h.Beat(ctx, map[hook.Push]bool{hook.Bus: true}))
	rig.Evidence(t, name)
}

// step moves the test's clock.
func (ta *testApp) step(d time.Duration) {
	ta.mu.Lock()
	ta.now = ta.now.Add(d)
	ta.mu.Unlock()
}

// lined runs f holding the server's line, as every verb the server runs does.
func (ta *testApp) lined(f func()) {
	ta.a.serial.Lock()
	defer ta.a.serial.Unlock()
	f()
}

func (ta *testApp) doLined(line string) (code int, out, errs string) {
	ta.lined(func() { code, out, errs = ta.do(line) })
	return
}

// refusedFirst wants the line refused with the gate's one line, exit 1, and nothing
// written: the line starts with want.
func (ta *testApp) refusedFirst(line, want string) string {
	ta.t.Helper()
	var errs string
	ta.lined(func() {
		before := ta.applies()
		code, out, e := ta.do(line)
		errs = e
		require.Equal(ta.t, 1, code, "%s: exit %d\n%s%s", line, code, out, errs)
		require.True(ta.t, strings.HasPrefix(errs, want) && strings.Count(errs, "\n") == 1 && strings.HasSuffix(errs, "\n"), "%s: %q", line, errs)
		require.Empty(ta.t, out, line)
		require.Equal(ta.t, before, ta.applies(), "%s was refused and wrote", line)
	})
	return errs
}

func gateFirstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

// The owner, 2026-10-11: "If you are not hooked in to push notifications, every verb
// [...] must fail, and tell you to hook in." For the coordinator whose nova-hook record
// says it is not hooked in, every verb of the help registry (verbs) fails with exit 1
// and the one line that starts "You must hook in first" and names the nova-hook command
// that hooks the coordinator in, and nothing is written. The table is the whole registry,
// so a verb added later is covered by construction. Exempt: the server's own life (run,
// tick, server switch, seat install, selftest land: the server holds the coordinator's
// sprint push, so a verb that starts it cannot wait on it). The holder's --as never takes
// it past the gate, and tick with --answer-rules is gated. The sprint's own hook verbs are
// retired: no verb of the registry is hook or unhook.
func TestEveryVerbRefusesTheCoordinatorUntilHooked(t *testing.T) {
	t.Parallel()
	const name = "hook-every"
	ta, _, _ := coordSprint(t, name)

	var lines []string
	for _, v := range verbs {
		assert.NotContains(t, []string{"hook", "unhook", "seat push"}, v.name, "the sprint's own seat hook and seat push are retired (v1.3.6)")
		if hookExempt[v.name] {
			continue
		}
		lines = append(lines, v.name)
		if fs := verbFlags(v.name); fs != nil && fs.Lookup("as") != nil {
			lines = append(lines, v.name+" --as m1")
		}
	}
	lines = append(lines, "tick --answer-rules")
	var refused, ran, crumbless []string
	for _, line := range lines {
		before := ta.applies()
		code, out, errs := ta.doLined(line)
		got := strings.TrimRight(errs, "\n")
		ok := code == 1 && out == "" && !strings.Contains(got, "\n") &&
			strings.HasPrefix(got, sprint.HookFirst+": "+name+" is not hooked in") &&
			strings.Contains(got, "run: nova-hook hook --as "+name+" --role coordinator") &&
			strings.Contains(got, "nova-hook prove") &&
			!strings.Contains(got, "nova-sprint"+" hook") &&
			before == ta.applies()
		if ok {
			refused = append(refused, line)
			continue
		}
		if code == 1 && strings.HasPrefix(got, sprint.HookFirst) {
			crumbless = append(crumbless, line+": "+got)
			continue
		}
		ran = append(ran, line+" (exit "+strconv.Itoa(code)+": "+gateFirstLine(out+errs)+")")
	}
	assert.Empty(t, ran, "verbs the unhooked coordinator ran past the gate")
	assert.Empty(t, crumbless, "verbs refused without the breadcrumb (the nova-hook command)")
	assert.NotEmpty(t, refused)
	for _, v := range []string{"adopt", "seat uninstall", "friend clean", "tick --answer-rules", "take --as m1"} {
		assert.Contains(t, refused, v, "gated for the holder")
	}
}

// The gate reads the nova-hook record alone (v1.3.6): unhooked, refused; hooked in as the
// coordinator with nova-hook holding the bus push and its challenge answered, still
// refused while no server holds the sprint push, naming the server as its holder; once
// the server holds it (holdSprintPush), the verb runs; the coordinator unhooked, refused
// again, and the server, which never arms a record, holds nothing until it hooks in
// again; hooked in as comms, refused (the coordinator's verbs are not comms'); and every
// push proven but no answer to the heartbeat within its window, refused.
func TestTheGateReadsTheNovaHookRecord(t *testing.T) {
	t.Parallel()
	const name = "hook-record"
	ta, rig, _ := coordSprint(t, name)
	ctx := context.Background()

	why := ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: no hook record")
	assert.Contains(t, why, "nova-hook hook --as "+name+" --role coordinator")

	hookBusOnly(t, rig, name)
	why = ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: partial as coordinator (bus=proven sprint=down)")
	assert.Contains(t, why, "the sprint push of a coordinator is held by: "+sprint.SprintPushHeldBy)

	said, err := ta.a.holdSprintPush(ctx, name, true)
	require.NoError(t, err)
	assert.Empty(t, said)
	assert.Equal(t, hook.StateProven, rig.State(t, name).State)
	ta.lined(func() { ta.ok("where") })

	rig.Unhook(t, name)
	ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: unhooked at")
	said, err = ta.a.holdSprintPush(ctx, name, true)
	require.NoError(t, err)
	assert.Contains(t, said, "is not held: its record is state=unhooked")
	assert.Equal(t, hook.StateUnhooked, rig.State(t, name).State, "the server never hooks a coordinator in: an unhook stands")

	rig.Hook(t, name, hook.Comms)
	ta.refusedFirst("where", sprint.HookFirst+": "+name+" is hooked in as comms, and this act is only for coordinator")
	said, err = ta.a.holdSprintPush(ctx, name, true)
	require.NoError(t, err)
	assert.Contains(t, said, "role=comms", "the server holds the sprint push of a coordinator's record alone")

	rig.Hook(t, name, hook.Coordinator)
	ta.lined(func() { ta.ok("where") })
	// the server is the coordinator's sprint push: a failed send unproves it
	said, err = ta.a.holdSprintPush(ctx, name, false)
	require.NoError(t, err)
	assert.Contains(t, said, "held and unproven")
	ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: live as coordinator")
	_, err = ta.a.holdSprintPush(ctx, name, true)
	require.NoError(t, err)
	ta.lined(func() { ta.ok("where") })

	// two heartbeat intervals with no answer from the session: down, whatever the pushes say
	require.NoError(t, rig.Client.HSet(ctx, hook.RecordKey(name), "answered_at", time.Date(2026, 10, 11, 11, 0, 0, 0, time.UTC).UnixMilli()).Err())
	ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: "+hook.NoAnswerReason)

	// another actor than the holder is not the coordinator's gate's to judge
	ta.lined(func() {
		code, _, errs := ta.do("where --actor someone-else")
		assert.NotContains(t, errs, sprint.HookFirst, "exit %d", code)
	})
}

// Fail closed (the owner: "Fail closed, not open."): a coordinator whose record cannot be
// read is refused naming why: the bus Redis, where nova-hook keeps the record, not named
// (NOVA_BUS_REDIS unset); the record's store failing; and a sprint store that does not
// open.
func TestTheGateFailsClosed(t *testing.T) {
	t.Parallel()
	const name = "hook-closed"
	ta, rig, env := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	ta.lined(func() { ta.ok("where") })

	// no test store: the process's own, which wants NOVA_BUS_REDIS
	delete(env, busRedisEnv)
	ta.a.hookStoreFn = nil
	why := ta.refusedFirst("where", sprint.HookFirst+": the nova-hook record of "+name+" could not be checked")
	assert.Contains(t, why, busRedisEnv+" is not set")
	assert.Contains(t, why, "nova-hook hook --as "+name+" --role coordinator")

	ta.a.hookStoreFn = func(context.Context) (hook.Store, error) {
		return hook.Store{}, errors.New("the bus Redis refused the dial")
	}
	why = ta.refusedFirst("where", sprint.HookFirst+": the nova-hook record of "+name+" could not be checked")
	assert.Contains(t, why, "the bus Redis refused the dial")

	ta.a.hookStoreFn = func(context.Context) (hook.Store, error) { return rig.Store, nil }
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		return nil, errors.New("connection refused")
	}
	code, out, errs := ta.do("where")
	assert.Equal(t, 1, code, out+errs)
	assert.True(t, strings.HasPrefix(errs, sprint.HookFirst), errs)
	assert.Contains(t, errs, "did not open")
	assert.Contains(t, errs, "connection refused")
}
