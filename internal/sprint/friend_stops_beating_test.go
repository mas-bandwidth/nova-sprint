package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A friend who stops beating is gone: her daemon alone runs her lanes, so the
// tick hands her dealt and taken cards and reads back to the deal with their
// carry heads kept, the same plan the run loop's friend reconcile applies
// (FriendReturn, readLostUnit); her session's pong is no evidence she is up, and
// the coordinator's return works on her read. docs/SPEC-SPRINT.md section 1,
// friend reconcile; docs/FRIENDS.md, "A friend's beat comes only from her daemon".
func TestAFriendWhoStopsBeatingIsDownAndHerCardsComeBack(t *testing.T) {
	t.Parallel()

	w := friendWorld(t, friendBrief("only friend amy"))
	dealStarted(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "amy holds a work card")
	require.Equal(t, Working, wc.Col)

	// two frontier reads asked of amy, on her fleet row, and started (her beat names them)
	putReview(w, "s1-2", "s1-2: work (s1) tier: frontier\n\nAS A READ\nread it\n", 1, 1, "head-1")
	putReview(w, "s1-3", "s1-3: work (s1) tier: frontier\n\nAS A READ\nread it\n", 1, 1, "head-2")
	askReaders(t, w, []FriendSeat{frontierSeat("amy", 2, Up, t.TempDir())})
	rid := ReadCardID("s1-2", 1, "amy")
	rid2 := ReadCardID("s1-3", 1, "amy")
	require.NotNil(t, w.s.Fleet.Card(rid), "amy holds a read")
	require.NotNil(t, w.s.Fleet.Card(rid2), "amy holds a second read")
	units, _ := friendStartUnits(w.s, []FriendSeat{{Name: "amy", Running: []string{rid, rid2}}})
	w.must(Plan{Units: units})
	require.Equal(t, Working, w.s.Fleet.Card(rid).Col)
	require.Equal(t, Working, w.s.Fleet.Card(rid2).Col)

	// her daemon stops beating past the beat bound; her session's pong is fresh
	now := w.s.Now
	beat := Beat{At: now.Add(-(FriendBeatBound + time.Second))}

	// presence: she is down since that beat, whatever her session's pong says
	fp := FriendPresence{
		Beat:       beat,
		Health:     FriendHealth{State: Up, Seen: now.Add(-time.Minute), Generation: 2},
		Generation: 2,
	}
	status, why := FriendEvidence(fp, now)
	require.Equal(t, Down, status, "a pong alone is no evidence her daemon beats, %s", why)
	require.Contains(t, why, "down since", "the row names where her down began")

	// the coordinator's return works on her read
	p := Return(w.s, ReturnReq{Sel: Sel{IDs: []string{rid}}, Reason: "she stopped beating", Who: "coordinator"})
	require.NotEmpty(t, p.Units, "the coordinator returns her read")
	w.must(p)
	require.False(t, w.s.Fleet.Card(rid).Placed(), "the coordinator's return retires her read")

	// the tick hands her remaining cards and read back to the deal
	got := w.do(LiveReturns(w.s, map[string]Beat{"amy": beat}, false))
	require.False(t, w.s.Fleet.Card("s1-1.w1").Placed(), "her work card comes back: %+v", got.Units)
	require.False(t, w.s.Fleet.Card(rid2).Placed(), "her read comes back")
	require.Equal(t, Ready, w.s.Work.Card("s1-1").Col, "her primary is ready for the deal again")
	require.Equal(t, "head-1", w.s.Work.Card("s1-2").F("head"), "the read keeps its carry head")
}
