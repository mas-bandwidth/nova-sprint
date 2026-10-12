//go:build functional

package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

// The landing stall on a real store (docs/SPEC-SPRINT.md, the judgments section; the owner,
// 2026-10-11: landed sat at +0/30m while cards waited in review and no judgment reached the
// seat). A private redis-server, the machine's own clock, the bound set to one second: cards
// that wait in review with no landing raise "landing stalled" to the coordinator with its
// breakdown, and the landing that follows closes it with "landing again" and pushes "landed
// <card>" to the coordinator.
func TestALandingStallIsJudgedAndClearedByALandingOnTheStore(t *testing.T) {
	t.Parallel()
	wall := time.Now()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	// A scrubbed environment: the private store, the actor, and a home and config directory of
	// the test's own; no other variable of the machine's (no NOVA_SPRINT_SERVER, no seat
	// record) is seen. Every verb names the private store with --redis, which never forwards
	// (forward.go, server), and the app's forward fails the test: no verb can reach a live
	// sprint server from here.
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator",
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	require.Empty(t, func() string { s, _ := a.namedServer(); return s }(), "no sprint server is named in the test's environment")
	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := a.run(append(strings.Fields(line), "--redis", addr), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}
	notesOf := func(typ string) []sprint.Note {
		t.Helper()
		_, cm := a.verbSetup("log")
		st, err := a.store(*cm)
		require.NoError(t, err)
		notes, _, err := st.B.NotesSince(context.Background(), "", 10000)
		require.NoError(t, err)
		var out []sprint.Note
		for _, n := range notes {
			if n.Type == typ {
				out = append(out, n)
			}
		}
		return out
	}
	stallOpen := func() bool {
		t.Helper()
		_, cm := a.verbSetup("inbox")
		st, err := a.store(*cm)
		require.NoError(t, err)
		groups, err := st.OpenGroups(context.Background())
		require.NoError(t, err)
		for _, g := range groups {
			if g.Type == sprint.NLandingStall {
				return true
			}
		}
		return false
	}

	for _, l := range []string{"init --readers reader-a,reader-b --members m1", "add --stream s1 --count 2", "fleet beat m1", "start",
		"set --landing-stall-bound 1s",
		"tick", "tick", // the fleet update brings m1 up; the next pump deals
		"take --as m1 --limit 5 --epoch 0", "finish --as m1 s1-1.w1@1 s1-2.w1@1 --epoch 0",
		"queue --as reader-a", "queue --as reader-b", // the readers ask for their queues: they are up
		"tick"} { // the pump moves both to review; the readers are asked
		run(l)
	}
	require.Empty(t, notesOf(sprint.NLandingStall), "no stall before the bound has run")

	// 1. the cards wait in review and nothing lands: within a few bounds of running time the
	// tick end raises the judgment, once
	for i := 0; i < 10 && !stallOpen(); i++ {
		time.Sleep(1100 * time.Millisecond)
		run("tick")
	}
	require.True(t, stallOpen(), "the stall judgment is open in the coordinator's inbox")
	stalls := notesOf(sprint.NLandingStall)
	require.Len(t, stalls, 1, "one judgment for the episode")
	j := stalls[0]
	require.Equal(t, sprint.Judgment, j.Kind)
	require.Equal(t, "coordinator", j.To)
	require.Contains(t, j.What, "2 in review and 0 merging", "the breakdown counts what waits: %s", j.What)
	require.Contains(t, j.What, "reads out:", "the breakdown names the reads out: %s", j.What)
	require.Contains(t, j.What, "the lander's last refusal: none", "the breakdown names the lander's refusal: %s", j.What)
	require.Contains(t, j.What, "CI green", "the breakdown names the base's CI: %s", j.What)

	// 2. the reads come in, the coordinator accepts, and the lander records the landing: the
	// next tick ends the episode and the landing reached the coordinator as it happened
	for _, l := range []string{"read --as reader-a --begin --limit 10 --epoch 0", "read --as reader-a --ok --limit 10 --epoch 0",
		"read --as reader-b --begin --limit 10 --epoch 0", "read --as reader-b --ok --limit 10 --epoch 0",
		"accept s1-1 s1-2"} {
		run(l)
	}
	out := recordMerge(t, a, "s1", 10, "0")
	require.Contains(t, out, "MERGE OK", "the landing:\n%s", out)
	landed := notesOf(sprint.NLanding)
	require.Len(t, landed, 1, "a batch of landings is one push")
	require.Equal(t, "coordinator", landed[0].To)
	require.Contains(t, landed[0].What, "landed s1-1 (stream s1")
	require.Contains(t, landed[0].What, "landed s1-2 (stream s1")
	run("tick")
	again := notesOf(sprint.NLandingAgain)
	require.Len(t, again, 1, "the landing clears the stall with one happened note")
	require.Equal(t, "coordinator", again[0].To)
	require.Contains(t, again[0].What, "landing again after")
	require.False(t, stallOpen(), "the stall judgment is closed by the landing")
	t.Logf("wall time %s", time.Since(wall).Round(time.Millisecond))
}
