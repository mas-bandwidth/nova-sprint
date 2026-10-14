package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A friend whose read was closed by her own verdict (retired_by read) and who
// re-submits that verdict is answered ALREADY RECORDED, exit 0, changed=no:
// no refusal, no unit, no note, no write. A different verdict is refused, honest
// that the recorded verdict is final. Every other retired_by keeps its answer
// (a-readers-second-verdict-answers-already-recorded, SPEC-SPRINT).
func TestAReadersSecondVerdictAnswersAlreadyRecorded(t *testing.T) {
	t.Parallel()
	w, id := friendReadAsked(t)
	p := FriendReadCloseChecked(w.s, "amy", "s1-1", "", "Verdict: HOLD\nmain.go:12 drops the error; return it\n", nil)
	require.Len(t, p.Units, 1)
	w.do(p)
	rc := w.s.Fleet.Card(id)
	require.False(t, rc.Placed(), "the outbox report retires the read")
	require.Equal(t, "broken", rc.F("verdict"))
	require.Equal(t, "read", rc.F("retired_by"))

	t.Run("the same verdict answers ALREADY RECORDED", func(t *testing.T) {
		t.Parallel()
		p := Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: FriendRow("amy"), Verdict: "broken", Finding: "main.go:12 drops the error; return it", Who: FriendRow("amy")})
		require.Empty(t, p.Refused)
		require.Empty(t, p.Units)
		require.Empty(t, p.Notes)
		require.Len(t, p.Said, 1)
		require.True(t, strings.HasPrefix(p.Said[0], "ALREADY RECORDED broken at "))
		require.Contains(t, p.Said[0], stamp(w.s.Now))
		require.Contains(t, p.Said[0], id)
	})

	t.Run("a different verdict is refused", func(t *testing.T) {
		t.Parallel()
		p := Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: FriendRow("amy"), Verdict: "ok", Who: FriendRow("amy")})
		require.Len(t, p.Refused, 1)
		require.True(t, strings.HasPrefix(p.Refused[0].Why, "already recorded broken at "))
		require.Empty(t, p.Units)
	})

	t.Run("a returned read keeps its refusal text", func(t *testing.T) {
		t.Parallel()
		ret, rid := friendReadAsked(t)
		ret.must(Read(ret.s, ReadReq{Sel: Sel{IDs: []string{rid}}, As: FriendRow("amy"), Verdict: "ok", Return: true, Reason: "not a read", Who: FriendRow("amy")}))
		p := Read(ret.s, ReadReq{Sel: Sel{IDs: []string{rid}}, As: FriendRow("amy"), Verdict: "ok", Who: FriendRow("amy")})
		require.Len(t, p.Refused, 1)
		require.True(t, strings.HasPrefix(p.Refused[0].Why, "retired at "))
		require.Contains(t, p.Refused[0].Why, "by returned: nothing to report")
		require.Empty(t, p.Units)
	})

	t.Run("the outbox line names one way", func(t *testing.T) {
		t.Parallel()
		line := FriendReadOutboxLine(FriendRow("amy"), id, 20)
		require.Contains(t, line, "do not also run nova-sprint read")
	})
}
