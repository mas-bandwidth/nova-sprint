package card

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestMigratedReasonHolds: every reason a card had in the dropped blocked
// state comes out of the store's round 29 migration (store.HoldingReason) as
// a reason this machine holds (HeldReason), the text kept after the prefix,
// so no card that was held is released to the deal by the migration.
func TestMigratedReasonHolds(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"bounced 3 times, the limit is 3": "held: bounced 3 times, the limit is 3",
		"held: looking at it":             "held: looking at it",
		"missing-route: adb no device":    "missing-route: adb no device",
		"held: stream s1 is held":         "held: held: stream s1 is held",
		"":                                "held: no reason was recorded",
	} {
		got := store.HoldingReason(in)
		assert.Equal(t, want, got, "from %q", in)
		assert.True(t, HeldReason(got), "%q holds", got)
	}
}
