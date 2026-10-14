package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAReadersSecondVerdictAnswersAlreadyRecorded pins a reader's second
// submission of a verdict already recorded on her own read: it is a stutter,
// answered ALREADY RECORDED, exit 0, writing nothing, while a different verdict
// on that read is refused before any write (this card,
// a-readers-second-verdict-answers-already-recorded; docs/SPEC-SPRINT.md, a
// reader who reports a verdict already recorded for its read).
func TestAReadersSecondVerdictAnswersAlreadyRecorded(t *testing.T) {
	t.Parallel()

	// (a) the outbox path, unchanged: FriendReadCloseChecked closes her read
	// broken from her report and a finding.
	w, _, first := friendReadWorld(t)
	w.must(FriendReadCloseChecked(w.s, "amy", "s1-1", first.ID, "Verdict: HOLD\nmain.go is wrong\n", nil))
	rc := w.s.Fleet.Card(first.ID)
	require.False(t, rc.Placed(), "the outbox report retires the read")
	require.Equal(t, "read", rc.F("retired_by"), "retired by the read")
	require.Equal(t, "broken", rc.F("verdict"))
	retired := rc.F("retired")
	require.NotEmpty(t, retired)

	// (b) the same verdict again: no refusal, no units, no notes, one Said line
	p := Read(w.s, ReadReq{Sel: Sel{IDs: []string{first.ID}}, As: FriendRow("amy"),
		Verdict: "broken", Finding: "main.go is wrong"})
	require.Empty(t, p.Refused, "a second submission of the recorded verdict is not refused")
	require.Empty(t, p.Units, "nothing is written: no note, no judgment, no consumer")
	require.Empty(t, p.Notes, "no judgment, no happened note")
	require.Len(t, p.Said, 1)
	require.True(t, strings.HasPrefix(p.Said[0], "ALREADY RECORDED broken at "), p.Said[0])
	require.Contains(t, p.Said[0], retired, "it names the fixed time it retired at")
	require.Contains(t, p.Said[0], first.ID, "and the read id")

	// (c) a different verdict is refused, honestly, and writes nothing
	p = Read(w.s, ReadReq{Sel: Sel{IDs: []string{first.ID}}, As: FriendRow("amy"), Verdict: "ok"})
	require.Empty(t, p.Units)
	require.Len(t, p.Refused, 1)
	require.True(t, strings.HasPrefix(p.Refused[0].Why, "already recorded broken at "), p.Refused[0].Why)
	require.Contains(t, p.Refused[0].Why, "a reader's verdict at an attempt is final")

	// (d) another retire reason keeps its current answer
	w2, _, first2 := friendReadWorld(t)
	w2.place(w2.s.Fleet, first2.ID, "", "")
	rc2 := w2.s.Fleet.Card(first2.ID)
	rc2.Fields["retired"], rc2.Fields["retired_by"] = stamp(w2.s.Now), RetiredByReturned
	p = Read(w2.s, ReadReq{Sel: Sel{IDs: []string{first2.ID}}, As: FriendRow("amy"), Verdict: "ok"})
	require.Empty(t, p.Units)
	require.Len(t, p.Refused, 1)
	require.Contains(t, p.Refused[0].Why, "by returned: nothing to report", "a returned read is not a verdict")

	// (e) the outbox line names one way and says plainly not to double-submit
	line := FriendReadOutboxLine(FriendRow("amy"), first.ID, 20)
	require.Contains(t, line, "do not also run nova-sprint read")
}
