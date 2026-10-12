package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exercise the public store steps, not a full snapshot's AttemptsCap helper.
func TestReviewFinishUsesTheStreamsAttemptCap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, global, stream string
		wantJudgment         bool
	}{
		{"stream raises the global cap", "1", "2", false},
		{"stream lowers the global cap", "2", "1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := routeHarness(t, route("flash-a", "flash"))
			h.addReady("s1", 1, briefOf("flash", ""))
			h.m.SetMaxAttempts(tc.global)
			h.must(SetStep(sprint.SetReq{Streams: []string{"s1"}, Attempts: tc.stream, Who: h.st.Actor}))
			h.must(DealStep(sprint.DealReq{}))
			h.finishAttempt("s1-1", true, "")
			pr := h.snap().Work.Card("s1-1")
			require.Equal(t, 1, pr.Int("attempt"))
			if tc.wantJudgment {
				notes := h.openOf(sprint.NBriefWrong)
				require.Len(t, notes, 1, "first failed attempt reaches the stream cap, regardless of the global cap")
				assert.Contains(t, notes[0].Note.What, "max_attempts "+tc.stream)
			} else {
				assert.Empty(t, h.openOf(sprint.NBriefWrong), "first failed attempt is below the stream cap, regardless of the global cap")
			}
		})
	}
}
