package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Ghost reads (v1.2.15 BLOCKER, a-dealt-read-is-held-or-returned-and-ask-agrees-with-
// itself). The reads of a primary's attempt live in two places, the readers table and
// the fleet read cards (readers.go, ReadsOut). Before this card, ask and ask --another
// counted different tables: `ask` counted ReadWanted (readers.go), which counts fleet
// read cards through friendReadLive, while `ask --another` counted readsAt, the readers
// table only (steps_review.go). With read cards on, one verb refused a card as "asked
// already: its reads are out" while the other refused it as "not asked yet", so no ask
// reached a fleet read card (docs/SPEC-SPRINT.md section 6, the reads). ReadsOut is the
// one predicate; this pins that the two verbs agree.

// readStatesOf builds each state of the table the test walks, on a world with the
// primary s1-1 in review at attempt 1 (a flash card: one read) whose work card m1
// worked. The reader rd1 is on the readers table; the fleet read cards are on member
// and friend rows as the read-card deal leaves them.
func askAgreeState(t *testing.T, state string) *world {
	t.Helper()
	w := newWorld(t, "rd1", "rd2")
	putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
	add := func(c *Card) { w.s.Fleet.Put(c) }
	switch state {
	case "none":
	case "readers-table read":
		w.s.Readers.Put(&Card{ID: ReadCardID("s1-1", 1, "rd1"), Row: "rd1", Col: Asked, Rev: 1, Fields: map[string]string{
			"kind": "read", "primary": "s1-1", "reader": "rd1", "attempt": "1", "stream": "s1", "head": "head-s1-1"}})
	case "fleet read ready":
		add(&Card{ID: ReadCardID("s1-1", 1, "m2"), Row: "m2", Col: Ready, Rev: 1, Fields: map[string]string{
			"kind": "read", "primary": "s1-1", "reader": "m2", "attempt": "1", "stream": "s1", "head": "head-s1-1", "gen": "1", FieldReadCard: "1"}})
	case "fleet read working":
		add(&Card{ID: ReadCardID("s1-1", 1, "m2"), Row: "m2", Col: Working, Rev: 1, Fields: map[string]string{
			"kind": "read", "primary": "s1-1", "reader": "m2", "attempt": "1", "stream": "s1", "head": "head-s1-1", "gen": "1", FieldReadCard: "1"}})
	case "ghost":
		// a read card working on a row whose beat does not name it: with the beats
		// the reconcile sees, ReadsOut marks it ghost and it does not stand
		add(&Card{ID: ReadCardID("s1-1", 1, "m2"), Row: "m2", Col: Working, Rev: 1, Fields: map[string]string{
			"kind": "read", "primary": "s1-1", "reader": "m2", "attempt": "1", "stream": "s1", "head": "head-s1-1", "gen": "1", FieldReadCard: "1"}})
	case "friend read":
		add(&Card{ID: ReadCardID("s1-1", 1, "fr1"), Row: FriendRow("fr1"), Col: Working, Rev: 1, Fields: map[string]string{
			"kind": "read", "primary": "s1-1", "reader": "fr1", "attempt": "1", "stream": "s1", "head": "head-s1-1", "gen": "1"}})
	case "closed ok":
		add(&Card{ID: ReadCardID("s1-1", 1, "m3"), Rev: 1, Fields: map[string]string{
			"kind": "read", "primary": "s1-1", "reader": "m3", "attempt": "1", "stream": "s1", "head": "head-s1-1", "gen": "1", "verdict": "ok"}})
	default:
		t.Fatalf("unknown state %q", state)
	}
	return w
}

// TestAskAndAskAnotherAgreeOnAsked is the card's table: over {none, readers-table
// read, fleet read ready, fleet read working, ghost, friend read, closed ok}, exactly
// one of ask and ask --another is refused, never both (docs/SPEC-SPRINT.md section 6,
// the reads).
func TestAskAndAskAnotherAgreeOnAsked(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"none", "readers-table read", "fleet read ready", "fleet read working", "ghost", "friend read", "closed ok"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			w := askAgreeState(t, state)
			pAsk := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}})
			pAnother := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Another: true})
			refusedAsk := len(pAsk.Refused) > 0
			refusedAnother := len(pAnother.Refused) > 0
			require.NotEqual(t, refusedAsk, refusedAnother,
				"ask refused=%v (%v), ask --another refused=%v (%v): exactly one is refused, never both",
				refusedAsk, pAsk.Refused, refusedAnother, pAnother.Refused)
		})
	}
}

// TestADealtReadAbsentFromItsMembersQueueIsReturnedAndAskedAgain pins the held bound
// (docs/SPEC-SPRINT.md section 6, the reads): a working read card whose row's fresh beat
// names it held past HeldGrace is a ghost, retired lost (spending no one) and dealt to
// another unit in the same tick's deal (tla-readcards-review-has-a-live-read-or-a-judgment).
func TestADealtReadAbsentFromItsMembersQueueIsReturnedAndAskedAgain(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2", "m3")
	putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
	dealReads(t, w, nil)
	reads := readCardsOf(w, "s1-1")
	require.Len(t, reads, 1)
	rc := reads[0]
	first := rc.Row
	w.must(Take(w.s, TakeReq{As: first, Sel: Sel{Limit: -1}, Who: first}))
	rc = w.s.Fleet.Card(rc.ID)
	require.Equal(t, Working, rc.Col)
	gen := max(rc.Int("gen"), 1)
	b := memberBeat(Beat{}, w.s.Now, LiveKey(rc.ID, gen))
	reconcile(w, b) // named running: alive

	b = memberBeat(b, w.s.Now, LiveKey(rc.ID, gen)+":"+LiveHeld)
	reconcile(w, b)
	require.Equal(t, Working, w.s.Fleet.Card(rc.ID).Col, "named held, within HeldGrace: the read stays working")
	require.NotEmpty(t, w.s.Fleet.Card(rc.ID).F("held_since"), "the held mark is recorded")

	w.tick(HeldGrace + time.Second)
	b = memberBeat(b, w.s.Now, LiveKey(rc.ID, gen)+":"+LiveHeld)
	reconcile(w, b)
	old := w.s.Fleet.Card(rc.ID)
	require.NotNil(t, old)
	require.False(t, old.Placed(), "the held read is off the member's row")
	require.Equal(t, RetiredByLost, old.F("retired_by"), "retired lost: no reader spent")
	require.Equal(t, gen, old.Int("lost_from_gen"))
	require.Contains(t, old.F("lost_reason"), "held")
	dealReads(t, w, nil)
	again := readCardsOf(w, "s1-1")
	require.Len(t, again, 1, "the read is dealt again in the same tick's deal")
	require.NotEqual(t, rc.ID, again[0].ID, "a fresh card of the attempt")
	require.NotEqual(t, first, again[0].Row, "dealt to another unit")
}

// TestASecondGhostAtOneAttemptPushesAJudgment pins the one judgment (docs/SPEC-SPRINT.md
// section 6, the reads): a read card whose row's beat names it held past HeldGrace is a
// ghost; the second ghost at one primary's attempt pushes one judgment naming each card,
// row and reason (tla-readcards-review-has-a-live-read-or-a-judgment).
func TestASecondGhostAtOneAttemptPushesAJudgment(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2")
	putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
	for i := 0; i < 2; i++ {
		dealReads(t, w, nil)
		reads := readCardsOf(w, "s1-1")
		require.Len(t, reads, 1)
		rc := reads[0]
		first := rc.Row
		w.must(Take(w.s, TakeReq{As: first, Sel: Sel{Limit: -1}, Who: first}))
		rc = w.s.Fleet.Card(rc.ID)
		gen := max(rc.Int("gen"), 1)
		b := memberBeat(Beat{}, w.s.Now, LiveKey(rc.ID, gen))
		reconcile(w, b)
		b = memberBeat(b, w.s.Now, LiveKey(rc.ID, gen)+":"+LiveHeld)
		reconcile(w, b)
		w.tick(HeldGrace + time.Second)
		b = memberBeat(b, w.s.Now, LiveKey(rc.ID, gen)+":"+LiveHeld)
		reconcile(w, b)
		require.False(t, w.s.Fleet.Card(rc.ID).Placed(), "ghost %d retired lost", i+1)
	}
	var lost []Note
	for _, n := range w.notes {
		if n.Type == NLostTwice {
			lost = append(lost, n)
		}
	}
	require.Len(t, lost, 1, "one judgment for two ghosts at one attempt: %v", w.notes)
	require.Contains(t, lost[0].Primaries, "s1-1")
}

// TestTheConsumersOutCheckIsNotVacuous pins (d): a working primary with no work card is a
// breach while another card works elsewhere, so the check keys on the primary's own work
// card, never "any fleet card anywhere is working" (docs/SPEC-SPRINT.md section 6, the
// reads).
func TestTheConsumersOutCheckIsNotVacuous(t *testing.T) {
	t.Parallel()
	s := consumersOutSnap(
		[]*Card{
			{ID: "s1-1", Row: "s1", Col: Working, Fields: map[string]string{}},
			{ID: "s1-2", Row: "s1", Col: Working, Fields: map[string]string{"work": "s1-2.w1"}},
		},
		[]*Card{
			{ID: "s1-2.w1", Row: "m1", Col: Working, Fields: map[string]string{"gen": "1"}},
			{ID: CtlID("m1"), Row: "m1", Col: Ctl, Fields: map[string]string{"status": Up}},
		},
		nil,
	)
	require.Equal(t, []string{"s1-1"}, quietSubjects(ConsumersOut(s)),
		"s1-1 has no work card: a breach, while s1-2's work card works elsewhere")
}

// TestAReviewCardWithNoReadAndNoJudgmentIsImpossible pins (d) and (e): after a tick a
// review primary with no live read and no judgment does not exist — a live fleet read card
// holds it, and no read and no judgment is a breach; a broken read with an empty finding is
// returned, not a verdict (docs/SPEC-SPRINT.md section 6, the reads).
func TestAReviewCardWithNoReadAndNoJudgmentIsImpossible(t *testing.T) {
	t.Parallel()
	t.Run("a live fleet read card holds it", func(t *testing.T) {
		t.Parallel()
		w := readCardsWorld(t, 4, "m1", "m2")
		putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
		dealReads(t, w, nil)
		require.Len(t, readCardsOf(w, "s1-1"), 1, "its read card is dealt")
		require.Empty(t, quietSubjects(ConsumersOut(w.s)), "a fleet read card is a live read out")
	})
	t.Run("no read and no judgment is a breach", func(t *testing.T) {
		t.Parallel()
		w := readCardsWorld(t, 4, "m1", "m2")
		putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
		require.Equal(t, []string{"s1-1"}, quietSubjects(ConsumersOut(w.s)),
			"a review primary with no read and no judgment is a breach")
	})
	t.Run("a broken read with an empty finding is returned", func(t *testing.T) {
		t.Parallel()
		w := readCardsWorld(t, 4, "m1", "m2")
		putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
		dealReads(t, w, nil)
		rc := readCardsOf(w, "s1-1")[0]
		w.must(Take(w.s, TakeReq{As: rc.Row, Sel: Sel{Limit: -1}, Who: rc.Row}))
		p := readCardVerb(w.s, ReadReq{Sel: Sel{IDs: []string{rc.ID}}, As: rc.Row, Verdict: "broken", Finding: "  ", Who: rc.Row}, rc.Row, rc.Row)
		w.must(p)
		require.Equal(t, RetiredByReturned, w.s.Fleet.Card(rc.ID).F("retired_by"), "an empty finding is no verdict")
		require.Empty(t, w.s.Fleet.Card(rc.ID).F("verdict"), "no verdict is written")
	})
}

// TestAskInsteadTakesBackAFleetReadCard pins (b) under read cards: ask --instead <unit>
// takes back that unit's live fleet read card (retired_by coordinator, spending no one) and
// deals it to another unit in the same step; ask --another cuts one more fleet read card
// through the read-card deal (docs/SPEC-SPRINT.md section 6, the reads).
func TestAskInsteadTakesBackAFleetReadCard(t *testing.T) {
	t.Parallel()
	t.Run("instead takes back a fleet read card", func(t *testing.T) {
		t.Parallel()
		w := readCardsWorld(t, 4, "m1", "m2", "m3")
		putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
		dealReads(t, w, nil)
		reads := readCardsOf(w, "s1-1")
		require.Len(t, reads, 1)
		rc := reads[0]
		first := rc.Row
		p := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Instead: first})
		require.Empty(t, p.Refused, "instead takes back the live fleet read: %v", p.Refused)
		w.must(p)
		old := w.s.Fleet.Card(rc.ID)
		require.NotNil(t, old)
		require.False(t, old.Placed(), "the taken-back read is off its row")
		require.Equal(t, RetiredByCoordinator, old.F("retired_by"), "retired_by coordinator, spending no one")
		again := readCardsOf(w, "s1-1")
		require.Len(t, again, 1, "dealt again in the same step")
		require.NotEqual(t, rc.ID, again[0].ID, "a fresh card of the attempt")
		require.NotEqual(t, first, again[0].Row, "dealt to another unit")
	})
	t.Run("another cuts one more fleet read card", func(t *testing.T) {
		t.Parallel()
		w := readCardsWorld(t, 4, "m1", "m2", "m3")
		putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
		dealReads(t, w, nil)
		require.Len(t, readCardsOf(w, "s1-1"), 1, "a flash card's one read is cut")
		p := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Another: true})
		require.Empty(t, p.Refused, "another cuts one more read: %v", p.Refused)
		w.must(p)
		require.Len(t, readCardsOf(w, "s1-1"), 2, "one more read card is cut")
	})
}

// TestAFriendDealtReadIsAGhostByHerOwnBeat pins the friend-row beat resolution of
// readCardGhost: a friend's beat is kept under her own name, not her row (Store.fleetBeats,
// RowBeat), so a friend-dealt read card working on friend.<name> is read as a ghost by her
// beat exactly as a member's is. Before this, readCardGhost keyed the beat on c.Row only, so
// every friend read was read as live (docs/SPEC-SPRINT.md section 6, the reads).
func TestAFriendDealtReadIsAGhostByHerOwnBeat(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "rd1", "rd2")
	putReviewBy(w, "s1-1", "s1-1: work (s1)\n", "m1", 1)
	rc := &Card{ID: ReadCardID("s1-1", 1, "fr1"), Row: FriendRow("fr1"), Col: Working, Rev: 1, Fields: map[string]string{
		"kind": "read", "primary": "s1-1", "reader": "fr1", "attempt": "1", "stream": "s1", "head": "head-s1-1", "gen": "1", FieldReadCard: "1"}}
	w.s.Fleet.Put(rc)
	gen := max(rc.Int("gen"), 1)
	// the friend's beat, kept under her name "fr1", names nothing since before LiveGrace
	b := memberBeat(Beat{}, w.s.Now, LiveKey(rc.ID, gen))
	w.tick(LiveGrace + time.Second)
	b = memberBeat(b, w.s.Now)
	w.s.Beats = map[string]Beat{"fr1": b}
	ghost, why := readCardGhost(w.s, rc)
	require.True(t, ghost, "a friend read is a ghost by her own beat: %s", why)
	var state string
	for _, ro := range ReadsOut(w.s, w.s.Work.Card("s1-1")) {
		if ro.Card.ID == rc.ID {
			state = ro.State
		}
	}
	require.Equal(t, ReadGhost, state, "ReadsOut marks the friend ghost, never live")
}

// TestDoctorCountsFleetReadCards pins the doctor's readers check counting the fleet read
// cards the readers-table check cannot see (docs/SPEC-SPRINT.md section 6, the reads): a
// read card working on a row whose fresh beat has not named it for LiveGrace is a ghost,
// and the readers check FAILs naming it.
func TestDoctorCountsFleetReadCards(t *testing.T) {
	t.Parallel()
	w, rc, gen, b := takenRead(t)
	w.tick(LiveGrace + time.Second)
	b = memberBeat(b, w.s.Now) // a fresh beat naming nothing since its first: the read is not run
	w.s.Beats = map[string]Beat{"m2": b}
	in := DoctorInput{Snap: w.s, Req: TickReq{Beats: w.s.Beats}, ReaderBeats: map[string]Beat{"reader-m1": {At: w.s.Now}, "reader-m2": {At: w.s.Now}}}
	cs := DoctorReaders(in)
	fails := failsOn(cs, CheckReaders)
	require.NotEmpty(t, fails, "a ghost read card fails the readers check: %s", lines(cs))
	joined := lines(fails)
	require.Contains(t, joined, rc.ID, "the ghost names its card: %s", joined)
	require.Contains(t, joined, LiveKey(rc.ID, gen), "the ghost names the card at its generation: %s", joined)
}

