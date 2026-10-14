package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A width is nova-config's alone: where shows each member's machine:<m> width
// in the fleet table's width column beside working, and --json carries it;
// fleet up <m> --width <n> and init --members <m>:<n> refuse, exit 2, nothing
// written, naming nova-config machine set. (The rig's configWidths writes the
// machine:<m> width for a line that names one; asWritten runs it as typed.)
func TestTheWidthIsSetByInitAndFleetUpAndShown(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:64,m2:3,m3")
	ta.ok("fleet up m3 --width 8")
	out := ta.ok("where")
	require.Contains(t, out, "fleet | ready | working | width |", "the fleet table has no width beside working")
	var v struct {
		Tables map[string]map[string]map[string]string `json:"tables"`
	}
	require.NoError(t, json.Unmarshal([]byte(ta.ok("where --json")), &v))
	fleet := v.Tables["fleet"]
	require.Equal(t, "64", fleet["m1"]["width"], "the widths: %v", fleet)
	require.Equal(t, "3", fleet["m2"]["width"], "the widths: %v", fleet)
	require.Equal(t, "8", fleet["m3"]["width"], "the widths: %v", fleet)
	// nova-config's width 0 drains the member (fleet_drain_test.go)
	ta.m.SetMachineWidth("m3", "0")
	ta.ok("fleet up m3") // a step refreshes the display cells from nova-config
	require.NoError(t, json.Unmarshal([]byte(ta.ok("where --json")), &v))
	require.Equal(t, "0", v.Tables["fleet"]["m3"]["width"], "the widths: %v", v.Tables["fleet"])
	// every width the verbs are given is refused, before anything is written
	for _, line := range []string{"fleet up m3 --width 8", "fleet up m4 --width 0", "fleet up m4 --width 1025", "fleet up m4 --width x"} {
		code, _, errs := ta.asWritten(line)
		assert.Equal(t, 2, code, "%s: exit %d, %s", line, code, errs)
		assert.Contains(t, errs, "a width is nova-config's alone", "%s: exit %d, %s", line, code, errs)
		assert.Contains(t, errs, "nova-config machine set", "%s: exit %d, %s", line, code, errs)
	}
	require.NoError(t, json.Unmarshal([]byte(ta.ok("where --json")), &v))
	require.Equal(t, "0", v.Tables["fleet"]["m3"]["width"], "a refused fleet up wrote a width: %v", v.Tables["fleet"])
	require.NotContains(t, v.Tables["fleet"], "m4", "a refused fleet up added a member: %v", v.Tables["fleet"])
	nb := newTestApp(t)
	code, _, errs := nb.asWritten("init --members m1:4")
	assert.Equal(t, 2, code, "init --members m1:4: exit %d, %s", code, errs)
	assert.Contains(t, errs, "a width is nova-config's alone", "init --members m1:4: exit %d, %s", code, errs)
}

// The fleet table's footer sums the width column of the members up, the
// fleet's total width: eight machines of 64 up total 512; a machine whose width
// changes moves the total (TestTheFleetFooterSumsOnlyTheMembersUp: a member
// held or down adds nothing).
func TestTheFleetFooterTotalsTheWidths(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.live = []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7", "m8"}
	ta.ok("init --readers reader-a,reader-b --members m1,m2,m3,m4,m5,m6,m7,m8")
	ta.ok("start")
	ta.ok("tick") // presence brings the eight beating members up
	footer := func() string {
		out := ta.ok("where")
		i := strings.Index(out, "fleet |")
		require.GreaterOrEqual(t, i, 0, "where has no fleet table:\n%s", out)
		lines := strings.Split(strings.TrimRight(strings.SplitN(out[i:], "\n\n", 2)[0], "\n"), "\n")
		return lines[len(lines)-1]
	}
	require.Contains(t, footer(), "|   512 |", "eight machines of 64 do not total 512 in the footer")
	ta.ok("fleet up m1 --width 8")
	require.Contains(t, footer(), "|   456 |", "the footer after m1 narrows to 8")
}
