package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSettingsPrintsEverySettingWithItsValueAndDefault is the DONE-WHEN of
// settings-prints-every-setting-with-its-default: a fresh sprint prints every
// setting at its default with set=no; a set moves its line; a clear returns
// it; set with no flags prints the same lines instead of refusing.
func TestSettingsPrintsEverySettingWithItsValueAndDefault(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1:2")

	fresh := ta.ok("settings")
	require.Contains(t, fresh, "SETTING read-tier value=default default=default set=no")
	require.Contains(t, fresh, "SETTINGS OK count=")
	assert.Contains(t, fresh, "epoch=0")

	ta.ok("set --read-tier pro")
	setOut := ta.ok("settings")
	require.Contains(t, setOut, "SETTING read-tier value=pro default=default set=yes")

	ta.ok("clear --confirm sprint")
	cleared := ta.ok("settings")
	require.Contains(t, cleared, "SETTING read-tier value=default default=default set=no")

	noFlags := ta.ok("set")
	assert.Equal(t, cleared, noFlags, "set with no flags prints the settings, exit 0")

	// every SETTING line carries value, default and set; count matches lines.
	lines := strings.Split(strings.TrimSpace(cleared), "\n")
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "SETTING ") {
			n++
			assert.Contains(t, l, " value=")
			assert.Contains(t, l, " default=")
			assert.Contains(t, l, " set=")
		}
	}
	require.Contains(t, cleared, "SETTINGS OK count="+itoa(n)+" ")
}
