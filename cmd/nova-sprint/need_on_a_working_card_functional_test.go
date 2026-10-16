//go:build functional

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A need added to a working card is not a broken invariant (docs/SPEC-SPRINT.md
// section 9, rule 11): the card was dealt before the need was recorded, so its
// attempt is legal until it ends. The brief verb and rule 11 agree: the card
// names the state ("working, waits for <need> after this attempt"), check raises
// no rule-11 violation, the attempt finishes, and the card then waits for the
// need. The store is the test's own redis, named with --redis on every verb, and
// the app reads no variable of the process environment (HOME and
// XDG_CONFIG_HOME are the test's temporary directories).
func TestANeedAddedToAWorkingCardIsNotABrokenInvariant(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	ta := newFunctionalTestApp(t, addr)
	run := func(args ...string) string {
		t.Helper()
		return ta.runVerb(args...)
	}
	dir := t.TempDir()
	run("init", "--readers", "reader-a,reader-b", "--members", "m1")
	// work is first in line, so the deal cuts it; dep stays ready, not landed
	run("add", "--one", "--stream", "a", "work", "--brief-file", writeHeaderBrief(t, dir, "work", "-", "internal/work.go"))
	run("add", "--one", "--stream", "a", "dep", "--brief-file", writeHeaderBrief(t, dir, "dep", "-", "internal/dep.go"))
	run("fleet", "beat", "m1")
	run("start")
	run("tick") // the fleet update brings m1 up
	run("tick") // the pump deals to it
	require.Contains(t, run("card", "work"), "working", "work must be dealt before the need is added")
	run("take", "--as", "m1", "--limit", "100") // the member takes the dealt card: it works on it

	// the coordinator adds DEPENDS-ON: dep to the working card with brief --brief-file.
	// The body changes too, so the edit is an in-place brief, as the working card's is.
	lead := "RESULT: work sha=000000000000 tier: flash\nKIND: fix\nDEPENDS-ON: dep\nPATHS: internal/work.go\n\nFix work differently."
	path := filepath.Join(t.TempDir(), "work.md")
	require.NoError(t, os.WriteFile(path, []byte(passingBrief(lead)), 0o600))
	out := run("brief", "work", "--brief-file", path, "--replace-in-flight")
	assert.Contains(t, out, "brief edited in place", "a working card takes the brief for its next attempt")
	assert.Contains(t, out, "working, waits for dep after this attempt", "the brief names the state")
	run("tick") // the machine applies the brief edit: the card names the need

	// no invariant judgment: check stays OK, and the state is named on the card
	assert.Contains(t, run("check"), "CHECK OK", "a need added to a working card is not a broken invariant")
	assert.Contains(t, run("card", "work"), "working, waits for dep after this attempt")

	// the attempt finishes; the card then waits for the need
	run("finish", "--as", "m1", "work.w1@1", "--epoch", "0", "--report", "done")
	pr := run("card", "work")
	assert.Contains(t, pr, "waiting", "the card waits for the need once its attempt ends")
	assert.Contains(t, pr, "dep", "it waits for dep")
	assert.Contains(t, run("check"), "CHECK OK", "check stays OK after the finish")
}
