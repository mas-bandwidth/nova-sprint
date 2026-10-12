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
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus/bustest"
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
// merge. The gate's test seam (hookTests) is keyed by the coordinator's name, so each rig
// names its own coordinator (seat) and the tests run in parallel.
type switchedRig struct {
	*landRig
	target, dir string
	seat        string
	now         *time.Time
	t0          time.Time
}

func newSwitchedRig(t *testing.T) *switchedRig {
	t.Helper()
	seat := "coordinator-" + strings.ToLower(strings.NewReplacer("/", "-", "_", "-").Replace(t.Name()))
	r := newLandRigAs(t, seat)
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
	// the gate's bus push is live over an in-memory bus store, as the hook process arms it
	fake := bustest.NewFake(t0, seat)
	r.a.busOpen = func(context.Context, string, string) (*bus.Bus, func(), error) {
		return &bus.Bus{Store: fake}, func() {}, nil
	}
	getenv := r.a.getenv
	r.a.getenv = func(k string) string {
		if k == busRedisEnv {
			return "bus:test"
		}
		return getenv(k)
	}
	require.NoError(t, sprint.ServerSwitch(context.Background(), sprint.ServerSwitchOptions{
		Binary: candidate, Target: target, Rollback: true, Window: 15 * time.Minute, Now: func() time.Time { return now },
	}))
	sr := &switchedRig{landRig: r, target: target, dir: dir, seat: seat, now: &now, t0: t0}
	sr.gate(t, true)
	t.Cleanup(func() { hookTests.Delete(seat) })
	return sr
}

// gate arms the hook gate for the coordinator, or lifts it for a read of the rig's own
// (the card verb is gated for the seat's holder too).
func (r *switchedRig) gate(t *testing.T, armed bool) {
	t.Helper()
	if armed {
		hookTests.Store(r.seat, true)
	} else {
		hookTests.Delete(r.seat)
	}
}

// hookLive records a live, proven hook for the coordinator, as the server's hook session
// does once the seat answers its challenge: the sprint push and the bus push both live.
func (r *switchedRig) hookLive(t *testing.T) {
	t.Helper()
	st, err := r.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	now := r.a.now()
	require.NoError(t, writeHook(context.Background(), st, sprint.HookRecord{
		Name: r.seat, Session: "s1", State: sprint.HookHooked, Since: now, Proven: now, Live: now.Add(time.Hour),
	}))
	hk, err := r.a.hooker(context.Background())
	require.NoError(t, err)
	require.NoError(t, hk.Arm(context.Background(), r.seat, now))
	require.NoError(t, hk.Prove(context.Background(), r.seat, now))
	r.a.hookerForget(r.seat)
}

func (r *switchedRig) binary(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(r.target)
	require.NoError(t, err)
	return string(b)
}

// A hand land by the coordinator is still a coordinator command: while the seat is
// unhooked, the hook gate refuses it before it acts on anything, and the refusal is a wait,
// never a rollback fault — the window and the card are left as they were. The server's own
// land loop lands while the seat is unhooked (TestTheServersLandLoopLandsWhileTheSeatIsUnhooked);
// a land that is not refused by the gate and then fails still rolls back.
func TestAHookGateRefusalInTheWindowIsNotARollback(t *testing.T) {
	t.Parallel()
	r := newSwitchedRig(t)
	var out bytes.Buffer
	code := r.a.cmdLand([]string{"--repo-dir", r.clone, "--base", "main"}, &out, &out)
	assert.NotEqual(t, 0, code, "a hand land is refused while the seat is unhooked")
	assert.NotContains(t, out.String(), "SERVER ROLLBACK", "a hook-gate refusal is no rollback fault: %s", out.String())
	assert.Equal(t, "binary-v2", r.binary(t), "the switch stands")
	_, err := os.Stat(r.target + ".switch.json")
	assert.NoError(t, err, "the rollback window is still open")
	r.gate(t, false)
	assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"), "the card is still queued to merge")

	// the seat hooks in again: a land that is not refused by the gate and then fails still
	// rolls back (TestARealLandFailureInTheWindowStillRollsBack)
	r.gate(t, true)
	r.hookLive(t)
	*r.now = r.t0.Add(time.Minute)
	out.Reset()
	code, _ = r.a.landOnce(context.Background(), "mem:0", []string{"--repo-dir", filepath.Join(r.dir, "missing"), "--base", "main"}, &out)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out.String(), "SERVER ROLLBACK land failed within switch window")
	assert.Equal(t, "binary-v1", r.binary(t))
}

// A land that is let through by the hook gate, acts and fails inside the window counts
// toward the rollback, and one failing past the window does not.
func TestARealLandFailureInTheWindowStillRollsBack(t *testing.T) {
	t.Parallel()
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

// The release gate's switch check (docs/TESTS.md, "Release acceptance"; it replaces the
// ten-minute watch after server switch --rollback --window 30m): the restart drops the
// seat's hook, every land pass inside the window waits on it without a rollback, and once
// the seat hooks in again the same window's next pass lands the queued card for real, on
// the switched build, and the window stays open.
func TestASwitchedServerKeepsLandingOnceTheSeatHooksInAgain(t *testing.T) {
	t.Parallel()
	r := newSwitchedRig(t)
	more := []string{"--repo-dir", r.clone, "--base", "main"}
	// while the seat is unhooked the hook gate refuses a hand land, and the refusal is a
	// wait: the switch stands and the card stays queued. The server's own loop lands on its
	// own while unhooked (TestTheServersLandLoopLandsWhileTheSeatIsUnhooked).
	for pass := range 2 {
		*r.now = r.t0.Add(time.Second + time.Duration(pass)*LandEvery)
		var out bytes.Buffer
		code := r.a.cmdLand([]string{"--repo-dir", r.clone, "--base", "main"}, &out, &out)
		assert.NotEqual(t, 0, code, "pass %d: nothing was landed while the hook is gone", pass)
		assert.NotContains(t, out.String(), "SERVER ROLLBACK", "pass %d: %s", pass, out.String())
		assert.Equal(t, "binary-v2", r.binary(t), "pass %d: the switch stands", pass)
	}
	r.hookLive(t)
	*r.now = r.t0.Add(2 * time.Minute)
	var out bytes.Buffer
	code, _ := r.a.landOnce(context.Background(), "mem:0", more, &out)
	assert.Equal(t, 0, code, "the land after the seat hooked in: %s", out.String())
	assert.Contains(t, out.String(), "LAND OK stream=s1 cards=1 base=main", out.String())
	assert.NotContains(t, out.String(), "SERVER ROLLBACK", out.String())
	assert.Equal(t, "binary-v2", r.binary(t), "a landing on the switched build keeps it")
	_, err := os.Stat(r.target + ".switch.json")
	assert.NoError(t, err, "the window is still open after a good landing")
	r.gate(t, false)
	assert.Equal(t, map[string]string{"s1-1": "landed/merged"}, r.places("s1-1"), "the card queued before the switch landed")
}
