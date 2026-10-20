package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/bus/bustest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A coordinator the bus does not name: the server's push to its name fails with
// the bus's no-known-name refusal, which unproves the sprint push; the gate then
// refuses every verb naming the failed send and its remedy (nova-config friend
// add <name> ... then nova-config apply), in the gate stays closed.
func TestHookRefusalNamesAnUndeliverableCoordinatorPush(t *testing.T) {
	t.Parallel()
	const name = "push-no-name"
	ta, rig, _ := coordSprint(t, name)
	hookBusOnly(t, rig, name)
	ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: partial as coordinator")
	ta.pass(name)
	ta.lined(func() { ta.ok("where") })
	timerJudgment(ta, name)

	send := ta.a.bus
	unknown := &bus.Refusal{Problems: []string{fmt.Sprintf("%s is no known name; the names are nova-config's friend and machine rows (nova-bus names lists them); add one with nova-config friend add %s --width 1 --tiers flash --as <you>, then nova-config apply", name, name)}}
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { return unknown }
	ta.step(hook.RenewEvery)
	ta.a.coordinatorPass(context.Background())
	why := ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: live as coordinator (bus=proven sprint=armed)")
	assert.Contains(t, why, "the last send to it failed")
	assert.Contains(t, why, "no known name")
	assert.Contains(t, why, "nova-config friend add "+name)
	ta.a.bus = send
}

// init --coordinator records the name the bus store answers is no known name: it
// refuses with the bus's remedy, and writes nothing; a name the bus roster holds
// is accepted.
func TestInitRefusesACoordinatorTheBusDoesNotName(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	env := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == busRedisEnv {
			return "127.0.0.1:6381"
		}
		return env(k)
	}
	fake := bustest.NewFake(time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC), "glenn", "m1", "m2")
	fake.Friends = []string{"glenn"}
	ta.a.busOpen = func(context.Context, string, string) (*bus.Bus, func(), error) {
		return &bus.Bus{Store: fake}, func() {}, nil
	}

	before := ta.applies()
	code, out, errs := ta.do("init --readers reader-a --members m1 --coordinator rowan")
	require.Equal(t, 2, code, "%s%s", out, errs)
	require.Contains(t, errs, "rowan is no known name")
	require.Contains(t, errs, "nova-config friend add rowan")
	require.Empty(t, out)
	require.Equal(t, before, ta.applies(), "a refused init wrote")

	out = ta.ok("init --readers reader-a --members m1 --coordinator glenn")
	require.Contains(t, out, "INIT OK")
}

// The read lane's app shares the server's failed coordinator send with the gate: a
// read the server serves on the lane (where, card, routes) refuses naming the failed
// send and its remedy, not just "not hooked in".
func TestReadLaneRefusalNamesAnUndeliverableCoordinatorPush(t *testing.T) {
	t.Parallel()
	const name = "read-lane-no-name"
	ta, rig, _ := coordSprint(t, name)
	hookBusOnly(t, rig, name)
	ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: partial as coordinator")
	ta.pass(name)
	ta.lined(func() { ta.ok("where") })
	timerJudgment(ta, name)

	send := ta.a.bus
	unknown := &bus.Refusal{Problems: []string{fmt.Sprintf("%s is no known name; the names are nova-config's friend and machine rows (nova-bus names lists them); add one with nova-config friend add %s --width 1 --tiers flash --as <you>, then nova-config apply", name, name)}}
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { return unknown }
	ta.step(hook.RenewEvery)
	ta.a.coordinatorPass(context.Background())

	res := serveOne(ta.a, true, "where", "--actor", name)
	require.Equal(t, 1, res.Code, "%s%s", res.Stdout, res.Stderr)
	why := res.Stderr
	assert.Contains(t, why, sprint.HookFirst)
	assert.Contains(t, why, "the last send to it failed")
	assert.Contains(t, why, "no known name")
	assert.Contains(t, why, "nova-config friend add "+name)
	ta.a.bus = send
}
