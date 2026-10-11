package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestTheServersLandLoopLandsWhileTheSeatIsUnhooked: queued accepted cards land with no hook.
// The server's internal land loop must run whether or not the seat is hooked.
func TestTheServersLandLoopLandsWhileTheSeatIsUnhooked(t *testing.T) {
	r := newLandRig(t)
	r.ok("add --stream s1 s1-1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}, "s1-1")

	// Ensure the hook gate is NOT armed for the coordinator (seat is unhooked)
	hookTests.Delete("coordinator")
	t.Cleanup(func() { hookTests.Delete("coordinator") })

	more := []string{"--repo-dir", r.clone, "--base", "main"}
	var out bytes.Buffer
	code, _ := r.a.landOnce(context.Background(), "mem:0", more, &out)

	// The land should succeed (exit 0) despite no hook
	assert.Equal(t, 0, code, "server land should succeed without seat hook: %s", out.String())
	assert.Contains(t, out.String(), "LAND OK", "card should land")
	assert.NotContains(t, out.String(), "LAND WAITS", "should not wait for hook")
}

// TestAHandLandIsStillRefusedUntilHooked: a hand land by the coordinator is still refused.
// The hook gate must still apply to manual land commands from the seat.
func TestAHandLandIsStillRefusedUntilHooked(t *testing.T) {
	r := newLandRig(t)
	r.ok("add --stream s1 s1-1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}, "s1-1")

	// Arm the hook gate for the coordinator (so the gate is checked)
	hookTests.Store("coordinator", true)
	t.Cleanup(func() { hookTests.Delete("coordinator") })

	// Simulate a hand land by the coordinator (not landLazy)
	r.a.landLazy = false
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := r.a.cmdLand([]string{"--redis", "mem:0", "--actor", "coordinator"}, stdout, stderr)

	// The hand land should be refused without the hook (exit 2 for refusal)
	assert.Equal(t, 2, code, "hand land should be refused without seat hook: %s", stderr.String())
	assert.Contains(t, stderr.String(), "hook", "hand land should require hook")
}

// TestServerLandLoopBypassesHookGate: the land loop sets landLazy, which bypasses the hook gate.
func TestServerLandLoopBypassesHookGate(t *testing.T) {
	r := newLandRig(t)
	r.ok("add --stream s1 s1-1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}, "s1-1")

	// Hook gate not armed (seat unhooked)
	hookTests.Delete("coordinator")
	t.Cleanup(func() { hookTests.Delete("coordinator") })

	// Set landLazy to simulate server's internal land loop
	r.a.landLazy = true
	defer func() { r.a.landLazy = false }()

	// The store should open without hook check when landLazy is set
	st, err := r.a.store(common{redis: "mem:0", verb: "land", actor: "coordinator"})
	require.NoError(t, err, "server land should bypass hook gate when landLazy=true")
	require.NotNil(t, st, "store should be opened")
}

// TestManualLandRequiresHookGate: a manual land from coordinator requires the hook.
func TestManualLandRequiresHookGate(t *testing.T) {
	r := newLandRig(t)
	r.ok("add --stream s1 s1-1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}, "s1-1")

	// Arm the hook gate for the coordinator (so the gate is checked)
	hookTests.Store("coordinator", true)
	t.Cleanup(func() { hookTests.Delete("coordinator") })

	// landLazy false simulates a manual land from the seat
	r.a.landLazy = false
	_, err := r.a.store(common{redis: "mem:0", verb: "land", actor: "coordinator"})

	require.Error(t, err, "manual land should be refused without hook")
	assert.Contains(t, err.Error(), "You must hook in first", "error should mention hook requirement")
}

// TestLandLoopExemptWhileHookStillEnforcedForManual: verify both behaviors work together.
func TestLandLoopExemptWhileHookStillEnforcedForManual(t *testing.T) {
	r := newLandRig(t)
	r.ok("add --stream s1 s1-1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}, "s1-1")

	// Arm the hook gate for the coordinator (so the gate is checked for manual land)
	hookTests.Store("coordinator", true)
	t.Cleanup(func() { hookTests.Delete("coordinator") })

	// Server land loop should succeed (bypasses hook even when armed)
	r.a.landLazy = true
	st1, err1 := r.a.store(common{redis: "mem:0", verb: "land", actor: "coordinator"})
	require.NoError(t, err1, "server land should bypass hook")
	require.NotNil(t, st1, "store should be opened for server land")

	// Manual land should fail (hook check enforced when not landLazy)
	r.a.landLazy = false
	_, err2 := r.a.store(common{redis: "mem:0", verb: "land", actor: "coordinator"})
	require.Error(t, err2, "manual land should require hook")
	assert.Contains(t, err2.Error(), "You must hook in first")
}

// TestHookGateBypassedForLandLoopContext: test the coordinatorGate method directly.
func TestHookGateBypassedForLandLoopContext(t *testing.T) {
	r := newLandRig(t)
	r.ok("add --stream s1 s1-1 --one")

	// Arm the hook gate for the coordinator (so the gate is checked for manual land)
	hookTests.Store("coordinator", true)
	t.Cleanup(func() { hookTests.Delete("coordinator") })

	// Test with landLazy = true (server land loop)
	r.a.landLazy = true
	st, err := r.a.store(common{redis: "mem:0", verb: "land", actor: "coordinator"})
	require.NoError(t, err, "server land should bypass hook")
	require.NotNil(t, st)

	// Test with landLazy = false (manual land)
	r.a.landLazy = false
	_, err = r.a.store(common{redis: "mem:0", verb: "land", actor: "coordinator"})
	require.Error(t, err, "manual land should require hook")
}

// TestLandLoopRunsWhileSeatAway: simulate seat being away (hook state = away).
func TestLandLoopRunsWhileSeatAway(t *testing.T) {
	r := newLandRig(t)
	r.ok("add --stream s1 s1-1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}, "s1-1")

	// Store and set hook state to away
	st, err := r.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	now := time.Now()
	require.NoError(t, writeHook(context.Background(), st, sprint.HookRecord{
		Name: "coordinator", Session: "s1", State: sprint.HookAway, Since: now,
	}))

	hookTests.Delete("coordinator")
	t.Cleanup(func() { hookTests.Delete("coordinator") })

	// Server land should succeed despite hook state away
	r.a.landLazy = true
	more := []string{"--repo-dir", r.clone, "--base", "main"}
	var out bytes.Buffer
	code, _ := r.a.landOnce(context.Background(), "mem:0", more, &out)

	assert.Equal(t, 0, code, "server land should succeed when seat is away: %s", out.String())
	assert.Contains(t, out.String(), "LAND OK")
}

// TestHookGateRefusalMessage: ensure hook gate refusal messages are clear.
func TestHookGateRefusalMessage(t *testing.T) {
	r := newLandRig(t)

	// Arm the hook gate for the coordinator (so the gate is checked)
	hookTests.Store("coordinator", true)
	t.Cleanup(func() { hookTests.Delete("coordinator") })

	// Manual land should get clear error message
	r.a.landLazy = false
	_, err := r.a.store(common{redis: "mem:0", verb: "land", actor: "coordinator"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook", "error should mention hook")
}
