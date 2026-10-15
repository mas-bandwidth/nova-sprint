package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpencodeSpendReadoutIsDone verifies opencode spend readout from internal records:
// opencode has no balance endpoint (Known=false), but the spend from its internal cost
// records shows.
func TestOpencodeSpendReadoutIsDone(t *testing.T) {
	t.Parallel()
	f := NewTable(Fleet)
	f.SetProp(PropProviderBalance("opencode"), ProviderBalance{Provider: "opencode", Known: false, At: coverT0,
		Note: "opencode Zen publishes no balance endpoint", SpendHour: 1234.56}.value())
	rows := ProviderRows([]Route{{Name: "flash-oc", Tier: "flash", Provider: "opencode", Model: "m", Enabled: true}}, f, coverT0)
	require.Len(t, rows, 1)
	assert.Equal(t, "opencode", rows[0].Name)
	assert.Equal(t, "unknown", rows[0].Balance)
	assert.Contains(t, rows[0].Note, "opencode Zen publishes no balance endpoint")
	// spend should show the value from internal records
	assert.InDelta(t, 1234.56, rows[0].SpendHour, 1e-9)
}
