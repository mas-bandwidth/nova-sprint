package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nova-sprint check exits non-zero when a row's width differs from nova-config's, one line per
// drifting row, both values: a drift is held at the take but loud (the owner, 2026-10-18;
// tla/WidthHard.tla OneSource).
func TestCheckExitsNonZeroOnWidthDrift(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 8)
	ta.ok("fleet sync")
	ta.probeUp("m1")
	require.Equal(t, "8", ta.fleetRows()["m1"]["width"])

	// nova-config now names 4: the copy 8 drifts, and check exits non-zero
	inv.set("m1", 4)
	code, out, errs := ta.do("check")
	assert.NotEqual(t, 0, code, "check exits non-zero on a width drift: %s %s", out, errs)
	assert.Contains(t, out+errs, "rule 14")
	assert.Contains(t, out+errs, "m1: sprint width 8, nova-config width 4")

	// the row agrees again: check is green (a fresh app, so its check reads nova-config fresh)
	ta2, inv2 := syncApp(t)
	inv2.set("m1", 8)
	ta2.ok("fleet sync")
	ta2.probeUp("m1")
	code, out, errs = ta2.do("check")
	assert.Equal(t, 0, code, "check is green when the row agrees: %s %s", out, errs)
}
