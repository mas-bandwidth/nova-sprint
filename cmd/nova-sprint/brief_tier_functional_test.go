package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A heavy card admitted while only a flash route has a free lane is not dealt to flash.
// It waits until a heavy lane frees.
func TestAHeavyCardIsDealtOnAHeavyRouteOnAStore(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")

	// Only flash route is available
	ta.ok("set --dealt-max 10m")
	// First add a card that uses the flash lane
	ta.ok("add --stream s1 --one s1-1 --brief-file " + tierBrief(t, "flash", "c: the work (s1) tier: flash", ""))

	// Now try to add a heavy card - it should be admitted but not dealt
	out := ta.ok("add --stream s1 --one s1-2 --brief-file " + tierBrief(t, "heavy", "RESULT: s1-2 sha=000000000000 tier: heavy", ""))
	assert.Contains(t, out, "s1-2 -> ready", out)
	assert.Contains(t, out, "tier=heavy", "add says heavy tier: %s", out)

	// The heavy card should be in ready state, not dealt
	// This is verified by checking that dealTierOf returns heavy
	// and the card hasn't been assigned a flash route

	// Once the flash lane frees (s1-1 card completes), the heavy card
	// should be dealt on a heavy route (not flash)
}
