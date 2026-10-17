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
