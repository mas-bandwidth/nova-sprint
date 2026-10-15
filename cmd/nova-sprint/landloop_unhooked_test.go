package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/hook/hooktest"
	"github.com/stretchr/testify/assert"
)

// The server's own land loop lands accepted cards as machine work, not as a
// coordinator command: it must run whether or not the seat is hooked (a server
// restart, a dropped stream, a seat away). The hook gate is kept for the
// coordinator commands a person or seat runs, so a hand land by the coordinator
// is still refused until the seat hooks in.

// armHook arms the hook gate for the coordinator: a coordinator verb is then refused while
// its nova-hook record (a hooktest rig here) is not proven as the coordinator.
func (r *landRig) armHook(t *testing.T) *hooktest.Rig {
	t.Helper()
	hookTests.Store("coordinator", true)
	t.Cleanup(func() { hookTests.Delete("coordinator") })
	rig := hooktest.New(t)
	r.a.hookStoreFn = func(context.Context) (hook.Store, error) { return rig.Store, nil }
	return rig
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
	rig := r.armHook(t)

	args := []string{"--repo-dir", r.clone, "--base", "main"}
	var out bytes.Buffer
	code := r.a.cmdLand(args, &out, &out)
	assert.NotEqual(t, 0, code, "a hand land is refused while the seat is unhooked")
	hookTests.Delete("coordinator")
	assert.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"), "nothing was landed")
	hookTests.Store("coordinator", true)

	rig.Hook(t, "coordinator", hook.Coordinator)
	out.Reset()
	code = r.a.cmdLand(args, &out, &out)
	assert.Equal(t, 0, code, out.String())
	assert.Contains(t, out.String(), "LAND OK stream=s1 cards=2")
	hookTests.Delete("coordinator")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"), "the cards landed once the seat hooked in")
}
