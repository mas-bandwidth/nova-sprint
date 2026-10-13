package store

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// lanesDisabledShipped is sprint.LanesDisabled as the package ships it, read before
// TestMain clears it for the lanes' own tests.
var lanesDisabledShipped bool

func TestMain(m *testing.M) {
	lanesDisabledShipped = sprint.LanesDisabled
	// the lanes are disabled until v1.3; their tests keep the machinery tested until it
	// is replaced, and TestTheBenchLockIsDisabledUntilV13 pins the disabled path
	sprint.LanesDisabled = false
	os.Exit(m.Run())
}

// The bench lock is disabled until v1.3 (Glenn, 2026-10-13: "Stop with these bench
// locks. Verboten." "Disable until then. Not a sanctioned or stable feature. Not to be
// used."): the package ships with sprint.LanesDisabled set, and then every take of one
// machine's one Go lane is granted at once (never queued, so it never blocks), a give
// gives nothing back, and the lanes record is never created. A bad name is still refused.
//
// Not parallel: it sets sprint.LanesDisabled, and a parallel test runs only after every
// sequential one has returned.
func TestTheBenchLockIsDisabledUntilV13(t *testing.T) {
	require.True(t, lanesDisabledShipped, "the lanes ship disabled until v1.3")
	sprint.LanesDisabled = true
	defer func() { sprint.LanesDisabled = false }()
	h := newHarness(t)
	for _, who := range []string{"a", "b", "c", "d"} {
		ans, err := h.st.LaneStep(h.ctx, sprint.LaneGo, "m1", who, false)
		require.NoError(t, err)
		assert.True(t, ans.Granted, "%s is granted at once on a one-lane machine", who)
		assert.Zero(t, ans.Place, "%s is never queued", who)
	}
	ans, err := h.st.LaneStep(h.ctx, sprint.LaneGo, "m1", "a", true)
	require.NoError(t, err)
	assert.False(t, ans.Gave, "a give gives nothing back: nothing was held")
	_, ok, err := h.m.GetKey(h.ctx, laneKey(sprint.LaneGo))
	require.NoError(t, err)
	assert.False(t, ok, "the lanes record is never created")
	rows, err := h.st.LaneRows(h.ctx)
	require.NoError(t, err)
	assert.Empty(t, rows)
	_, err = h.st.LaneStep(h.ctx, "rust", "m 1", "a,b", false)
	assert.Error(t, err, "a bad kind, machine or worker is still refused")
}
