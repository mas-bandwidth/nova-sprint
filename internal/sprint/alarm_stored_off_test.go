package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An off a store holds from before set refused it is no off: the alarm runs at its working
// threshold, and the design settings' warning says the property holds off and what restores
// it, until a set clears it.
func TestAnAlarmStoredOffReadsAsItsWorkingThresholdAndIsWarnedOf(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Work: NewTable(Work), Fleet: NewTable(Fleet), Coordinator: "boss"}
	s.Work.SetRows([]string{"s1"})
	s.Work.SetProps(map[string]string{PropAlarmReview: AlarmOff, PropAlarmReady: AlarmOff})
	for i := range AlarmReviewDefault + 1 {
		s.Work.Put(&Card{ID: "c" + itoa(i), Row: "s1", Col: Review, Fields: map[string]string{"attempt": "1"}})
	}
	s.Work.Put(&Card{ID: "w", Row: "s1", Col: Waiting})

	assert.Equal(t, AlarmReviewDefault, s.alarmSetting(PropAlarmReview), "a stored off reads as the working threshold")
	facts := alarmFacts(s)
	require.Contains(t, facts, NAlarmReview, "41 in review raises the review alarm a stored off would have silenced")
	require.Contains(t, facts, NAlarmReady, "nothing ready with a card waiting raises the ready alarm")

	what := SettingsOffWhat(DesignSettings(s))
	assert.Contains(t, what, "--alarm-review is off (written before off was refused, read as 40), working value 40 (restore: nova-sprint set --alarm-review 40)")
	assert.Contains(t, what, "--alarm-ready is off (written before off was refused, read as on), working value on")

	s.Work.SetProps(map[string]string{PropAlarmReview: "40", PropAlarmReady: "on"})
	assert.Empty(t, SettingsOffWhat(DesignSettings(s)), "a set that clears it: nothing to say")
}
