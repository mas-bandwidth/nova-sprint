package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A reader's second verdict on a read already closed by her own verdict is answered ALREADY
// RECORDED, exit 0, and writes nothing; a different verdict is refused (docs/SPEC-SPRINT.md,
// "A reader's second verdict"). The outbox path closes the read once (retired_by read), and a
// harness whose child wrote the report and whose session also runs the read verb submits
// twice: the second submission must not be read as "my verdict was not filed".
func TestAReadersSecondVerdictAnswersAlreadyRecorded(t *testing.T) {
	t.Parallel()

	t.Run("the outbox path closes it", func(t *testing.T) {
		t.Parallel()
		w, id := friendReadAsked(t)
		p := FriendReadCloseChecked(w.s, "amy", "s1-1", "", "Verdict: HOLD\nmain.go is wrong\n", nil)
		require.Len(t, p.Units, 1)
		require.Empty(t, p.Refused)
		w.must(p)
		rc := w.s.Fleet.Card(id)
		require.False(t, rc.Placed())
		require.Equal(t, "read", rc.F("retired_by"))
		require.Equal(t, "broken", rc.F("verdict"))
	})

	t.Run("the same verdict is a stutter", func(t *testing.T) {
		t.Parallel()
		w, id := friendReadAsked(t)
		w.must(FriendReadCloseChecked(w.s, "amy", "s1-1", "", "Verdict: HOLD\nmain.go is wrong\n", nil))
		p := Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: FriendRow("amy"), Verdict: "broken",
			Finding: "main.go:12 drops the error; return it", Who: FriendRow("amy")})
		require.Empty(t, p.Refused)
		require.Empty(t, p.Units)
		require.Empty(t, p.Notes)
		require.Len(t, p.Said, 1, "one NOTE line, nothing written: %v", p.Said)
		require.True(t, strings.HasPrefix(p.Said[0], "ALREADY RECORDED broken at "+stamp(t0)+" "), "said: %v", p.Said)
	})

	t.Run("a different verdict is refused", func(t *testing.T) {
		t.Parallel()
		w, id := friendReadAsked(t)
		w.must(FriendReadCloseChecked(w.s, "amy", "s1-1", "", "Verdict: HOLD\nmain.go is wrong\n", nil))
		p := Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: FriendRow("amy"), Verdict: "ok", Who: FriendRow("amy")})
		require.Len(t, p.Refused, 1)
		require.True(t, strings.HasPrefix(p.Refused[0].Why, "already recorded broken at "+stamp(t0)), "why: %s", p.Refused[0].Why)
		require.Contains(t, p.Refused[0].Why, "a reader's verdict at an attempt is final, and a different one is not taken")
		require.Empty(t, p.Units)
	})

	t.Run("a read retired by returned keeps its refusal", func(t *testing.T) {
		t.Parallel()
		w, id := friendReadAsked(t)
		w.must(Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: FriendRow("amy"), Return: true, Reason: "not now", Who: FriendRow("amy")}))
		require.Equal(t, RetiredByReturned, w.s.Fleet.Card(id).F("retired_by"))
		p := Read(w.s, ReadReq{Sel: Sel{IDs: []string{id}}, As: FriendRow("amy"), Verdict: "ok", Who: FriendRow("amy")})
		require.Len(t, p.Refused, 1)
		require.Equal(t, "retired at "+stamp(t0)+" by returned: nothing to report", p.Refused[0].Why)
		require.Empty(t, p.Units)
	})

	t.Run("the outbox line names one way", func(t *testing.T) {
		t.Parallel()
		line := FriendReadOutboxLine(FriendRow("amy"), "s1-1.r1.amy", 20)
		require.Contains(t, line, "do not also run nova-sprint read")
		require.NotContains(t, line, "or run: nova-sprint read --as")
	})
}
