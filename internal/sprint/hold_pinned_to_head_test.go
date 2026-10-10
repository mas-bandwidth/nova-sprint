package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A hold pinned to an older head is not a hold at the current head (the
// roadmap entry hold-pinned-to-head-sha): a broken read — the hold a read puts
// on a primary in review, its finding the read-broken rule's fix — whose head
// is not the primary's head does not stand when a later read at the head
// stands, as an ok read at an older head already does not (reviewJudgment,
// okReaders, readHeadMatches).

func pinnedToHeadRead(id, primary string, attempt int, row, col, reader, head, verdict, finding string) *Card {
	fields := map[string]string{"kind": "read", "primary": primary, "attempt": itoa(attempt),
		"reader": reader, "verdict": verdict, "head": head}
	if finding != "" {
		fields["finding"] = finding
	}
	return &Card{ID: id, Row: row, Col: col, Fields: fields}
}

// A broken read of an older head is no hold at the head the card is at: its
// finding is no finding here, its reader no finder, and the read-broken rule
// reworks nothing on it. A later ok read at the head stands, and the card's
// reads go on being asked, not judged exhausted on the stale hold.
func TestABrokenReadPinnedToAnOlderHeadIsNotAHoldAtTheCurrentHead(t *testing.T) {
	t.Parallel()
	const (
		old = "1111111111111111111111111111111111111111"
		now = "2222222222222222222222222222222222222222"
	)
	w := newWorld(t, "reader-a", "reader-b")
	putReview(w, "s1-1", "s1-1: the work (s1) tier: pro\nPATHS: main.go\n", 2, 1, now)
	w.s.Readers.Put(pinnedToHeadRead(ReadCardID("s1-1", 2, "reader-a"), "s1-1", 2, "reader-a", Broken, "reader-a", old, "broken", "main.go is wrong"))
	w.s.Readers.Put(pinnedToHeadRead(ReadCardID("s1-1", 2, "reader-b"), "s1-1", 2, "reader-b", OK, "reader-b", now, "ok", ""))

	pr := w.s.Work.Card("s1-1")
	require.Empty(t, brokenFindings(w.s, pr), "a broken read pinned to an older head names no finding at this head")
	require.Equal(t, "", finderOf(w.s, pr), "the finder of this attempt is a reader of its head, not of an older one")

	a := RuleAnswer{Subject: "s1-1"}
	ruleReadBroken(w.s, &a)
	require.False(t, a.Answers(), "the read-broken rule answers nothing on an older head's finding: %s (%s)", a.Act, a.Why)

	j, ok := reviewJudgment(w.s, pr, reviewStep{})
	require.False(t, ok, "the reads go on being asked, not judged exhausted on an older head's broken read: %s %s", j.Type, j.What)
}

// A broken read whose head pins nothing (a read asked before a head was
// recorded) stands as it did: only a sha that is not the PR head is ignored.
func TestABrokenReadWithNoHeadStillStands(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	putReview(w, "s1-1", "s1-1: the work (s1)\nPATHS: main.go\n", 2, 1, "2222222222222222222222222222222222222222")
	w.s.Readers.Put(pinnedToHeadRead(ReadCardID("s1-1", 2, "reader-a"), "s1-1", 2, "reader-a", Broken, "reader-a", "", "broken", "main.go is wrong"))

	pr := w.s.Work.Card("s1-1")
	require.Equal(t, "main.go is wrong", brokenFindings(w.s, pr), "a read that pins no head is not one pinned to an older head")
}

// A friend's broken read stands the same way: one pinned to an older head does
// not, one that pins no head does, and a later ok read at the head stands.
func TestAFriendsBrokenReadPinnedToAnOlderHeadDoesNotStand(t *testing.T) {
	t.Parallel()
	const (
		old = "1111111111111111111111111111111111111111"
		now = "2222222222222222222222222222222222222222"
	)
	w := newWorld(t)
	putReview(w, "s1-1", "s1-1: the work (s1)\nPATHS: main.go\n", 2, 1, now)
	oldCard := pinnedToHeadRead(ReadCardID("s1-1", 2, "amy"), "s1-1", 2, "", "", "amy", old, "broken", "main.go is wrong")
	w.s.Fleet.Put(oldCard)
	pr := w.s.Work.Card("s1-1")
	_, oks, broken := friendReadLive(w.s, pr)
	require.Empty(t, broken, "a friend's broken read pinned to an older head does not stand")
	require.Empty(t, oks)

	nowCard := pinnedToHeadRead(ReadCardID("s1-1", 2, "bea"), "s1-1", 2, "", "", "bea", now, "broken", "main.go:1 is wrong")
	w.s.Fleet.Put(nowCard)
	_, oks, broken = friendReadLive(w.s, pr)
	require.Len(t, broken, 1, "a friend's broken read at the head stands")
	require.Equal(t, "bea", broken[0].F("reader"))
	require.Empty(t, oks)

	w.s.Fleet.Drop(oldCard.ID)
	w.s.Fleet.Put(pinnedToHeadRead(ReadCardID("s1-1", 2, "amy"), "s1-1", 2, "", "", "amy", "", "broken", "main.go is wrong"))
	_, oks, broken = friendReadLive(w.s, pr)
	require.Len(t, broken, 2, "a friend's broken read that pins no head stands as it did")

	w.s.Fleet.Drop(ReadCardID("s1-1", 2, "amy"))
	w.s.Fleet.Drop(nowCard.ID)
	w.s.Fleet.Put(pinnedToHeadRead(ReadCardID("s1-1", 2, "amy"), "s1-1", 2, "", "", "amy", now, "ok", ""))
	_, oks, _ = friendReadLive(w.s, pr)
	require.Len(t, oks, 1, "a friend's ok read at the head stands")
}
