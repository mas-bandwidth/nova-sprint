package sprint_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The reads go out together, in parallel (docs/SPEC-SPRINT.md section 6; the owner,
// 2026-10-11: "Multiple reads now go out in ||"; 2026-10-06: "Make sure that we deal the
// multiple reads out in ||"; "I don't want any serial reads again").

const flashBrief = "c: the work (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\n\nThe task.\n"

// TestAnAttemptsReadsGoOutTogether: set --reads 3, a primary in review, three idle
// members each a reader of its tier and none its worker: after one tick three read cards
// are dealt, one on each; each member takes its read through the member's take; after that
// all three are working.
func TestAnAttemptsReadsGoOutTogether(t *testing.T) {
	t.Parallel()
	r := newReadCardsRig(t, "m1", "m2", "m3", "m4")
	r.must(store.SetStep(sprint.SetReq{Reads: "3", Who: "coordinator"}))
	worker := r.toReview("s1-1", proBrief)
	require.Equal(t, sprint.Review, r.snap().Work.Card("s1-1").Col)
	reads := r.readCards("s1-1")
	require.Len(t, reads, 3, "all three reads dealt in one tick")
	rows := map[string]bool{}
	for _, c := range reads {
		require.NotEqual(t, worker, c.Row, "never the worker")
		require.Equal(t, sprint.Ready, c.Col)
		rows[c.Row] = true
	}
	require.Len(t, rows, 3, "one on each reader")
	for _, c := range reads {
		r.must(store.TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}},
			Gens: map[string]int{c.ID: c.Int("gen")}, Who: c.Row}))
	}
	for _, c := range reads {
		rc := r.snap().Fleet.Card(c.ID)
		require.NotNil(t, rc)
		require.Equal(t, sprint.Working, rc.Col, "taken: working")
	}
}

// advance moves the rig's clock forward, as a long gap between ticks does.
func (r *readCardsRig) advance(d time.Duration) {
	r.t.Helper()
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
}

// TestATakenBackReadIsAskedOfTheSameReaderAgain: the only reader that may read an attempt
// has its read taken back by the machine nine times (the unstarted bound), past the load
// window (readCardLoadWindow): it is dealt the read again each time at a fresh generation,
// never refused "no id" nor "read generations past the load window", and ReadCardExtras
// names the generation after it (the owner, 2026-10-06: "I don't want any serial reads
// again"; 2026-10-15, forty review cards wedged at five taken-back generations; the read
// top, sprint.PropReadGenTop, tla/ReadCards.tla NeverWedged).
func TestATakenBackReadIsAskedOfTheSameReaderAgain(t *testing.T) {
	t.Parallel()
	r := newReadCardsRig(t, "m1", "m2")
	worker := r.toReview("s1-1", flashBrief)
	reads := r.readCards("s1-1")
	require.Len(t, reads, 1, "a flash card needs one read")
	reader := reads[0].Row
	require.NotEqual(t, worker, reader)
	base := sprint.ReadCardID("s1-1", 1, reader)
	const takeBacks = 9
	for i := 1; i <= takeBacks; i++ {
		r.advance(7 * time.Hour) // past the dealt bound: the machine takes the unstarted read back
		r.tick()
		reads = r.readCards("s1-1")
		require.Len(t, reads, 1, "dealt again at take-back %d", i)
		require.Equal(t, reader, reads[0].Row, "the same reader")
		require.Equal(t, fmt.Sprintf("%s.g%d", base, i), reads[0].ID, "a fresh generation")
	}
	require.Contains(t, sprint.ReadCardExtras(r.snap()), fmt.Sprintf("%s.g%d", base, takeBacks+1),
		"ReadCardExtras names the generation the store must read")
}
