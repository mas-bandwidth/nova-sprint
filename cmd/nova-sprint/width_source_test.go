package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A member's width is nova-config's alone (the owner, 2026-10-18: "all fleet machines and
// friends must follow the one true definition of width, in nova-config. No exceptions.";
// tla/WidthHard.tla OneSource, witness MCWidthHardBrokenSecondSource): fleet up --width is
// refused a width that is not the member's nova-config machine row's, writing nothing and
// naming the config verbs; the config's own width is taken, and --width 0 still drains.
func TestFleetUpWidthIsRefusedWhenItIsNotNovaConfigs(t *testing.T) {
	t.Parallel()
	ta, inv := syncApp(t)
	inv.set("m1", 8)
	ta.ok("fleet sync")
	ta.probeUp("m1")
	require.Equal(t, "8", ta.fleetRows()["m1"]["width"])

	code, out, errs := ta.do("fleet up m1 --width 16")
	assert.NotEqual(t, 0, code, "a width that is not nova-config's is refused: %s %s", out, errs)
	assert.Contains(t, out+errs, "--width 16 is not m1's nova-config width (nova-config names 8)")
	assert.Contains(t, out+errs, "nova-config machine set m1 --width 16, then nova-config apply")
	assert.Equal(t, "8", ta.fleetRows()["m1"]["width"], "nothing was changed")

	// a member nova-config does not name is refused any width
	code, out, errs = ta.do("fleet up m9 --width 4")
	assert.NotEqual(t, 0, code, "%s %s", out, errs)
	assert.Contains(t, out+errs, "m9 has no nova-config machine row")

	// the config's own width is taken, and the drain stays
	ta.ok("fleet up m1 --width 8")
	assert.Equal(t, "8", ta.fleetRows()["m1"]["width"])
	ta.ok("fleet up m1 --width 0")
	assert.Equal(t, "0", ta.fleetRows()["m1"]["width"], "--width 0 drains")
}
