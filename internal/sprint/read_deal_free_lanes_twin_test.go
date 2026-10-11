package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The read deal counts the queue and fills free lanes first (docs/SPEC-SPRINT.md section 6,
// the read cards; the owner, 2026-10-11: "cards only may accrue per-fleet or friend, if
// there are not other machines/friends ready to do that work right now!"; "Deal logic should
// be using ((index++) % n) to roll around, not hitting the same fleet machine or friend each
// deal"; tla/WhoPreference.tla, DealFreeLanesFirst): a unit's free lanes are twice its width
// less twice its work cards and its read cards, ready and working (rowLoad), so a lane
// already promised to a queued card is not free, and a burst of reads rolls round every unit
// with a free lane, never the most idle and never the name.

// queueReads puts n read cards ready (dealt, not taken) on the member's row, each of a flash
// primary in review at attempt 1 whose one read it is, so the read stands and is not taken
// back.
func queueReads(w *world, member string, n int) {
	for i := 0; i < n; i++ {
		id := "q-" + member + "-" + itoa(i)
		putReviewBy(w, id, id+": work (s1) tier: flash\n", "w0", float64(100+i))
		w.s.Fleet.Put(&Card{ID: ReadCardID(id, 1, member), Row: member, Col: Ready, Score: float64(100 + i), Rev: 1, Fields: map[string]string{
			"kind": "read", "primary": id, "stream": "s1", "reader": member, "attempt": "1", "gen": "1",
			FieldReadCard: "1", "asked": stamp(w.s.Now), FieldTier: "flash"}})
	}
}

// busyMember puts n work cards working on the member's row.
func busyMember(w *world, member string, n int) {
	for i := 0; i < n; i++ {
		id := "busy-" + member + "-" + itoa(i)
		w.s.Fleet.Put(&Card{ID: id + ".w1", Row: member, Col: Working, Score: 1, Rev: 1, Fields: map[string]string{"kind": "work", "primary": id, "stream": "s9", "attempt": "1"}})
	}
}

// readsOn is the read cards placed on the member's row, ready or working.
func readsOn(w *world, member string) int {
	_, reads := rowLoad(w.s, member)
	return reads
}

// TestTheReadDealSkipsAMemberWithAQueue (observation 1, 2026-10-11): hetzner2 at width 16
// with 4 working and 24 reads ready (4 + 24/2 = 16 slots: every lane promised; the
// observation's 22 leaves it one free lane, two reads, which it may rightly take); b3 and
// space at width 16 with 8 working and nothing ready; a burst of ten primaries each wanting
// one read: none lands on hetzner2, whose lanes are promised to its queue (a free lane
// counts the queue: rowLoad, ready and working). Before, its idle count read only its 4
// working cards, so it looked the most idle and took 8 of the 10.
func TestTheReadDealSkipsAMemberWithAQueue(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 16, "hetzner2", "b3", "space")
	busyMember(w, "hetzner2", 4)
	queueReads(w, "hetzner2", 24)
	busyMember(w, "b3", 8)
	busyMember(w, "space", 8)
	for i := 1; i <= 10; i++ {
		putReviewBy(w, "s1-"+itoa(i), "s1-"+itoa(i)+": work (s1) tier: flash\n", "w0", float64(i))
	}
	dealReads(t, w, nil)
	require.Equal(t, 24, readsOn(w, "hetzner2"), "no read of the burst lands on the member with a queue")
	require.Equal(t, 10, readsOn(w, "b3")+readsOn(w, "space"))
}

// TestTheReadDealGivesNarrowMembersTheirTurn: members of widths 16, 16, 8 and 4, all idle,
// readers of the tier, sixteen reads: the deal rolls round the members with a free lane
// ((index++) % n), so batman's lanes take reads too, and none queues while another member
// has a free lane.
func TestTheReadDealGivesNarrowMembersTheirTurn(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 16, "b1", "b2", "superman", "batman")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "superman", Width: 8}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "batman", Width: 4}))
	for i := 1; i <= 16; i++ {
		putReviewBy(w, "s1-"+itoa(i), "s1-"+itoa(i)+": work (s1) tier: flash\n", "w0", float64(i))
	}
	dealReads(t, w, nil)
	require.Positive(t, readsOn(w, "batman"), "the narrowest member takes reads too")
	require.Positive(t, readsOn(w, "superman"))
	for _, m := range []string{"b1", "b2", "superman", "batman"} {
		require.LessOrEqual(t, readsOn(w, m), 2*w.s.Width(m), "%s: no read queues past its free lanes", m)
	}
	require.Equal(t, 16, readsOn(w, "b1")+readsOn(w, "b2")+readsOn(w, "superman")+readsOn(w, "batman"))
}
