package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The v1.2.12 switch rolled itself back 1.5 seconds after it began: the restart dropped the
// seat's hook, the new server's first land pass ran as the coordinator before the seat
// hooked in again, the hook gate refused it before it acted on anything, and the land loop
// counted that refusal as a land fault inside the rollback window
// (docs/SPEC-SPRINT.md section 14, "The rollback window"; tla/ServerSwitch.tla
// HookRefusedLand). These tests pin the rule: a refusal by the hook gate is a wait, a land
// that acted and failed is a fault.

// switchedRig is a land rig whose binary was switched with a rollback window of 15
// minutes at t0, the coordinator's hook gate armed for the test, and one card queued to
// merge. Not parallel: the gate's test seam (hookTests) is keyed by the coordinator's name,
// which every rig shares.
type switchedRig struct {
	*landRig
	target, dir string
	now         *time.Time
	t0          time.Time
}

func newSwitchedRig(t *testing.T) *switchedRig {
	t.Helper()
	r := newLandRig(t)
	r.ok("add --stream s1 s1-1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "a.txt", "a\n")}, "s1-1")

	dir := t.TempDir()
	target := filepath.Join(dir, "nova-sprint")
	candidate := filepath.Join(dir, "nova-sprint-candidate")
	require.NoError(t, os.WriteFile(target, []byte("binary-v1"), 0o755))
	require.NoError(t, os.WriteFile(candidate, []byte("binary-v2"), 0o755))

	t0 := time.Date(2026, 10, 11, 21, 0, 0, 0, time.UTC)
	now := t0
	r.a.now = func() time.Time { return now }
	r.a.executable = func() (string, error) { return target, nil }
	require.NoError(t, sprint.ServerSwitch(context.Background(), sprint.ServerSwitchOptions{
		Binary: candidate, Target: target, Rollback: true, Window: 15 * time.Minute, Now: func() time.Time { return now },
	}))
	sr := &switchedRig{landRig: r, target: target, dir: dir, now: &now, t0: t0}
	sr.gate(t, true)
	t.Cleanup(func() { hookTests.Delete("coordinator") })
	return sr
}

// gate arms the hook gate for the coordinator, or lifts it for a read of the rig's own
// (the card verb is gated for the seat's holder too).
func (r *switchedRig) gate(t *testing.T, armed bool) {
	t.Helper()
	if armed {
		hookTests.Store("coordinator", true)
	} else {
		hookTests.Delete("coordinator")
	}
}

// hookLive records a live, proven hook for the coordinator, as the server's hook session
// does once the seat answers its challenge.
func (r *switchedRig) hookLive(t *testing.T) {
	t.Helper()
	st, err := r.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	now := r.a.now()
	require.NoError(t, writeHook(context.Background(), st, sprint.HookRecord{
		Name: "coordinator", Session: "s1", State: sprint.HookHooked, Since: now, Proven: now, Live: now.Add(time.Hour),
	}))
}

func (r *switchedRig) binary(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(r.target)
	require.NoError(t, err)
	return string(b)
}

// The seat's hook is gone after the restart: the land is refused by the hook gate before
// it acts on anything, every pass inside the window, and none of those passes rolls the
// switch back. Each pass says once that it waits for the seat's hook, and the window and
// the card queued to merge are left as they were.
func TestAHookGateRefusalInTheWindowIsNotARollback(t *testing.T) {
	r := newSwitchedRig(t)
	more := []string{"--repo-dir", r.clone, "--base", "main"}
	for pass := range 3 {
		*r.now = r.t0.Add(1500*time.Millisecond + time.Duration(pass)*LandEvery)
		var out bytes.Buffer
		code, _ := r.a.landOnce(context.Background(), "mem:0", more, &out)
		assert.NotEqual(t, 0, code, "pass %d: nothing was landed", pass)
		assert.NotContains(t, out.String(), "SERVER ROLLBACK", "pass %d: %s", pass, out.String())
		assert.Equal(t, 1, strings.Count(out.String(), "LAND WAITS for the seat's hook"), "pass %d says once that it waits: %s", pass, out.String())
		assert.Equal(t, "binary-v2", r.binary(t), "pass %d: the switch stands", pass)
		_, err := os.Stat(r.target + ".switch.json")
		assert.NoError(t, err, "pass %d: the rollback window is still open", pass)
	}
	r.gate(t, false)
	assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"), "the card is still queued to merge")
	r.gate(t, true)

	// the seat hooks in again: the same window, a land that is not refused by the gate and
	// then fails still rolls back (TestARealLandFailureInTheWindowStillRollsBack)
	r.hookLive(t)
	*r.now = r.t0.Add(time.Minute)
	var out bytes.Buffer
	code, _ := r.a.landOnce(context.Background(), "mem:0", []string{"--repo-dir", filepath.Join(r.dir, "missing"), "--base", "main"}, &out)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out.String(), "SERVER ROLLBACK land failed within switch window")
	assert.Equal(t, "binary-v1", r.binary(t))
}

// A land that is let through by the hook gate, acts and fails inside the window counts
// toward the rollback, and one failing past the window does not.
func TestARealLandFailureInTheWindowStillRollsBack(t *testing.T) {
	r := newSwitchedRig(t)
	r.hookLive(t)
	more := []string{"--repo-dir", filepath.Join(r.dir, "missing"), "--base", "main"}

	*r.now = r.t0.Add(5 * time.Minute)
	var out bytes.Buffer
	code, _ := r.a.landOnce(context.Background(), "mem:0", more, &out)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out.String(), "SERVER ROLLBACK land failed within switch window")
	assert.NotContains(t, out.String(), "waits for the seat's hook", "the gate let it through")
	assert.Equal(t, "binary-v1", r.binary(t), "a failed land in the window rolls the target back")
	_, err := os.Stat(r.target + ".switch.json")
	assert.True(t, errors.Is(err, os.ErrNotExist), "the rollback consumed the window")
}
