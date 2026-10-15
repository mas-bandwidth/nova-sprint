package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// friend take --all-unstarted never withdraws a working card (2026-10-14: "friend take aurora
// --all-unstarted" took back every card two friends were working, each "withdrawn gen=2"
// mid-flight). All selects only the cards dealt to her and not taken: ready on her row with
// no take, progress, report or carried head recorded, and not started. A working card goes
// back only named, with Working (friend take --working), and the plan warns once. The world
// is the in-memory snapshot, its clock w.s.Now: no store, no socket, no real time.

// workingWorld is amy at width 3 with five cards for her: s1-1..s1-3 working, s1-4 and s1-5
// ready (dealt, not taken).
func workingWorld(t *testing.T) *world {
	t.Helper()
	b := friendBrief("only friend amy")
	w := friendWorld(t, b, b, b, b, b)
	dealStarted(w, workerSeatFixture(FriendSeat{Name: "amy", Width: 3, Status: Up, Class: "flash"}))
	for _, id := range []string{"s1-1.w1", "s1-2.w1", "s1-3.w1"} {
		require.Equal(t, Working, w.s.Fleet.Card(id).Col, id)
	}
	for _, id := range []string{"s1-4.w1", "s1-5.w1"} {
		require.Equal(t, Ready, w.s.Fleet.Card(id).Col, id)
		require.Empty(t, FriendCardInFlight(w.s.Fleet.Card(id)), id)
	}
	return w
}

func movedCards(p Plan) []string {
	var out []string
	for _, u := range p.Units {
		out = append(out, strings.SplitN(u.Moved, " ", 2)[0])
	}
	return out
}

func TestAllUnstartedSelectsOnlyTheCardsDealtAndNotTaken(t *testing.T) {
	t.Parallel()
	w := workingWorld(t)
	// progress on a working card: the incident's shape
	w.s.Fleet.Card("s1-1.w1").Fields[FieldProgress] = stamp(w.s.Now)
	p := w.must(FriendTake(w.s, FriendTakeReq{Friend: "amy", All: true, Reason: "rebalance", Who: "rowan"}))
	assert.ElementsMatch(t, []string{"s1-4.w1", "s1-5.w1"}, movedCards(p), "exactly the two dealt and not taken")
	assert.Empty(t, p.Refused)
	for _, id := range []string{"s1-1.w1", "s1-2.w1", "s1-3.w1"} {
		assert.Equal(t, Working, w.s.Fleet.Card(id).Col, "%s: a working card stays with her", id)
		assert.Empty(t, w.s.Fleet.Card(id).F(FieldTakenBack), id)
	}
	for _, id := range []string{"s1-4.w1", "s1-5.w1"} {
		assert.Equal(t, Withdrawn, w.s.Fleet.Card(id).Col, id)
	}
	said := strings.Join(p.Said, "\n")
	for _, id := range []string{"s1-1.w1", "s1-2.w1", "s1-3.w1"} {
		assert.Contains(t, said, "friend amy keeps "+id+": it is working on the friend's row")
	}
	assert.NotContains(t, said, "WARNING")
	assert.Contains(t, said, "marked=2")
	assert.Empty(t, Check(w.s, nil))
}

func TestAllUnstartedKeepsAReadyCardWithAnyRecordOfWork(t *testing.T) {
	t.Parallel()
	for field, why := range map[string]string{
		"taken":        "a take is recorded on it",
		"first_taken":  "a take is recorded on it",
		FieldProgress:  "progress is recorded on it",
		FieldReported:  "a report is recorded on it",
		FieldCarryHead: "it carries a pushed head",
	} {
		w := workingWorld(t)
		w.s.Fleet.Card("s1-4.w1").Fields[field] = stamp(w.s.Now)
		p := FriendTake(w.s, FriendTakeReq{Friend: "amy", All: true, Started: map[string]string{"s1-5.w1": "a push on its branch sprint/s1-5.w1.g1.e0 at abc"}})
		assert.Empty(t, p.Units, "%s: nothing is dealt and untouched", field)
		assert.Contains(t, strings.Join(p.Said, "\n"), "friend amy keeps s1-4.w1: "+why, field)
		assert.Contains(t, strings.Join(p.Said, "\n"), "friend amy keeps s1-5.w1: a push on its branch", field)
	}
}

func TestAWorkingCardNamedWithoutWorkingIsRefused(t *testing.T) {
	t.Parallel()
	w := workingWorld(t)
	p := FriendTake(w.s, FriendTakeReq{Friend: "amy", IDs: []string{"s1-2"}, Who: "rowan"})
	require.Len(t, p.Refused, 1)
	assert.Equal(t, "s1-2", p.Refused[0].Key)
	assert.Equal(t, "s1-2.w1 is in flight with friend amy: it is working on the friend's row; withdrawing it mid-flight wants --working", p.Refused[0].Why)
	assert.Empty(t, p.Units)

	// a ready card named, not taken, needs no --working
	p = w.must(FriendTake(w.s, FriendTakeReq{Friend: "amy", IDs: []string{"s1-4"}, Who: "rowan"}))
	assert.Equal(t, []string{"s1-4.w1"}, movedCards(p))
	assert.NotContains(t, strings.Join(p.Said, "\n"), "WARNING")
}

func TestAWorkingCardNamedWithWorkingMovesAndWarnsOnce(t *testing.T) {
	t.Parallel()
	w := workingWorld(t)
	p := w.must(FriendTake(w.s, FriendTakeReq{Friend: "amy", IDs: []string{"s1-2", "s1-3"}, Working: true, Reason: "reassign", Who: "rowan"}))
	assert.ElementsMatch(t, []string{"s1-2.w1", "s1-3.w1"}, movedCards(p))
	assert.Equal(t, Withdrawn, w.s.Fleet.Card("s1-2.w1").Col)
	assert.Equal(t, Withdrawn, w.s.Fleet.Card("s1-3.w1").Col)
	var warnings []string
	for _, s := range p.Said {
		if strings.HasPrefix(s, "WARNING") {
			warnings = append(warnings, s)
		}
	}
	assert.Equal(t, []string{"WARNING: withdraws 2 working card(s) from friend amy mid-flight: s1-2.w1,s1-3.w1"}, warnings, "one warning line, counting them")
	assert.Equal(t, Working, w.s.Fleet.Card("s1-4.w1").Col, "her freed lanes take her ready cards")
	assert.Equal(t, Working, w.s.Fleet.Card("s1-5.w1").Col)
	assert.Empty(t, Check(w.s, nil))
}
