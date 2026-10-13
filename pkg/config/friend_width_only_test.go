package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend row is width only: no slots field and no ceiling the machine sets
// over her. The owner, 2026-10-13: "there is no ceiling for friend slots
// per-machine. i never asked for one!" A friend says how wide she is (stella
// is width 16) and that is the whole row; a machine's ceiling bounds only its
// benches, so a width far above a machine's slots is still accepted, with no
// refusal.
func TestAFriendRowIsWidthOnlyAndNoCeilingRefusesIt(t *testing.T) {
	t.Parallel()

	friend, ok := Lookup(KindFriend)
	require.True(t, ok, "the friend kind is registered")
	_, hasSlots := friend.Field("slots")
	assert.False(t, hasSlots, "the friend kind has no slots field")
	for _, f := range friend.Fields {
		assert.NotEqual(t, "slots", f.Name, "no friend field is named slots")
	}

	machine, ok := Lookup(KindMachine)
	require.True(t, ok, "the machine kind is registered")
	_, hasMachineSlots := machine.Field("slots")
	require.True(t, hasMachineSlots, "the machine kind still carries its ceiling in slots")

	// The machine's ceiling is 1; stella is width 16. Both rows validate and
	// are accepted: nothing refuses the friend for outgrowing the machine.
	_, err := machine.NewRow("benchbox", map[string]string{
		"user":  "glenn",
		"seat":  "benchbox",
		"slots": "1",
	})
	require.NoError(t, err, "a machine with slots 1 is accepted")

	row, err := friend.NewRow("stella", map[string]string{
		"tiers": "flash,pro,heavy",
		"width": "16",
	})
	require.NoError(t, err, "a friend of width 16 under no ceiling is accepted, not refused")
	assert.Equal(t, "16", row.Fields["width"], "the width is written as given")
	_, wroteSlots := row.Fields["slots"]
	assert.False(t, wroteSlots, "the written friend row holds no slots")

	require.NoError(t, friend.Check(row), "the friend's own check refuses nothing")
}
