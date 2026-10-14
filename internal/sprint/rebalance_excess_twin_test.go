package sprint_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// The rebalance moves the excess of every unit, not only of a unit whose every
// lane works (the owner, 2026-10-11 ~19:05-19:20Z: "cards only may accrue
// per-fleet or friend, if there are not other machines/friends ready to do that
// work right now!"; NoQueuedCardWhileACapableLaneIsFree, tla/WhoPreference.tla,
// the model card tla-whopreference-free-lanes-first-and-taken-stays). On the twin
// store (store.NewTwin).

// readCardsOn turns read cards on and gives every member up a reader row reading flash,
// beside a flash route (as read_cards_twin_test.go sets it up).
func (r *dealRig) readCardsOn(tiers string) {
	r.t.Helper()
	require.NoError(r.t, r.m.RowsAdd(r.ctx, "t-work", []string{"s1"}))
	r.m.SetRoutes([]sprint.Route{{Name: "flash-a", Tier: "flash", Provider: "p", Model: "f", Tokens: 1000, Deadline: 600, Enabled: true}})
	var readers []string
	for _, m := range r.members {
		readers = append(readers, sprint.ReaderPrefix+m)
	}
	require.NoError(r.t, r.m.RowsAdd(r.ctx, "t-readers", readers))
	require.NoError(r.t, r.st.EnsureReaderTiers(r.ctx))
	for _, rd := range readers {
		require.NoError(r.t, r.m.RowSet(r.ctx, "t-readers", rd, map[string]string{sprint.ReaderTiers: tiers}))
	}
	r.must(store.SetStep(sprint.SetReq{ReadCards: sprint.ReadCardsOnWord, Who: "coordinator"}))
}

// seedRead puts n read cards on the member in the column, as a store the member
// already holds would hold them (kind read, on stream s1), a reader of the reads'
// tier, and its primary in review (flash).
func (r *dealRig) seedRead(member, col string, n int) {
	r.t.Helper()
	for i := 0; i < n; i++ {
		r.cardN++
		primary := "seedr-" + itoaTest(r.cardN)
		r.poke(sprint.Work, ntable.BatchMemberEntry{ID: primary,
			Expect: &ntable.MemberExpect{Absent: true},
			Create: &ntable.MemberCreateOp{Row: "s1", Col: sprint.Review, Score: float64(r.cardN)},
			Set:    map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", sprint.FieldTierNow: "flash"}})
		r.poke(sprint.Fleet, ntable.BatchMemberEntry{ID: primary + ".r1",
			Expect: &ntable.MemberExpect{Absent: true},
			Create: &ntable.MemberCreateOp{Row: member, Col: col, Score: float64(r.cardN)},
			Set:    map[string]string{"kind": "read", "primary": primary, "stream": "s1", "reader": member, "attempt": "1", "gen": "1"}})
	}
}

// readyReads is the member's ready read cards.
func (r *dealRig) readyReads(name string) int {
	n := 0
	for _, c := range r.snap().Fleet.Cell(name, sprint.Ready) {
		if c.F("kind") == "read" {
			n++
		}
	}
	return n
}

// TestTheRebalanceMovesTheExcessOfAPartlyIdleUnit pins the source rule on the
// twin: hetzner2 (width 16) holds 8 work and 8 reads working (12 of 16 lanes)
// and 22 read cards ready; every other member is a reader of the reads' tier
// with a free lane. One tick leaves hetzner2 no more ready reads than its 8 free
// halves; the rest are on units that had a free lane.
func TestTheRebalanceMovesTheExcessOfAPartlyIdleUnit(t *testing.T) {
	t.Parallel()
	r := newDealRig(t)
	r.up("hetzner2", 16)
	r.up("b3", 16)
	r.up("mini-m5", 16)
	r.up("space", 16)
	r.up("vision", 16)
	r.up("superman", 8)
	r.up("batman", 4)
	r.readCardsOn("flash")
	r.seedWork("hetzner2", sprint.Working, 8)
	r.seedRead("hetzner2", sprint.Working, 8)
	r.seedRead("hetzner2", sprint.Ready, 22)
	for _, m := range []string{"b3", "mini-m5", "space", "vision"} {
		r.seedWork(m, sprint.Working, 8)
	}
	r.seedWork("superman", sprint.Working, 2)
	require.Equal(t, 22, r.readyReads("hetzner2"), "hetzner2 holds %d ready reads, want 22", r.readyReads("hetzner2"))

	r.tick()
	require.LessOrEqual(t, r.readyReads("hetzner2"), 8,
		"hetzner2 keeps %d ready reads, want at most its 8 free halves", r.readyReads("hetzner2"))
	moved := 0
	for _, m := range []string{"b3", "mini-m5", "space", "vision", "superman", "batman"} {
		moved += r.readyReads(m)
	}
	require.GreaterOrEqual(t, moved, 14,
		"only %d ready reads are on units with a free lane, want the excess, >= 14", moved)
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// NoQueuedCardWhileACapableLaneIsFree is the rebalance's invariant (rebalance.go,
// tla/WhoPreference.tla): "" when no unit keeps a queued card while another unit able to take
// it has a free lane; else a message naming the moves the rebalance would still make.
func NoQueuedCardWhileACapableLaneIsFree(s *sprint.Snapshot) string {
	p := sprint.Rebalance(s, nil, "test")
	if len(p.Units) == 0 {
		return ""
	}
	var b strings.Builder
	for _, u := range p.Units {
		b.WriteString(u.Moved)
		b.WriteString("\n")
	}
	return b.String()
}

// seedWorkCard puts a work card and its primary (working, as a taken card would hold it) on
// the member in the column, so the rebalance's work pass may move it.
func (r *dealRig) seedWorkCard(member, col string) string {
	r.t.Helper()
	r.cardN++
	primary := fmt.Sprintf("seedw-%d", r.cardN)
	wcID := primary + ".w1"
	r.poke(sprint.Work, ntable.BatchMemberEntry{ID: primary,
		Expect: &ntable.MemberExpect{Absent: true},
		Create: &ntable.MemberCreateOp{Row: "s1", Col: sprint.Working, Score: float64(r.cardN)},
		Set:    map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "work": wcID, sprint.FieldTierNow: "flash"}})
	r.poke(sprint.Fleet, ntable.BatchMemberEntry{ID: wcID,
		Expect: &ntable.MemberExpect{Absent: true},
		Create: &ntable.MemberCreateOp{Row: member, Col: col, Score: float64(r.cardN)},
		Set:    map[string]string{"kind": "work", "primary": primary, "stream": "s1", "attempt": "1", "gen": "1", "member": member, "redeals": "0"}})
	return wcID
}

// seedFix puts one fix card (a rework attempt carrying the fix field, kind work) ready on the
// member, so the rebalance moves it as a work card.
func (r *dealRig) seedFix(member string) string {
	r.t.Helper()
	r.cardN++
	primary := fmt.Sprintf("seedf-%d", r.cardN)
	wcID := primary + ".w2"
	r.poke(sprint.Work, ntable.BatchMemberEntry{ID: primary,
		Expect: &ntable.MemberExpect{Absent: true},
		Create: &ntable.MemberCreateOp{Row: "s1", Col: sprint.Working, Score: float64(r.cardN)},
		Set:    map[string]string{"kind": "primary", "attempt": "2", "stream": "s1", "work": wcID, sprint.FieldTierNow: "flash"}})
	r.poke(sprint.Fleet, ntable.BatchMemberEntry{ID: wcID,
		Expect: &ntable.MemberExpect{Absent: true},
		Create: &ntable.MemberCreateOp{Row: member, Col: sprint.Ready, Score: float64(r.cardN)},
		Set:    map[string]string{"kind": "work", "primary": primary, "stream": "s1", "attempt": "2", "gen": "1", "member": member, "redeals": "0", "fix": "the finding"}})
	return wcID
}

// finish finishes up to n working work cards of the member, as the member's finish verb does,
// passing over a seeded working card whose primary is not working on it (a taken card seeded
// in place, never dealt).
func (r *dealRig) finish(name string, n int) {
	r.t.Helper()
	s := r.snap()
	done := 0
	for _, c := range s.Fleet.Cell(name, sprint.Working) {
		if c.F("kind") == "read" {
			continue
		}
		if pr := s.Work.Placed(c.F("primary")); pr == nil || pr.Col != sprint.Working || pr.F("work") != c.ID {
			continue // a seeded working card: its primary is not working on it, leave it taken
		}
		head := fmt.Sprintf("%040d", c.Int("gen"))
		r.must(store.FinishStep(sprint.FinishReq{As: name, Head: head, Branch: "sprint/run", Report: "done",
			Usage: costUsage, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: c.Int("gen")}, Who: name}))
		done++
		if done >= n {
			return
		}
	}
}

// A cleared sprint with no setting set runs the rebalance every tick: there is no switch that
// turns it off (docs/SPEC-SPRINT.md section 1, the rebalance; rebalance.go PartRebalance).
func TestAClearedSprintRunsTheRebalanceEveryTick(t *testing.T) {
	t.Parallel()
	r := newDealRig(t) // a fresh sprint, as a clear leaves it: no setting set
	r.m.SetRoutes([]sprint.Route{{Name: "flash-a", Tier: "flash", Provider: "p", Model: "f", Tokens: 1000, Deadline: 600, Enabled: true}})
	require.NoError(t, r.m.RowsAdd(r.ctx, "t-work", []string{"s1"}))
	r.up("m1", 2)
	r.up("m2", 2)
	r.seedWorkCard("m1", sprint.Working)
	r.seedWorkCard("m1", sprint.Working)
	r.seedWorkCard("m1", sprint.Ready) // m1's excess: no free half beside two working
	require.Equal(t, 1, r.ready("m1"), "m1 holds one ready card beyond its lanes")

	r.tick()
	require.Equal(t, 0, r.ready("m1"), "the rebalance ran: m1's excess moved to m2")
	require.Equal(t, 1, r.ready("m2"), "m2, with a free lane, took the excess")

	r.seedWorkCard("m1", sprint.Ready) // excess again
	r.tick()
	require.Equal(t, 0, r.ready("m1"), "the rebalance runs every tick, with no setting set")
	require.Equal(t, 2, r.ready("m2"), "m2 took the excess again")
}

// No unit keeps a queued card while another unit able to take it has a free lane, over a run
// of 60 cards (work, reads and a fix card) admitted over 12 ticks, the members taking by
// count at their width and finishing a share each tick: after every tick the invariant holds.
// The run starts from (1)'s fleet: hetzner2 holds 8 work and 8 reads working (12 of 16 lanes)
// and 22 ready reads, so its excess exists from the first tick, and reads are admitted beside
// work each tick, so the invariant is checked where it can actually be violated, never trivially.
func TestNTicksKeepNoQueuedCardWhileALaneIsFree(t *testing.T) {
	r := newDealRig(t)
	r.up("hetzner2", 16)
	r.up("b3", 16)
	r.up("mini-m5", 16)
	r.up("space", 16)
	r.up("vision", 16)
	r.up("superman", 8)
	r.up("batman", 4)
	r.readCardsOn("flash")
	// (1)'s fleet: hetzner2 at 12 of 16 lanes with 22 ready reads (14 excess); b3, mini-m5,
	// space, vision at 8 working; superman at 2; batman idle.
	r.seedWork("hetzner2", sprint.Working, 8)
	r.seedRead("hetzner2", sprint.Working, 8)
	r.seedRead("hetzner2", sprint.Ready, 22)
	for _, m := range []string{"b3", "mini-m5", "space", "vision"} {
		r.seedWork(m, sprint.Working, 8)
	}
	r.seedWork("superman", sprint.Working, 2)
	r.seedFix("hetzner2") // one fix card, kind work, among the admitted cards
	v := NoQueuedCardWhileACapableLaneIsFree(r.snap())
	require.NotEmpty(t, v, "the run does not start with excess: hetzner2's 22 ready reads are its 8 free halves' start, so the rebalance has nothing to move and the invariant pins nothing")

	for tick := 0; tick < 12; tick++ {
		r.add(3)                                // 36 work primaries over the run
		r.seedRead("hetzner2", sprint.Ready, 2) // 24 read cards over the run
		r.tick()
		s := r.snap()
		require.Empty(t, NoQueuedCardWhileACapableLaneIsFree(s), "tick %d: a unit keeps a queued card while another has a free lane", tick)
		for _, m := range r.members {
			r.take(m)
			r.finish(m, 1)
		}
	}
}
