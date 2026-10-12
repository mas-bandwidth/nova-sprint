//go:build functional

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A brief edit keeps what a verb set, on a real store (docs/SPEC-SPRINT.md, the
// needs and the bound; the seat's 2026-10-11 hour of relinks undone four times):
// b is admitted needing a-1, relink re-points it to a-2, and `brief --brief-file`
// with the text's stale DEPENDS-ON: a-1 keeps a-2 and says so on a NOTE line; a
// DEPENDS-ON-only edit that names a new card c adds it beside the kept twin; and
// on a card dealt, a DEPENDS-ON-only edit leaves the bound standing and says
// `bound: same task (<n> attempts stand)`. The store is the test's own redis,
// named with --redis on every verb, and the app reads no variable of the process
// environment: HOME and XDG_CONFIG_HOME are the test's temporary directories.
func TestABriefEditKeepsARelinkedNeedOnTheStore(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	// a scrubbed env: the app reads only these names (no NOVA_* but the actor), its
	// home and config directory are the test's own, and every verb names its store
	// with --redis, so no seat record or server on the host can take a verb
	// (forward.go: a verb given its own --redis runs here)
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_ACTOR": "coordinator", "HOME": home, "XDG_CONFIG_HOME": t.TempDir()}
	a := newApp(func(k string) string { return env[k] })
	a.home = func() (string, error) { return home, nil }
	defer a.close()
	run := func(args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := a.run(append(args, "--redis", addr), &out, &errb)
		require.Equal(t, 0, code, "%v: %d %s%s", args, code, out.String(), errb.String())
		return out.String()
	}
	dir := t.TempDir()
	run("init", "--readers", "reader-a,reader-b", "--members", "m1")
	run("add", "--one", "--stream", "a", "d", "--brief-file", writeHeaderBrief(t, dir, "d", "-", "internal/d.go"))
	run("add", "--one", "--stream", "a", "a-1", "--brief-file", writeHeaderBrief(t, dir, "a-1", "-", "internal/a1.go"))
	run("add", "--one", "--stream", "a", "a-2", "--brief-file", writeHeaderBrief(t, dir, "a-2", "-", "internal/a2.go"))
	run("add", "--one", "--stream", "a", "c", "--brief-file", writeHeaderBrief(t, dir, "c", "-", "internal/c.go"))
	run("add", "--one", "--stream", "a", "b", "--brief-file", writeHeaderBrief(t, dir, "b", "a-1", "internal/b.go"))
	assert.Contains(t, run("relink", "a-1", "a-2", "--reason", "a-1 split"), "b needs a-1 -> a-2")

	// the text dumped from the card still says DEPENDS-ON: a-1: the relink record wins
	out := run("brief", "b", "--brief-file", writeHeaderBrief(t, t.TempDir(), "b", "a-1", "internal/b.go,internal/b_test.go"))
	assert.Contains(t, out, "NOTE needs kept: a-2 (relinked from a-1 ", "the brief verb names the need it kept against the text")
	// a DEPENDS-ON-only edit naming a new card adds it beside the kept twin
	out = run("brief", "b", "--brief-file", writeHeaderBrief(t, t.TempDir(), "b", "a-1,c", "internal/b.go,internal/b_test.go"))
	assert.Contains(t, out, "MOVED b DEPENDS-ON replaced: needs a-2,c ", "b keeps a-2 and gains c, never the stale a-1")

	// a card dealt: a DEPENDS-ON-only edit is the same task, so the bound stands
	run("fleet", "beat", "m1")
	run("start")
	run("tick") // the fleet update, last in the tick, brings m1 up
	run("tick") // the pump deals to it
	out = run("brief", "d", "--brief-file", writeHeaderBrief(t, t.TempDir(), "d", "c", "internal/d.go"))
	assert.Contains(t, out, "NOTE bound: same task (1 attempts stand)", "a routing-only edit keeps the bound count and says so")

	// brief --tier --brief-file requires --reason; refuses without it, writes nothing
	run("add", "--one", "--stream", "a", "e", "--brief-file", writeHeaderBrief(t, dir, "e", "-", "internal/e.go"))
	var out2, errb2 bytes.Buffer
	code := a.run(append([]string{"brief", "e", "--tier", "heavy", "--brief-file", writeBrief(t, "the brief")}, "--redis", addr), &out2, &errb2)
	assert.Equal(t, 2, code, "brief --tier --brief-file without --reason")
	assert.Contains(t, errb2.String(), "--reason")
	tierField := run("card", "e", "--fields")
	assert.NotContains(t, tierField, "tier:", "a refused re-tier with --brief-file writes nothing")

	// brief --tier --brief-file with --reason works; reason visible on the card
	out2.Reset()
	errb2.Reset()
	code = a.run(append([]string{"brief", "e", "--tier", "heavy", "--brief-file", writeBrief(t, "the brief"), "--reason", "a heavy card"}, "--redis", addr), &out2, &errb2)
	assert.Equal(t, 0, code, "brief --tier --brief-file with --reason")
	assert.Contains(t, out2.String(), "reason: a heavy card")
	run("card", "e", "--fields")
}
