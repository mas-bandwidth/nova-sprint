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

