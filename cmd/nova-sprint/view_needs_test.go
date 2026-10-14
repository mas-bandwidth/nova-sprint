package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// view coordinator --needs (view.go readCoordNeeds / coordNeedsText; docs/SPEC-SPRINT.md,
// "view-coordinator-needs.w1"): the decisions waiting on the coordinator, heaviest first,
// in text and in --json. The ranking itself is sprint.NeedsRank's (needs_rank_test.go);
// these tests lock the verb's rendering of it: the document, its order, and its refusals.

// coordNeeds reads view coordinator --needs --json.
func (ta *testApp) coordNeeds() coordNeedsView {
	ta.t.Helper()
	var v coordNeedsView
	ta.json("view coordinator --needs", &v)
	require.Equal(ta.t, "coordinator-needs", v.View)
	require.Equal(ta.t, viewSchema, v.Schema)
	return v
}

func TestCoordNeedsTextRendersHeaviestFirst(t *testing.T) {
	t.Parallel()
	v := coordNeedsView{View: "coordinator-needs", Schema: viewSchema, Total: 3, Needs: []sprint.Need{
		{Kind: sprint.NeedJudgment, ID: "j-heavy", Cards: []string{"r3"}, Behind: 12, Age: 2 * time.Hour},
		{Kind: sprint.NeedSentinel, ID: "gate", Type: "w", Cards: []string{"gate"}, Behind: 4, Age: 25 * time.Minute},
		{Kind: sprint.NeedHeld, ID: "h1", Type: "g", Cards: []string{"h1"}, Behind: 0, Age: 45 * time.Second},
	}}
	out := coordNeedsText(v)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 4, "%s", out)
	assert.Equal(t, "VIEW coordinator --needs total=3", lines[0])
	assert.Equal(t, "12 behind 2h judgment j-heavy: cards r3", lines[1], "heaviest first: %s", out)
	assert.Equal(t, "4 behind 25m sentinel gate: cards gate", lines[2], "%s", out)
	assert.Equal(t, "0 behind 45s held h1: cards h1", lines[3], "%s", out)
}

func TestCoordNeedsTextEmptyAndTruncation(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "VIEW coordinator --needs total=0\n", coordNeedsText(coordNeedsView{Needs: []sprint.Need{}}))

	mk := func(n int) coordNeedsView {
		v := coordNeedsView{Total: n, Needs: make([]sprint.Need, n)}
		for i := range v.Needs {
			v.Needs[i] = sprint.Need{Kind: sprint.NeedHeld, ID: fmt.Sprintf("h%02d", i), Cards: []string{fmt.Sprintf("h%02d", i)}, Behind: n - i}
		}
		return v
	}
	// exactly viewTextLines-1 needs fit under the header with no remainder line
	out := coordNeedsText(mk(viewTextLines - 1))
	assert.NotContains(t, out, "more:")
	assert.Len(t, strings.Split(strings.TrimRight(out, "\n"), "\n"), viewTextLines, "%s", out)
	// one more need than fits collapses the tail into the remainder line
	out = coordNeedsText(mk(viewTextLines + 5))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, viewTextLines, "%s", out)
	assert.Equal(t, fmt.Sprintf("+%d more: nova-sprint view coordinator --needs --json", 7), lines[len(lines)-1], "%s", out)
	assert.Contains(t, lines[1], "h00", "heaviest first is kept before the cut: %s", out)
	assert.NotContains(t, out, "h18", "needs past the cut are counted, not printed: %s", out)
}

func TestViewCoordinatorNeedsRanksHeaviestFirstInJSONAndText(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream w --sentinel gate --held")
	ta.ok("add --stream w a b")
	ta.ok("add --stream v c --one --held")

	v := ta.coordNeeds()
	require.Equal(t, len(v.Needs), v.Total, "total counts the needs: %+v", v)
	require.Len(t, v.Needs, 2, "%+v", v.Needs)
	assert.Equal(t, sprint.NeedSentinel, v.Needs[0].Kind, "the held sentinel, heaviest first: %+v", v.Needs)
	assert.Equal(t, "gate", v.Needs[0].ID)
	assert.Equal(t, 2, v.Needs[0].Behind, "a and b wait behind the gate: %+v", v.Needs[0])
	assert.Equal(t, sprint.NeedHeld, v.Needs[1].Kind, "%+v", v.Needs)
	assert.Equal(t, "c", v.Needs[1].ID)
	assert.Equal(t, 0, v.Needs[1].Behind, "%+v", v.Needs[1])
	assert.False(t, v.At.IsZero(), "the read is stamped: %+v", v)

	out := ta.ok("view coordinator --needs")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 3, "%s", out)
	assert.Equal(t, fmt.Sprintf("VIEW coordinator --needs total=%d", v.Total), lines[0])
	assert.True(t, strings.HasPrefix(lines[1], "2 behind "), "heaviest first in text too: %s", out)
	assert.Contains(t, lines[1], "sentinel gate: cards gate")
	assert.True(t, strings.HasPrefix(lines[2], "0 behind "), "%s", out)
	assert.Contains(t, lines[2], "held c: cards c")
}

func TestViewCoordinatorNeedsEmpty(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	v := ta.coordNeeds()
	assert.Equal(t, 0, v.Total)
	assert.Empty(t, v.Needs)
	assert.Equal(t, "VIEW coordinator --needs total=0\n", ta.ok("view coordinator --needs"))
}

func TestViewCoordinatorNeedsRefusesAllAndSince(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	for _, line := range []string{"view coordinator --needs --all", "view coordinator --needs --since 1.abc"} {
		code, _, errs := ta.do(line)
		assert.Equal(t, 2, code, "%s", line)
		assert.Contains(t, errs, "--needs lists the decisions only and takes neither --all nor --since", "%s: %s", line, errs)
	}
}
