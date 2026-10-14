package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAReadersSecondVerdictAnswersAlreadyRecorded pins a second verdict on a
// read friend sync already closed (card
// a-readers-second-verdict-answers-already-recorded; docs/SPEC-SPRINT.md, a
// friend's read). The same verdict is ALREADY RECORDED, exit path with no
// write; a different one is refused; another retired_by is unchanged.
func TestAReadersSecondVerdictAnswersAlreadyRecorded(t *testing.T) {
	t.Parallel()
	const finding = "main.go:12 drops the error; return it"
	w, id := friendReadAsked(t)
	require.Equal(t, t0, w.s.Now, "the snapshot clock is the fixed time passed in")

	closed := FriendReadCloseChecked(w.s, "amy", "s1-1", id, "Verdict: HOLD\n"+finding+"\n", nil)
	require.Empty(t, closed.Refused)
	require.Len(t, closed.Units, 1, "the outbox path closes the read in one unit")
	var set map[string]string
	for _, ch := range closed.Units[0].Changes {
		if ch.Table == Fleet {
			set = ch.Entry.Set
		}
	}
	require.NotNil(t, set)
	require.Equal(t, "read", set["retired_by"])
	require.Equal(t, "broken", set["verdict"])
	w.must(closed)
	rc := w.s.Fleet.Card(id)
	require.False(t, rc.Placed())
	require.Equal(t, "broken", rc.F("verdict"))
	require.Equal(t, "read", rc.F("retired_by"))

	again := Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: FriendRow("amy"), Verdict: "broken", Finding: finding})
	require.Empty(t, again.Refused)
	require.Empty(t, again.Units)
	require.Empty(t, again.Notes)
	require.Empty(t, again.Props, "a stutter writes no window ledger entry")
	require.Len(t, again.Said, 1)
	require.True(t, strings.HasPrefix(again.Said[0], "ALREADY RECORDED broken at "+stamp(t0)), "said %q", again.Said[0])
	require.Contains(t, again.Said[0], "("+id+")")
	require.Equal(t, "broken", w.s.Fleet.Card(id).F("verdict"), "nothing was written")

	other := Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: FriendRow("amy"), Verdict: "ok"})
	require.Empty(t, other.Units)
	require.Len(t, other.Refused, 1)
	require.True(t, strings.HasPrefix(other.Refused[0].Why, "already recorded broken at "), "why %q", other.Refused[0].Why)

	back, bid := friendReadAsked(t)
	back.must(Read(back.s, ReadReq{Sel: Sel{IDs: []string{bid}}, As: FriendRow("amy"), Return: true, Reason: "no finding to give"}))
	require.Equal(t, RetiredByReturned, back.s.Fleet.Card(bid).F("retired_by"))
	kept := Read(back.s, ReadReq{Sel: Sel{IDs: []string{bid}}, As: FriendRow("amy"), Verdict: "ok"})
	require.Empty(t, kept.Units)
	require.Len(t, kept.Refused, 1)
	require.Equal(t, "retired at "+stamp(t0)+" by returned: nothing to report", kept.Refused[0].Why)

	line := FriendReadOutboxLine(FriendRow("amy"), id, 20)
	require.Contains(t, line, "do not also run nova-sprint read")
	require.NotContains(t, line, "or run: nova-sprint read")
}
