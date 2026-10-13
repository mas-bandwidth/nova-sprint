package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A write that changes nothing says so and never OK (v1.2.17, friction 18):
// unhold of a stream that is not held, a second hold of a held stream, a set
// to the value it holds, and reader up of an up reader each print UNCHANGED
// with the why and notes=0; reader add of an unmatched name is refused.
func TestAWriteThatChangesNothingSaysSoAndNeverOK(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1,m2")
	ta.ok("add --stream s1 --count 2")

	// unhold of a stream that is not held: UNCHANGED, the reason, notes=0.
	code, out, errs := ta.do("unhold s1")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "UNHOLD UNCHANGED moved=0 refused=0 notes=0")
	assert.Contains(t, out, "NOTE nothing changed: it was not held")
	assert.NotContains(t, out, "UNHOLD OK")

	// --json carries the unchanged answer and the why.
	code, out, errs = ta.do("unhold s1 --json")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, `"changed":"no"`)
	assert.Contains(t, out, "it was not held")

	// a second hold of a held stream changes nothing.
	assert.Contains(t, ta.ok("hold s1 --reason 'the trim'"), "HOLD OK")
	code, out, errs = ta.do("hold s1 --reason 'the trim'")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "HOLD UNCHANGED moved=0 refused=0 notes=0")
	assert.Contains(t, out, "NOTE nothing changed:")
	assert.NotContains(t, out, "HOLD OK")
	// a real unhold still says OK.
	assert.Contains(t, ta.ok("unhold s1"), "UNHOLD OK")

	// reader up of an up reader changes nothing; a real release still says OK.
	code, out, errs = ta.do("reader up reader-a")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "READER-UP UNCHANGED moved=0 refused=0 notes=0")
	assert.Contains(t, out, "NOTE nothing changed:")
	assert.NotContains(t, out, "READER-UP OK")
	assert.Contains(t, ta.ok("reader away reader-a"), "READER-AWAY OK readers=reader-a")
	assert.Contains(t, ta.ok("reader up reader-a"), "READER-UP OK readers=reader-a")

	// a set to the value it holds changes nothing.
	assert.Contains(t, ta.ok("set --reads 1"), "SET OK")
	code, out, errs = ta.do("set --reads 1")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "SET UNCHANGED moved=0 refused=0 notes=0")
	assert.Contains(t, out, "NOTE nothing changed:")
	assert.NotContains(t, out, "SET OK")

	// reader add of a name no member, friend or member reader matches is
	// refused with the reason and the remedy, and writes nothing.
	code, _, errs = ta.do("reader add nobody")
	require.NotEqual(t, 0, code, errs)
	assert.Contains(t, errs, "nobody is no member, friend or member reader: it would never read")
	assert.Contains(t, errs, "run: nova-sprint reader add")
	assert.NotContains(t, ta.readerRows(), "nobody")
	// a member reader still adds with OK.
	assert.Contains(t, ta.ok("reader add reader-m1"), "READER-ADD OK")
}
