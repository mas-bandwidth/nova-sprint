package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Ghost reads (v1.2.14 BLOCKER, a-member-hands-back-a-read-it-is-not-running). 2026-10-11
// ~23:00Z on the live v1.2.13 server: of 42 cards in review about 20 each held one read card
// "working" for up to an hour (schema-c-t04-versions.r8.b2 since 21:53Z) while no member
// process ran it; they held their primaries to the 60-minute ReadCardDeadline. A read card
// working on a member is named by the member's latest beat (its live set), or it is within
// the grace: past it the machine takes it back to the read deal at its next generation,
// spending no reader and marking no failure (tla/ReadCards.tla NamedOrWithinGrace).

// memberBeat is a member's beat at now carrying the live set named, kept from prev as the
// store keeps it (NextLive).
func memberBeat(prev Beat, now time.Time, live ...string) Beat {
	b := prev
	b.At = now
	set := live
	if set == nil {
		set = []string{}
	}
	b.Live, b.LiveKnown, b.LiveSince, b.LiveSeen = NextLive(prev, now, set)
	return b
}

// takenRead is a world whose primary s1-1 (worked by m1) has its one read card dealt to m2
// and taken there: the card, its generation, and m2's first beat naming it.
func takenRead(t *testing.T) (*world, *Card, int, Beat) {
	t.Helper()
	w := readCardsWorld(t, 4, "m1", "m2")
	putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
	dealReads(t, w, nil)
	reads := readCardsOf(w, "s1-1")
	require.Len(t, reads, 1)
	rc := reads[0]
	require.Equal(t, "m2", rc.Row)
	w.must(Take(w.s, TakeReq{As: "m2", Sel: Sel{Limit: -1}, Who: "m2"}))
	rc = w.s.Fleet.Card(rc.ID)
	require.Equal(t, Working, rc.Col)
	gen := max(rc.Int("gen"), 1)
	return w, rc, gen, memberBeat(Beat{}, w.s.Now, LiveKey(rc.ID, gen))
}

// reconcile runs the tick's live part (TickLive) with m2's beat.
func reconcile(w *world, b Beat) {
	w.part(TickLive, TickReq{Beats: map[string]Beat{"m2": b}})
}

// backInTheDeal says the read card left the table retired lost, spending nothing, and the
// next ask dealt the read again at its next generation.
func backInTheDeal(t *testing.T, w *world, rc *Card, gen int) {
	t.Helper()
	old := w.s.Fleet.Card(rc.ID)
	require.NotNil(t, old)
	require.False(t, old.Placed(), "the lost read is off the member's row, not left in its ready")
	require.Equal(t, RetiredByLost, old.F("retired_by"), "taken back lost: no reader spent, no fail mark")
	require.Equal(t, gen, old.Int("lost_from_gen"))
	require.Contains(t, old.F("lost_reason"), "no live run")
	dealReads(t, w, nil)
	again := readCardsOf(w, "s1-1")
	require.Len(t, again, 1, "the read is dealt again at once")
	require.Equal(t, rc.ID+".g1", again[0].ID, "at its next generation, to a reader it spent nothing of")
	require.Equal(t, Ready, again[0].Col)
}

func TestAReadTheMemberDoesNotRunIsReturnedAtTheNextBeat(t *testing.T) {
	t.Parallel()
	w, rc, gen, b := takenRead(t)
	reconcile(w, b)
	require.Equal(t, Working, w.s.Fleet.Card(rc.ID).Col, "named by the beat: working")

	// the member's next beats name nothing (its child is gone, or never started)
	w.tick(LiveGrace / 2)
	b = memberBeat(b, w.s.Now)
	reconcile(w, b)
	require.Equal(t, Working, w.s.Fleet.Card(rc.ID).Col, "inside the grace a read stays working")

	w.tick(LiveGrace/2 + time.Second)
	b = memberBeat(b, w.s.Now)
	reconcile(w, b)
	backInTheDeal(t, w, rc, gen)
}

func TestAMemberRestartReturnsItsReadsAtOnce(t *testing.T) {
	t.Parallel()
	w, rc, gen, b := takenRead(t)
	reconcile(w, b)
	// the member is restarted (a rollout's switch): the process that ran the child is gone.
	// The new process's first beat comes before its first pass and carries no live set (the
	// row is left as it is), and its beats from then carry its own live set, which is empty.
	w.tick(BeatDeadline / 2)
	restart := w.s.Now
	b.At = w.s.Now
	b.Live, b.LiveKnown, b.LiveSince, b.LiveSeen = NextLive(b, w.s.Now, nil)
	reconcile(w, b)
	require.Equal(t, Working, w.s.Fleet.Card(rc.ID).Col, "a beat with no live set is no evidence")
	for w.s.Now.Sub(t0) < ReadCardDeadline && w.s.Fleet.Card(rc.ID).Placed() {
		w.tick(BeatDeadline / 2)
		b = memberBeat(b, w.s.Now)
		reconcile(w, b)
	}
	require.LessOrEqual(t, w.s.Now.Sub(restart), LiveGrace+BeatDeadline, "back within one beat plus the grace of the restart, never at the read deadline")
	backInTheDeal(t, w, rc, gen)
}

func TestAWorkingReadTheBeatNamesIsNeverReturned(t *testing.T) {
	t.Parallel()
	w, rc, gen, b := takenRead(t)
	for range 50 {
		w.tick(LiveGrace / 2)
		b = memberBeat(b, w.s.Now, LiveKey(rc.ID, gen))
		reconcile(w, b)
	}
	require.Equal(t, Working, w.s.Fleet.Card(rc.ID).Col, "a read its beat names runs as long as it runs (the deadline is the deal's, not the reconcile's)")
	b = memberBeat(b, w.s.Now, LiveKey(rc.ID, gen)+":"+LiveHeld)
	w.tick(LiveGrace * 3)
	b = memberBeat(b, w.s.Now, LiveKey(rc.ID, gen)+":"+LiveHeld)
	reconcile(w, b)
	require.Equal(t, Working, w.s.Fleet.Card(rc.ID).Col, "nor one named held (its report waits)")
	// a silent member is no evidence: a stale beat names nothing and returns nothing
	w.tick(10 * LiveGrace)
	reconcile(w, b)
	require.Equal(t, Working, w.s.Fleet.Card(rc.ID).Col, "a stale beat returns nothing")
	require.Empty(t, w.s.Fleet.Card(rc.ID).F("retired_by"))
}
