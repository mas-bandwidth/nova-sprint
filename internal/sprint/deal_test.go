package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadDealRoundKeepsAnIndependentTierCursor pins the read-card deal's persisted fleet
// cursor (docs/SPEC-SPRINT.md section 1): each tier starts from its own epoch property and
// a placement moves the tier's cursor past the chosen reader.
func TestReadDealRoundKeepsAnIndependentTierCursor(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Fleet: NewTable(Fleet)}
	s.Fleet.SetProp(PropReadDealIndex("flash"), "7")
	units := []readUnit{{name: "m1"}, {name: "m2"}, {name: "m3"}, {name: "m4"}}
	flash := readDealRound(s, "flash", units)
	require.Equal(t, "m4", flash.scan(func(string) bool { return true }), "counter seven starts at the fourth reader")
	flash.moved("m4")
	require.Equal(t, "8", flash.value(), "the placement advances one past m4")
	pro := readDealRound(s, "pro", units)
	require.Equal(t, "m1", pro.scan(func(string) bool { return true }), "pro has an independent cursor")
}
