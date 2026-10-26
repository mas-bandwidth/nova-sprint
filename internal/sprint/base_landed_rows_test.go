package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 2026-10-17 14:02: every tick drain was refused because the work table's
// base_landed.<sha> properties, one per repository and branch the lander ever
// pushed, passed the table's property bound. A landing's provenance is a field
// of the landing stream's control row, never a work-table property, and one row
// holds a given repository and branch.
func TestBaseLandingsAddNoWorkTableProperty(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s2", Count: 2}))
	accepted(w, "s1-1")
	accepted(w, "s2-1")
	accepted(w, "s2-2")
	const repo = "mas-bandwidth/nova-sprint"
	tipA, tipB, tipC := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	w.must(RecordBaseLanding(w.s, MergeReq{Stream: "s1", Cards: []string{"s1-1"}}, repo, "sprint/b1", tipA))
	w.must(RecordBaseLanding(w.s, MergeReq{Stream: "s2", Cards: []string{"s2-1"}}, repo, "sprint/b2", tipB))
	for name := range w.s.Work.Props() {
		require.False(t, strings.HasPrefix(name, "base_landed"), "a landing writes no per-base work-table property: %s", name)
	}
	require.False(t, BaseMovedOutside(w.s, repo, "sprint/b1", tipA), "the first base keeps its landed tip")
	require.False(t, BaseMovedOutside(w.s, repo, "sprint/b2", tipB), "the second base keeps its landed tip")
	require.True(t, BaseMovedOutside(w.s, repo, "sprint/b1", tipB), "another base's tip is not this base's")
	require.True(t, BaseMovedOutside(w.s, "other/repo", "sprint/b1", tipA), "another repository has unknown provenance")
	// another stream lands on the first base: its tip is the base's, the older one is not
	w.must(RecordBaseLanding(w.s, MergeReq{Stream: "s2", Cards: []string{"s2-2"}}, repo, "sprint/b1", tipC))
	require.False(t, BaseMovedOutside(w.s, repo, "sprint/b1", tipC), "the latest landing on a base is its tip")
	require.True(t, BaseMovedOutside(w.s, repo, "sprint/b1", tipA), "an older landing on the base is not its tip")
}

// A base_landed.<sha256> property that a v1.6.3 store already holds is kept
// current by a v1.7 landing and is never created: rollback and re-upgrade agree,
// and the property count never grows. With no control row, an existing property
// still moves to the landing's tip.
func TestALandingUpdatesTheOldPropertyAndAddsNone(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	accepted(w, "s1-1", "s1-2")
	const repo = "mas-bandwidth/nova-sprint"
	const baseX, baseY, baseZ = "sprint/x", "sprint/y", "sprint/z"
	old, x, y, z := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	keyX := BaseLandedKey(repo, baseX)
	w.s.Work.SetProp(keyX, old)

	w.must(RecordBaseLanding(w.s, MergeReq{Stream: "s1", Cards: []string{"s1-1"}}, repo, baseX, x))
	ctl := w.s.StreamCtl("s1")
	require.Equal(t, repo, ctl.F(FieldBaseLandedRepo), "the landing stream's row names the repository")
	require.Equal(t, baseX, ctl.F(FieldBaseLandedBase), "the landing stream's row names the base")
	require.Equal(t, x, ctl.F(FieldBaseLandedTip), "the landing stream's row names the new tip")
	got, ok := w.s.Work.Prop(keyX)
	require.True(t, ok, "the old property is kept")
	require.Equal(t, x, got, "the old property is updated to the new tip")

	// A base with no old property lands and writes none.
	before := len(w.s.Work.Props())
	w.must(RecordBaseLanding(w.s, MergeReq{Stream: "s1", Cards: []string{"s1-2"}}, repo, baseY, y))
	require.Equal(t, y, w.s.StreamCtl("s1").F(FieldBaseLandedTip), "the second base's tip is on the row")
	_, ok = w.s.Work.Prop(BaseLandedKey(repo, baseY))
	require.False(t, ok, "a base with no old property gains none")
	require.Equal(t, before, len(w.s.Work.Props()), "a landing adds no work-table property")

	// With no control row an existing property still moves to the tip.
	keyZ := BaseLandedKey(repo, baseZ)
	w.s.Work.SetProp(keyZ, z)
	require.Nil(t, w.s.StreamCtl("s-none"), "no control row for this stream")
	w.must(BaseLandingUnits(w.s, Plan{}, "s-none", repo, baseZ, x))
	got, ok = w.s.Work.Prop(keyZ)
	require.True(t, ok, "the property is kept with no control row")
	require.Equal(t, x, got, "the property moves with no control row")
}

// A v1.6.3 landing between two v1.7 landings writes only the old property. Its
// tip is the lander's, so the next red gate does not open a false base-red
// episode; an outside tip is still outside.
func TestAV16LandingBetweenTwoV17LandingsIsTheLanders(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	accepted(w, "s1-1", "s1-2")
	const repo, base = "mas-bandwidth/nova-sprint", "sprint/v16"
	x, y, z := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)

	w.must(RecordBaseLanding(w.s, MergeReq{Stream: "s1", Cards: []string{"s1-1"}}, repo, base, x))
	require.False(t, BaseMovedOutside(w.s, repo, base, x), "the v17 landing is the lander's")

	// A v1.6.3-style landing writes the property alone, leaving the row at X.
	w.s.Work.SetProp(BaseLandedKey(repo, base), y)
	require.Equal(t, x, w.s.StreamCtl("s1").F(FieldBaseLandedTip), "the v1.6.3 write leaves the row alone")
	require.False(t, BaseMovedOutside(w.s, repo, base, y), "the v1.6.3 landing between two v17 landings is the lander's")
	require.True(t, BaseMovedOutside(w.s, repo, base, z), "an outside tip is still outside")

	// The next v17 landing keeps the property current again.
	w.must(RecordBaseLanding(w.s, MergeReq{Stream: "s1", Cards: []string{"s1-2"}}, repo, base, z))
	got, ok := w.s.Work.Prop(BaseLandedKey(repo, base))
	require.True(t, ok, "the property is kept")
	require.Equal(t, z, got, "the property names the latest v17 tip")
	require.False(t, BaseMovedOutside(w.s, repo, base, z), "the latest v17 landing is the lander's")
}
