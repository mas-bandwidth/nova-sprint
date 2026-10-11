package sprint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// An alarm is never off (the owner, 2026-10-11: "Turning off alarms, warnings is forbidden
// from now on."): set refuses off for each of the four, with the rule and the set that
// restores the working threshold, and writes nothing; the alarm keeps its threshold.
func TestAnAlarmIsNeverOff(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		flag, working string
		req           sprint.SetReq
	}{
		{"--alarm-review", "40", sprint.SetReq{AlarmReview: "off"}},
		{"--alarm-ready", "on", sprint.SetReq{AlarmReady: "off"}},
		{"--alarm-merging", "15", sprint.SetReq{AlarmMerging: "off"}},
		{"--alarm-fleet", "50", sprint.SetReq{AlarmFleet: "off"}},
	} {
		t.Run(c.flag, func(t *testing.T) {
			t.Parallel()
			r := newAlarmRig(t)
			before := r.snap().Work.Props()
			c.req.Who = "coordinator"
			res, err := r.st.Run(r.ctx, store.SetStep(c.req))
			require.NoError(t, err)
			require.Len(t, res.Refused, 1, "%s off is refused", c.flag)
			why := res.Refused[0].Why
			assert.Contains(t, why, c.flag+" off is refused")
			assert.Contains(t, why, sprint.AlarmOffRefused, "the refusal says the rule")
			assert.Contains(t, why, "nova-sprint set "+c.flag+" default", "and the set that restores the working threshold")
			assert.Equal(t, before, r.snap().Work.Props(), "a refused set writes nothing")
			for _, d := range sprint.DesignSettings(r.snap()) {
				if d.Flag == c.flag {
					assert.Equal(t, c.working, d.Value, "%s keeps its working value", c.flag)
				}
			}
		})
	}
}
