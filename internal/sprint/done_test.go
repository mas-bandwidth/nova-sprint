package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A row's done counts every card it completed, each kind by its rule (DoneKinds), from the log
// lines that complete them, a friend's row and a member's alike: a work card and a fix card the
// readers' verdict moved to ok, a read card retired off its reader's row with a verdict, ok or
// broken, and a readers-table read counted on its reader's machine. A failed verdict, a read
// handed back with no verdict and a judgment complete nothing; a card is counted once.
func TestDoneCountsEveryKindOfCardFromTheLinesThatCompleteIt(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
	move := func(table, card, from, to string) Line {
		return Line{Kind: "move", At: at, Card: card, Table: table, From: from, To: to}
	}
	retire := func(card, from, verdict, by string) Line {
		set := map[string]string{"retired_by": by}
		if verdict != "" {
			set["verdict"] = verdict
		}
		return Line{Kind: "move", At: at, Card: card, Table: Fleet, From: from, Removed: true, Set: set}
	}
	for _, tc := range []struct {
		row   string
		lines []Line
		want  map[string]int
	}{
		{row: FriendRow("stella"), lines: []Line{
			move(Fleet, "s1-1.w1", "friend.stella:finished", "friend.stella:ok"),
			move(Fleet, "s1-2.w1", "friend.stella:finished", "friend.stella:ok"),
			move(Fleet, "s1-6.w1", "friend.stella:finished", "friend.stella:failed"),
			move(Fleet, "s1-6.w2", "friend.stella:finished", "friend.stella:ok"),
			retire("s1-4.r1.stella", "friend.stella:working", "ok", "read"),
			retire("s1-5.r1.stella", "friend.stella:working", "broken", "read"),
			retire("s1-8.r1.stella", "friend.stella:working", "", RetiredByReturned),
			move(Fleet, "s1-1.w1", "friend.stella:finished", "friend.stella:ok"), // the same card again
		}, want: map[string]int{DoneKindWork: 2, DoneKindFix: 1, DoneKindRead: 2}},
		{row: "m1", lines: []Line{
			move(Fleet, "s1-7.w1", "m1:finished", "m1:ok"),
			retire("s1-1.r1.m1", "m1:working", "ok", "read"),
			move(Readers, "s1-2.r1.reader-m1", "reader-m1:reading", "reader-m1:broken"),
			{Kind: "note", At: at, Note: &Note{Type: NReadBroken}},
		}, want: map[string]int{DoneKindWork: 1, DoneKindRead: 2}},
	} {
		var cs []Completion
		for _, l := range tc.lines {
			cs = append(cs, CompletionsOf(l)...)
		}
		got := CountDone(cs, time.Time{})
		assert.Equal(t, tc.want, got[tc.row], tc.row)
		n := 0
		for _, v := range tc.want {
			n += v
		}
		assert.Equal(t, n, got.Total(tc.row), "%s's done is every kind summed", tc.row)
		assert.Zero(t, CountDone(cs, at).Total(tc.row), "%s: a stats reset's mark counts what completes after it", tc.row)
	}
}

// A kind is one entry: a kind registered in DoneKinds counts in every row's done with nothing
// else changed (a merge card, once merges are dealt cards).
func TestANewDoneKindIsOneEntry(t *testing.T) {
	was := DoneKinds
	t.Cleanup(func() { DoneKinds = was })
	DoneKinds = append(append([]DoneKind(nil), was...), DoneKind{Name: "merge", Of: func(l Line) []Completion {
		if l.Table != Merge || l.To != "s1:merged" {
			return nil
		}
		return []Completion{{Kind: "merge", Card: l.Card, Row: "lander", At: l.At}}
	}})
	cs := CompletionsOf(Line{Kind: "move", At: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), Card: "s1-1", Table: Merge, From: "s1:queued", To: "s1:merged"})
	assert.Equal(t, 1, CountDone(cs, time.Time{}).Total("lander"))
}
