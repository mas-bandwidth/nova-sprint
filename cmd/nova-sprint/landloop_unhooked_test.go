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

// The server's own land loop lands accepted cards as machine work, not as a
// coordinator command: it must run whether or not the seat is hooked (a server
// restart, a dropped stream, a seat away). The hook gate is kept for the
// coordinator commands a person or seat runs, so a hand land by the coordinator
// is still refused until the seat hooks in.

// armHook arms the hook gate for the coordinator, as the server does once its
// hook session is live: a coordinator verb is then refused when no live, proven
// hook record is held.
func (r *landRig) armHook(t *testing.T) {
	t.Helper()
	hookTests.Store("coordinator", true)
	t.Cleanup(func() { hookTests.Delete("coordinator") })
}

// hookLiveUnhooked records a live, proven hook for the coordinator, as the
// server's hook session does once the seat answers its challenge.
func (r *landRig) hookLiveUnhooked(t *testing.T) {
	t.Helper()
	st, err := r.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	now := r.a.now()
	require.NoError(t, writeHook(context.Background(), st, sprint.HookRecord{
		Name: "coordinator", Session: "s1", State: sprint.HookHooked, Since: now, Proven: now, Live: now.Add(time.Hour),
	}))
}

// The machine land loop lands queued accepted cards while the seat is unhooked:
// the hook gate does not apply to the server's own landing.
func TestTheServersLandLoopLandsWhileTheSeatIsUnhooked(t *testing.T) {
	r := newLandRig(t)
	r.ok("add --stream s1 --count 2")
	heads := map[string]string{
		"s1-1": r.head("s1-1", "main", "a.txt", "a\n"),
		"s1-2": r.head("s1-2", "main", "b.txt", "b\n"),
	}
	r.queued(heads, "s1-1", "s1-2")
	r.stubGreenGate()
	r.armHook(t)

	more := []string{"--repo-dir", r.clone, "--base", "main"}
	var out bytes.Buffer
	assert.Equal(t, 0, r.a.landRound(context.Background(), "mem:0", more, &out), out.String())
	assert.Contains(t, out.String(), "LAND OK stream=s1 cards=2")
	hookTests.Delete("coordinator")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"), "the cards landed with no hook")
}

// A hand land by the coordinator is still a coordinator command: it is refused by
// the hook gate while the seat is unhooked, nothing is landed, and it lands once
// the seat hooks in.
func TestAHandLandIsStillRefusedUntilHooked(t *testing.T) {
	r := newLandRig(t)
	r.ok("add --stream s1 --count 2")
	heads := map[string]string{
		"s1-1": r.head("s1-1", "main", "a.txt", "a\n"),
		"s1-2": r.head("s1-2", "main", "b.txt", "b\n"),
	}
	r.queued(heads, "s1-1", "s1-2")
	r.stubGreenGate()
	r.armHook(t)

	args := []string{"--repo-dir", r.clone, "--base", "main"}
	var out bytes.Buffer
	code := r.a.cmdLand(args, &out, &out)
	assert.NotEqual(t, 0, code, "a hand land is refused while the seat is unhooked")
	hookTests.Delete("coordinator")
	assert.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"), "nothing was landed")
	hookTests.Store("coordinator", true)

	r.hookLiveUnhooked(t)
	out.Reset()
	code = r.a.cmdLand(args, &out, &out)
	assert.Equal(t, 0, code, out.String())
	assert.Contains(t, out.String(), "LAND OK stream=s1 cards=2")
	hookTests.Delete("coordinator")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"), "the cards landed once the seat hooked in")
}
