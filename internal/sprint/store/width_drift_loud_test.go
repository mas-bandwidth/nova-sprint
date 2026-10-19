package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The one width is nova-config's (tla/WidthHard.tla OneSource): check counts a member or friend
// row whose width differs, one line per drifting row, both values (the owner, 2026-10-18).
func TestCheckCountsWidthDrift(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.now = time.Unix(1000, 0).UTC() // the test's clock, from 1000 s: no real time is read
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 8}))
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2, Class: "flash"}})
	require.NoError(t, err)
	h.up("amy")

	// no config read is held: no width drift is judged (never act on a missing read)
	rep, _, err := h.st.Check(h.ctx, 3)
	require.NoError(t, err)
	for _, v := range rep.Violations {
		assert.NotEqual(t, 14, v.Rule, "no width-drift violation without a config read: %v", v)
	}

	// member m1 copy 8 vs nova-config 4; friend amy seat 2 vs nova-config 4
	h.st.Widths = func(context.Context) (map[string]int, bool) { return map[string]int{"m1": 4}, true }
	h.st.FriendWidths = func(context.Context) (map[string]int, bool) { return map[string]int{"amy": 4}, true }
	rep, _, err = h.st.Check(h.ctx, 3)
	require.NoError(t, err)
	var details []string
	for _, v := range rep.Violations {
		if v.Rule == 14 {
			details = append(details, v.Detail)
		}
	}
	require.Len(t, details, 2, "one line per drifting row: %v", details)
	assert.Contains(t, strings.Join(details, "\n"), "m1: sprint width 8, nova-config width 4")
	assert.Contains(t, strings.Join(details, "\n"), "amy: sprint width 2, nova-config width 4")

	// the rows agree again: nothing is counted
	h.st.Widths = func(context.Context) (map[string]int, bool) { return map[string]int{"m1": 8}, true }
	h.st.FriendWidths = func(context.Context) (map[string]int, bool) { return map[string]int{"amy": 2}, true }
	rep, _, err = h.st.Check(h.ctx, 3)
	require.NoError(t, err)
	for _, v := range rep.Violations {
		assert.NotEqual(t, 14, v.Rule, "no width-drift violation when the rows agree: %v", v)
	}
}
