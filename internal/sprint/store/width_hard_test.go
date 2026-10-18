package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// THE WIDTH IS HARD at the one writer, on every path (tla/WidthHard.tla WidthHeld): the
// store holds every step's plan to each fleet row's width as it applies it
// (sprint.HoldWidths, Store.Run), so a path that forgot its own check still cannot pass
// the width. A unit past the width is skipped alone, with one refusal line naming the
// row, its working count and its width; every other unit of the plan applies.

// workingCreate is a unit creating card id in row's working column, as a read create
// or a take would put a card there.
func workingCreate(id, row string) sprint.Unit {
	return sprint.Unit{Key: id, Changes: []sprint.Change{{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: id,
		Expect: &ntable.MemberExpect{Absent: true},
		Create: &ntable.MemberCreateOp{Row: row, Col: sprint.Working}, Set: map[string]string{"kind": "work", "gen": "1"}}}},
		Moved: id + " created working on " + row}
}

func TestStoreRefusesAnOverWidthUnit(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.now = time.Unix(1000, 0).UTC() // the test's clock, from 1000 s: no real time is read
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 1}))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: 1}))
	// m1 is at width-1 (0 working of 1): the first unit fits, the second would pass it,
	// and the third is another row's
	res := h.run(Step{Verb: "width probe", Load: []string{sprint.Fleet}, Plan: func(*sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Units: []sprint.Unit{workingCreate("x1", "m1"), workingCreate("x2", "m1"), workingCreate("x3", "m2")}}
	}})
	require.Len(t, res.Refused, 1, "one refusal, the unit past the width alone: %v", res.Refused)
	assert.Equal(t, "x2", res.Refused[0].Key)
	assert.Contains(t, res.Refused[0].Why, "m1 is at its width (1 working of 1)", "the refusal names the row, its working count and its width")
	s := h.snap()
	assert.Equal(t, 1, s.Fleet.Count("m1", sprint.Working), "m1 never passes its width")
	assert.Equal(t, sprint.Working, s.Fleet.Card("x1").Col, "the first unit applies")
	assert.NotEqual(t, sprint.Working, s.Fleet.Card("x2").Col, "the second is not made working")
	assert.Equal(t, sprint.Working, s.Fleet.Card("x3").Col, "the third unit, another row's, applies")
	for _, line := range res.Moved {
		if strings.HasPrefix(line, "x2 ") {
			assert.Contains(t, line, "x2 made ready, not working: m1 is at its width (1 of 1)", "the skipped create is said made ready: %q", line)
		}
	}
}

// The store holds a member's width to nova-config's when it holds the config read
// (Store.Widths): a control-card copy of 16 against a config of 8 is written back to 8
// in the step, and the step's own takes are held to 8 (tla/WidthHard.tla OneSource).
func TestStoreHoldsTheConfigWidthAndRefreshesTheCopy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.now = time.Unix(1000, 0).UTC() // the test's clock, from 1000 s: no real time is read
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 16}))
	require.Equal(t, 16, h.snap().Width("m1"))
	h.st.Widths = func(context.Context) (map[string]int, bool) { return map[string]int{"m1": 1}, true }
	res := h.run(Step{Verb: "width probe", Load: []string{sprint.Fleet}, Plan: func(*sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Units: []sprint.Unit{workingCreate("y1", "m1"), workingCreate("y2", "m1")}}
	}})
	require.Len(t, res.Refused, 1, "the config's width 1 holds, not the copy's 16: %v", res.Refused)
	assert.Contains(t, res.Refused[0].Why, "m1 is at its width (1 working of 1)")
	s := h.snap()
	assert.Equal(t, 1, s.Fleet.Count("m1", sprint.Working))
	assert.Equal(t, 1, s.Width("m1"), "the control card's copy is nova-config's again")
}
