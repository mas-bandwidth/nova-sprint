package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// at1000 is the tests' clock: an argument, never the real one (the card's
// rules: a test clock starts at 1000.0 seconds).
func at1000(w *world) { w.s.Now = time.Unix(1000, 0).UTC() }

// A loop between two landed cards is history: a landed card is no node of the
// wait-for graph and needs nothing, so it refuses no later edit. The squirrel
// pair of tla/WAITFOR.tsv class B -- one needs the other, the other was
// LAND-AFTER it -- was landed with that loop, and still refused an add that
// only reached one of them and a needs edit that only reached the other.
// tla/WaitFor.tla WaitEdges, WouldCycleLandedPoisons, NoWaitCycle,
// EditRefusedOnlyForItsOwnCycle; docs/SPEC-SPRINT.md section 4 (Order).
func TestALandedLoopNeverRefusesAnotherEdit(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	at1000(w)
	for _, id := range []string{"squirrel-base-forward-merge-schema-go-r", "schema-squirrel-resource-bounds-r", "d-1"} {
		w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{id}, Brief: id, Who: "coordinator"}))
	}
	// the pair as it landed: one needs the other, the other was LAND-AFTER it
	squirrel := w.s.Work.Placed("squirrel-base-forward-merge-schema-go-r")
	bounds := w.s.Work.Placed("schema-squirrel-resource-bounds-r")
	squirrel.Fields["needs"] = "schema-squirrel-resource-bounds-r"
	bounds.Fields[FieldLandAfter] = "squirrel-base-forward-merge-schema-go-r,d-1"
	w.place(w.s.Work, "squirrel-base-forward-merge-schema-go-r", "a", Landed)
	w.place(w.s.Work, "schema-squirrel-resource-bounds-r", "a", Landed)

	// an add that needs one of the pair and is LAND-AFTER the other: it closes
	// no loop through itself, so it is admitted (the CLI holds an add's LAND-AFTER
	// to WithAddSoftDeps, soft_deps.go)
	add := AddReq{Stream: "a", IDs: []string{"c-1"}, Needs: []string{"squirrel-base-forward-merge-schema-go-r"},
		Brief: "c-1\nLAND-AFTER: schema-squirrel-resource-bounds-r\n\nthe add", Who: "coordinator"}
	w.must(WithAddSoftDeps(w.s, Add(w.s, add), []AddReq{add}))
	// a needs edit that reaches the pair is admitted too
	w.must(NeedsSet(w.s, NeedsSetReq{ID: "d-1", Needs: []string{"schema-squirrel-resource-bounds-r"}, Who: "coordinator"}))
	require.Equal(t, "schema-squirrel-resource-bounds-r", w.s.Work.Placed("d-1").F("needs"))
	require.Empty(t, Cycles(w.s), "the landed pair is no cycle on the table")

	// the reversed witness: with landed cards kept in the graph (the tests' seam,
	// tla/WaitFor.tla WouldCycleLandedPoisons) the needs edit would close the old
	// loop through the pair and be refused.
	g := newNeedGraph(w.s, map[string][]string{"d-1": {"schema-squirrel-resource-bounds-r"}}, nil, nil)
	g.keepLanded = true
	require.NotNil(t, g.closes([]string{"d-1"}), "landedpoisons: landed cards kept in the graph")
	g.keepLanded = false
	require.Nil(t, g.closes([]string{"d-1"}), "open cards only")
}

// A needs edit that closes a loop through a LAND-AFTER edge is refused, naming
// the loop: LAND-AFTER is an edge of the one wait-for graph the refuser walks.
// tla/WaitFor.tla WaitEdges, NoWaitCycle, NeedsSet; docs/SPEC-SPRINT.md section
// 4 (Order).
func TestANeedsEditClosingALandAfterLoopIsRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	at1000(w)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "one", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"b-1"}, Brief: "two", Who: "coordinator"}))
	w.must(SetSoftDeps(w.s, SoftDepsSetReq{ID: "a-1", Raw: "b-1", Who: "coordinator"}))

	p := NeedsSet(w.s, NeedsSetReq{ID: "b-1", Needs: []string{"a-1"}, Who: "coordinator"})
	require.Empty(t, p.Units, "the edit closes a loop through LAND-AFTER: %+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	require.Contains(t, p.Refused[0].Why, "the needs would make a cycle", p.Refused[0].Why)
	require.Contains(t, p.Refused[0].Why, "b-1 -needs-> a-1 -land-after-> b-1", p.Refused[0].Why)
	require.Equal(t, "", w.s.Work.Placed("b-1").F("needs"))
}

// The other side: a LAND-AFTER edit that closes a loop through named needs is
// refused too. (This one already held at BASE: the old soft walk saw the hard
// edge.)
// tla/WaitFor.tla WaitEdges, NoWaitCycle, LandAfterSet; docs/SPEC-SPRINT.md
// section 4 (Order).
func TestALandAfterEditClosingANeedsLoopIsRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	at1000(w)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "one", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"b-1"}, Brief: "two", Who: "coordinator"}))
	w.must(NeedsSet(w.s, NeedsSetReq{ID: "a-1", Needs: []string{"b-1"}, Who: "coordinator"}))

	p := SetSoftDeps(w.s, SoftDepsSetReq{ID: "b-1", Raw: "a-1", Who: "coordinator"})
	require.Empty(t, p.Units, "the land-after closes a loop through the need: %+v", p)
	require.Len(t, p.Refused, 1, "%+v", p)
	require.Contains(t, p.Refused[0].Why, "cycle in dependencies", p.Refused[0].Why)
	require.Contains(t, p.Refused[0].Why, "a-1")
	require.Contains(t, p.Refused[0].Why, "b-1")
	require.Empty(t, SoftDepsRaw(w.s.Work.Placed("b-1")))
}

// A refusal names only a loop through the card the edit rewrites: an open loop
// P-Q elsewhere refuses nothing, and the tick's own judgment names P-Q once.
// tla/WaitFor.tla EditRefusedOnlyForItsOwnCycle, NoWaitCycle; docs/SPEC-SPRINT.md
// section 4 (Order).
func TestARefusalNamesOnlyALoopThroughTheEdit(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	at1000(w)
	for _, id := range []string{"p-1", "q-1", "r-1"} {
		w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{id}, Brief: id, Who: "coordinator"}))
	}
	// an open loop written before this rule, in the store
	w.s.Work.Placed("p-1").Fields["needs"] = "q-1"
	w.s.Work.Placed("q-1").Fields["needs"] = "p-1"
	w.place(w.s.Work, "p-1", "a", Waiting)
	w.place(w.s.Work, "q-1", "a", Waiting)

	// r-1 reaches the loop but closes none of its own: admitted
	p := SetSoftDeps(w.s, SoftDepsSetReq{ID: "r-1", Raw: "p-1", Who: "coordinator"})
	require.Empty(t, p.Refused, "a loop elsewhere refuses this edit: %+v", p)
	w.must(p)
	require.Equal(t, []string{"p-1"}, SoftDepsRaw(w.s.Work.Placed("r-1")))

	// the tick's judgment is one cycle, naming P-Q
	cs := Cycles(w.s)
	require.Len(t, cs, 1, "cycles: %v", cs)
	got := cs[0].String()
	require.True(t, strings.Contains(got, "p-1") && strings.Contains(got, "q-1"), "the loop is not named: %s", got)
}
