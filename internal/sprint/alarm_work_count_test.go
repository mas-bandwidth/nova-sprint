package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheFleetAlarmIgnoresReadsCrossingTheThreshold(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	for _, m := range []string{"m1", "m2"} {
		w.setWidth(m, 4)
		w.up(m)
	}
	w.s.Work.SetProp(PropAlarmFleet, "50")
	w.s.Work.Put(&Card{ID: "s1-wait", Row: "s1", Col: Waiting, Rev: 1, Fields: map[string]string{"kind": "primary"}})
	w.s.Fleet.Put(&Card{ID: "work.w1", Row: "m1", Col: Working, Rev: 1, Fields: map[string]string{"kind": "work"}})

	applyAlarms := func() {
		t.Helper()
		p, _ := tickAlarms(w.s, TickReq{})
		w.must(p)
	}
	applyAlarms()
	require.Len(t, w.notesOf(NAlarmFleet), 1, "one work card is below half the fleet width")

	// Three reads make the raw fleet cell count reach four of eight, exactly the
	// threshold, while the one work card stays fixed below it.
	for i := range 3 {
		w.s.Fleet.Put(&Card{ID: "read-" + itoa(i), Row: "m1", Col: Working, Rev: 1, Fields: map[string]string{"kind": "read"}})
	}
	applyAlarms()
	assert.Empty(t, w.notesOf(NAlarmCleared), "reads crossing the threshold do not clear the work alarm")
	require.Len(t, w.notesOf(NAlarmFleet), 1, "the same work alarm episode remains raised")

	for i := range 3 {
		id := "read-" + itoa(i)
		w.s.Fleet.Put(&Card{ID: id, Row: "m1", Col: DoneOK, Rev: 2, Fields: map[string]string{"kind": "read"}})
	}
	applyAlarms()
	assert.Empty(t, w.notesOf(NAlarmCleared), "reads falling back below the threshold do not clear")
	assert.Len(t, w.notesOf(NAlarmFleet), 1, "reads falling back do not start a second episode")
}

func TestTheIdleFleetCountsWorkNotReads(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	for _, m := range []string{"m1", "m2"} {
		w.setWidth(m, 4)
		w.up(m)
	}
	w.s.Fleet.Put(&Card{ID: "work.w1", Row: "m1", Col: Working, Rev: 1, Fields: map[string]string{"kind": "work"}})
	working, width := FleetWorking(w.s)
	require.Equal(t, 1, working)
	require.Equal(t, 8, width)

	for i := range 3 {
		w.s.Fleet.Put(&Card{ID: "read-" + itoa(i), Row: "m1", Col: Working, Rev: 1, Fields: map[string]string{"kind": "read"}})
	}
	working, width = FleetWorking(w.s)
	assert.Equal(t, 1, working, "reads crossing 50 percent leave fleet work unchanged")
	assert.Equal(t, 8, width)

	for i := range 3 {
		id := "read-" + itoa(i)
		w.s.Fleet.Put(&Card{ID: id, Row: "m1", Col: DoneOK, Rev: 2, Fields: map[string]string{"kind": "read"}})
	}
	working, width = FleetWorking(w.s)
	assert.Equal(t, 1, working, "reads leaving 50 percent leave fleet work unchanged")
	assert.Equal(t, 8, width)
}
