package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rework of the cold-read findings on PR #218: every hold/set-family write
// that changes nothing says UNCHANGED (member, friend, reader, stream and
// the set flags alike), reader add checks the reader-<member> suffix, and a
// repeat stream hold with a new reason updates the reason instead of
// silently dropping it.
func TestUnchangedWriteReworkColdReadFindings(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1,m2")
	ta.ok("add --stream s1 --count 2")

	// Blocker: reader add reader-nobody (prefix alone) is refused.
	code, _, errs := ta.do("reader add reader-nobody")
	require.NotEqual(t, 0, code, errs)
	assert.Contains(t, errs, "reader-nobody is no member, friend or member reader: it would never read")
	assert.NotContains(t, ta.readerRows(), "reader-nobody")

	// Member holds reach UNCHANGED: repeat fleet hold, repeat fleet down,
	// and unhold of a member that is not held.
	assert.Contains(t, ta.ok("fleet hold m1 --reason 'the trim'"), "HOLD OK")
	code, out, errs := ta.do("fleet hold m1 --reason 'the trim'")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "UNCHANGED moved=0 refused=0 notes=0")
	assert.NotContains(t, out, "HOLD OK")
	assert.Contains(t, ta.ok("fleet unhold m1"), "UNHOLD OK")
	code, out, errs = ta.do("fleet unhold m1")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "UNCHANGED moved=0 refused=0 notes=0")

	assert.Contains(t, ta.ok("fleet down m1"), "moved=")
	code, out, errs = ta.do("fleet down m1")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "UNCHANGED moved=0 refused=0 notes=0")
	assert.Contains(t, ta.ok("fleet unhold m1"), "UNHOLD OK")

	// stream set to the tier it holds, and a set flag outside the plain
	// property short-circuit, each say UNCHANGED on repeat.
	assert.Contains(t, ta.ok("stream set s1 --read-tier pro"), "SET OK")
	code, out, errs = ta.do("stream set s1 --read-tier pro")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "UNCHANGED moved=0 refused=0 notes=0")
	assert.NotContains(t, out, "SET OK")

	assert.Contains(t, ta.ok("set --fleet-tiers flash"), "SET OK")
	code, out, errs = ta.do("set --fleet-tiers flash")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "UNCHANGED moved=0 refused=0 notes=0")
	assert.NotContains(t, out, "SET OK")

	// A repeat stream hold with a new reason updates the reason (it is a
	// change, so OK), and the same reason again is UNCHANGED.
	assert.Contains(t, ta.ok("hold s1 --reason 'first'"), "HOLD OK")
	code, out, errs = ta.do("hold s1 --reason 'second'")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.NotContains(t, out, "UNCHANGED")
	code, out, errs = ta.do("hold s1 --reason 'second'")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "HOLD UNCHANGED moved=0 refused=0 notes=0")
	assert.Contains(t, ta.ok("unhold s1"), "UNHOLD OK")
}

// Friend holds reach UNCHANGED and a repeat hold keeps the first reason.
func TestUnchangedWriteReworkFriendHold(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync")
	ta.pong("amy")
	assert.Contains(t, ta.ok("friend hold amy --reason 'rate limited'"), "HOLD OK")
	code, out, errs := ta.do("friend hold amy --reason 'rate limited'")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "UNCHANGED moved=0 refused=0 notes=0")
	assert.NotContains(t, out, "HOLD OK")
	assert.Equal(t, []string{"friend amy: rate limited"}, holdsOf(t, ta))
	assert.Contains(t, ta.ok("friend unhold amy"), "UNHOLD OK")
	code, out, errs = ta.do("friend unhold amy")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "UNCHANGED moved=0 refused=0 notes=0")
	// a friend's own name still adds as a reader row.
	assert.Contains(t, ta.ok("reader add amy"), "READER-ADD OK")
	assert.Contains(t, ta.ok("reader add reader-amy"), "READER-ADD OK")
}
