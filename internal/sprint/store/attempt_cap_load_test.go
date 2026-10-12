package store

import (
	"slices"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
)

// asksCapWithoutStreamControls is a step that asks the attempt cap (Step.Bars: finish,
// read, merge, set) and loads no merge table: sprint.AttemptsCap then never sees a
// stream's own cap (its control card is a merge table card) and runs the card under the
// sprint-wide one.
func asksCapWithoutStreamControls(s Step) bool {
	return s.Bars && !slices.Contains(s.Load, sprint.Merge)
}

// Every step that asks the attempt cap loads the streams' control cards, so a stream's
// own cap (stream set --attempts) decides its cards in the finish and the reads as it
// does in the tick. The finish and the reads loaded no merge table, and a stream's cap
// was read as the sprint's (TestReviewFinishUsesTheStreamsAttemptCap).
func TestEveryStepThatAsksTheAttemptCapLoadsTheStreamControls(t *testing.T) {
	t.Parallel()
	gens := map[string]int{"s1-1.w1": 1}
	ids := sprint.Sel{IDs: []string{"s1-1.w1"}}
	steps := map[string]Step{
		"finish":                FinishStep(sprint.FinishReq{Sel: ids, As: "m1", Gens: gens}),
		"finish failed":         FinishStep(sprint.FinishReq{Sel: ids, As: "m1", Gens: gens, Failed: true, Report: "f"}),
		"finish with usage":     FinishStep(sprint.FinishReq{Sel: ids, As: "m1", Gens: gens, Usage: "wall=1s"}),
		"read by a reader":      ReadStep(sprint.ReadReq{Sel: sprint.Sel{Limit: 1}, As: "reader-a", Verdict: "broken", Finding: "f:1"}),
		"read by a member row":  ReadStep(sprint.ReadReq{Sel: sprint.Sel{Limit: 1}, As: "m1", Verdict: "broken", Finding: "f:1"}),
		"read by a friend row":  ReadStep(sprint.ReadReq{Sel: sprint.Sel{Limit: 1}, As: "friend-stella", Verdict: "broken", Finding: "f:1"}),
		"the coordinator reads": CoordinatorReadStep(sprint.CoordinatorReadReq{Verdict: "broken"}),
		"merge":                 MergeStep(sprint.MergeReq{}),
		"set":                   SetStep(sprint.SetReq{}),
	}
	for name, s := range steps {
		assert.True(t, s.Bars, "%s asks the attempt cap (Step.Bars)", name)
		assert.False(t, asksCapWithoutStreamControls(s), "%s asks the attempt cap and loads no merge table %v: a stream's own cap goes unread", name, s.Load)
	}
	// the check bites: the finish as it loaded before
	assert.True(t, asksCapWithoutStreamControls(Step{Bars: true, Load: tables(sprint.Fleet, sprint.Readers, sprint.Work)}))
}
