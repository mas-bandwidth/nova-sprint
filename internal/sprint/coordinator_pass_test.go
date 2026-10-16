package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The coordinator's pass (docs/SPEC-SPRINT.md section 8, "The coordinator's pass";
// tla/CoordinatorPass.tla), on the twin store with a fake clock: a deaf friend, an idle
// friend and a judgment late on the coordinator, each raised once, raised again with a
// push every ten minutes of running time while it holds, and closed when it stops.

// passRig is the hold rig with each friend's session pong kept on her beat. Each
// friend's session last answered five minutes before the rig starts, so it is deaf
// (fifteen minutes) ten minutes and more into the rig.
type passRig struct {
	*holdRig
	pongs map[string]time.Time
	// answered is each friend's last wake ping answer the rig recorded (friend health):
	// the store renews only on a newer one.
	answered map[string]time.Time
	// proved is each friend's last session proof the rig put on her beat.
	proved map[string]time.Time
}

// passPongBefore is how long before the rig starts each friend's session last answered.
const passPongBefore = 5 * time.Minute

func newPassRig(t *testing.T) *passRig {
	t.Helper()
	r := &passRig{holdRig: newHoldRig(t, 1, 1), pongs: map[string]time.Time{"amy": holdT0.Add(-passPongBefore), "bob": holdT0.Add(-passPongBefore)}, answered: map[string]time.Time{}, proved: map[string]time.Time{}}
	rows, err := r.st.FriendRows(r.ctx, r.clock())
	require.NoError(t, err)
	_ = rows
	r.tick(0)
	r.startFriends()
	return r
}

// startFriends is each friend's start of every card dealt ready on her row: her own take
// (take --as friend.<name>), as her daemon starts a card, so it is working only once she
// starts it (docs/SPEC-SPRINT.md section 1, a friend's card is working once she starts it).
func (r *holdRig) startFriends() {
	r.t.Helper()
	for _, row := range r.snap().Fleet.Rows() {
		if _, ok := sprint.FriendOfRow(row); ok {
			name, _ := sprint.FriendOfRow(row)
			req := sprint.FriendStartReq{Friend: name, Gens: map[string]int{}}
			for _, c := range r.snap().Fleet.Cell(row, sprint.Ready) {
				req.IDs, req.Gens[c.ID] = append(req.IDs, c.ID), max(c.Int("gen"), 1)
			}
			if len(req.IDs) > 0 {
				r.must(store.FriendStartStep(req)) // her start receipt, as friend sync reads it
			}
		}
	}
}

// tick moves the clock by d, beats everyone (each friend with her pong, and her session
// active, as on a long turn: the pass alone judges her), records each pong as a wake ping
// her session answered (her beat is no evidence: she is up only on that answer) and runs
// one tick.
func (r *passRig) tick(d time.Duration) {
	r.t.Helper()
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
	require.NoError(r.t, r.st.BeatReaders(r.ctx))
	zero := 0.0
	for _, m := range []string{"m1", "m2"} {
		_, err := r.st.Beat(r.ctx, m, &zero, hostload.Source{})
		require.NoError(r.t, err)
	}
	for f := range r.pongs {

		_, err := r.st.FriendBeatReport(r.ctx, f, sprint.FriendReport{Active: r.clock()}, nil)
		require.NoError(r.t, err)
		require.NoError(r.t, setFriendHook(r.st, f, sprint.Up))
	}
	_, err := r.st.Tick(r.ctx)
	require.NoError(r.t, err)
}

// clock is the rig's clock now.
func (r *passRig) clock() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.now
}

// notes is every note the sprint wrote.
func (r *passRig) notes() []sprint.Note {
	r.t.Helper()
	notes, _, err := r.st.B.(*store.Mem).NotesSince(r.ctx, "", 100000)
	require.NoError(r.t, err)
	return notes
}

// count is how many notes of the kind and type the sprint wrote whose what says the word.
func (r *passRig) count(kind, typ, word string) int {
	n := 0
	for _, x := range r.notes() {
		if x.Kind == kind && x.Type == typ && strings.Contains(x.What, word) {
			n++
		}
	}
	return n
}

// open is the open judgment of the type on the subject, nil when none is.
func (r *passRig) open(typ, subject string) *sprint.Note {
	for _, o := range r.snap().Open {
		if o.Note.Type == typ && o.Subject() == subject {
			n := o.Note
			return &n
		}
	}
	return nil
}

// tickEnds is how many tick-end notes (the coordinator's wake: inbox --wait) were written.
func (r *passRig) tickEnds() int { return r.count(sprint.Happened, sprint.NTickEnd, "") }

func TestThePassSparesAHeldFriendAnAcknowledgedJudgmentAndKeepsTheFinishSetting(t *testing.T) {
	t.Parallel()
	r := newPassRig(t)
	s := r.snap()
	fc := s.Fleet.Card(s.Work.Card("f1-1").F("work"))
	require.NotNil(t, fc)
	holder, _ := sprint.FriendOfRow(fc.Row)
	other := map[string]string{"amy": "bob", "bob": "amy"}[holder]
	r.hold(sprint.HoldReq{Names: []string{other}})
	set := store.SetStep(sprint.SetReq{Who: "coordinator"})
	plan := set.Plan
	set.Plan = func(s *sprint.Snapshot) sprint.Plan { return sprint.WithFriendFinish(plan(s), s, "45m") }
	r.must(set)

	r.tick(30*time.Minute + 30*time.Second)
	assert.Nil(t, r.open(sprint.NFriendIdle, fc.Row), "30m30s is inside the 45m work window")
	assert.Nil(t, r.open(sprint.NFriendIdle, sprint.FriendRow(other)), "held friend is not judged")
	r.tick(15 * time.Minute)
	assert.NotNil(t, r.open(sprint.NFriendIdle, fc.Row), "45m30s with no finish is idle")
}

// A quiet friend whose session answers is never judged deaf: her daemon asks the session
// after ten minutes with no word and waits five for the answer, so her proof is at most
// fifteen minutes old. Past that it has lapsed, and the coordinator is told once, as a
// judgment naming her and the remedy, never only in her daemon's log.
func TestAnIdleUpFriendWhileCardsWaitElsewhereIsToldOnce(t *testing.T) {
	t.Parallel()
	r := newPassRig(t)
	s := r.snap()
	fc := s.Fleet.Card(s.Work.Card("f1-1").F("work"))
	require.NotNil(t, fc, "the friend's card is dealt")
	require.Equal(t, sprint.Working, fc.Col)
	holder, ok := sprint.FriendOfRow(fc.Row)
	require.True(t, ok)
	idle := map[string]string{"amy": "bob", "bob": "amy"}[holder]
	require.Zero(t, r.snap().Fleet.Count(sprint.FriendRow(idle), sprint.Ready)+r.snap().Fleet.Count(sprint.FriendRow(idle), sprint.Working), "her row is empty")
	row := sprint.FriendRow(idle)
	fresh := func() { r.pongs[holder], r.pongs[idle] = r.clock(), r.clock() }

	assert.Nil(t, r.open(sprint.NFriendEmpty, row), "the empty row has just been seen: the ten minutes start now")
	assert.Equal(t, 0, r.count(sprint.Judgment, sprint.NFriendEmpty, ""))

	fresh()
	r.tick(9*time.Minute + 59*time.Second)
	assert.Nil(t, r.open(sprint.NFriendEmpty, row), "nine minutes and fifty-nine seconds is not ten")
	assert.Equal(t, 0, r.count(sprint.Judgment, sprint.NFriendEmpty, ""))

	fresh()
	r.tick(time.Second)
	j := r.open(sprint.NFriendEmpty, row)
	require.NotNil(t, j, "an up friend empty for ten minutes while a card she could do waits is told")
	assert.Equal(t, sprint.Judgment, j.Kind)
	assert.Contains(t, j.What, "friend "+idle)
	assert.Contains(t, j.What, sprint.EmptyRowAfter.String())
	assert.Contains(t, j.What, fc.ID)
	assert.Contains(t, j.What, fc.Row+":"+fc.Col)
	assert.Contains(t, j.What, "unstarted")
	assert.Contains(t, j.What, "deal them to that friend")
	assert.Contains(t, j.What, "friend take the other row")
	assert.Contains(t, j.What, "keep")
	assert.Contains(t, j.Decisions, "deal them to "+idle)
	assert.Contains(t, j.Decisions, "friend take "+holder+" --all-unstarted")
	assert.Contains(t, j.Decisions, "keep")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NFriendEmpty, "friend "+idle), "told once")

	fresh()
	r.tick(9*time.Minute + 59*time.Second)
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NFriendEmpty, ""), "still the one judgment")
	assert.Equal(t, 0, r.count(sprint.Happened, sprint.NRaisedAgain, sprint.NFriendEmpty), "not again within ten minutes")

	ends := r.tickEnds()
	fresh()
	r.tick(time.Second)
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NFriendEmpty, "friend "+idle), "raised again in place")
	assert.Equal(t, 1, r.count(sprint.Happened, sprint.NRaisedAgain, sprint.NFriendEmpty))
	again := r.open(sprint.NFriendEmpty, row)
	require.NotNil(t, again)
	assert.Equal(t, 1, again.Before, "the judgment counts its raises again")
	assert.Greater(t, r.tickEnds(), ends, "the push wakes inbox --wait")

	s = r.snap()
	fc = s.Fleet.Card(fc.ID)
	require.NotNil(t, fc)
	r.must(store.FinishStep(sprint.FinishReq{As: fc.Row, Sel: sprint.Sel{IDs: []string{fc.ID}}, Gens: map[string]int{fc.ID: fc.Int("gen")}, Head: "abc", Who: fc.Row}))
	fresh()
	r.tick(time.Second)
	assert.Nil(t, r.open(sprint.NFriendEmpty, row), "nothing she could do is waiting: the judgment closes")
	pushes := r.count(sprint.Happened, sprint.NRaisedAgain, sprint.NFriendEmpty)
	fresh()
	r.tick(10 * time.Minute)
	assert.Equal(t, pushes, r.count(sprint.Happened, sprint.NRaisedAgain, sprint.NFriendEmpty), "a closed episode is not pushed")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NFriendEmpty, ""))
}

// An empty-row episode the coordinator acknowledged stays one while the
// conjunction holds: another card she could do changes the text a fresh
// judgment would carry, but the acknowledged judgment's own text is the
// episode's, so the pass writes no second judgment. The acknowledgement closes
// when her row is not empty, and a new episode is judged past EmptyRowAfter
// again (tla/CoordinatorPass.tla, OneJudgmentAnEpisode, Kind = "empty").
func TestAnAckedEmptyRowIsOneEpisode(t *testing.T) {
	t.Parallel()
	r := newPassRig(t)
	s := r.snap()
	fc := s.Fleet.Card(s.Work.Card("f1-1").F("work"))
	require.NotNil(t, fc)
	holder, ok := sprint.FriendOfRow(fc.Row)
	require.True(t, ok)
	idle := map[string]string{"amy": "bob", "bob": "amy"}[holder]
	row := sprint.FriendRow(idle)
	empty := func() int {
		return r.snap().Fleet.Count(row, sprint.Ready) + r.snap().Fleet.Count(row, sprint.Working)
	}
	require.Zero(t, empty(), "her row is empty")
	fresh := func() { r.pongs[holder], r.pongs[idle] = r.clock(), r.clock() }

	fresh()
	r.tick(sprint.EmptyRowAfter + time.Second)
	j := r.open(sprint.NFriendEmpty, row)
	require.NotNil(t, j, "past EmptyRowAfter while a card she could do waits elsewhere")
	require.Equal(t, 1, r.count(sprint.Judgment, sprint.NFriendEmpty, "friend "+idle), "told once")

	// the coordinator acknowledges it; a second card she could do joins the wait
	r.must(store.AckStep(sprint.AckReq{Notes: []string{j.ID}, Reason: "her steps are run", Who: "coordinator"}))
	r.must(store.AddStep(sprint.AddReq{Stream: "f1", Cards: []sprint.CardAdd{{ID: "f1-2", Brief: friendsBrief("friend " + holder)}}}))
	fresh()
	r.tick(time.Second)
	assert.Nil(t, r.open(sprint.NFriendEmpty, row), "an acknowledged empty row is not written again while it holds")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NFriendEmpty, "friend "+idle), "one episode, one judgment")

	// a card dealt to her fills the row: the conjunction clears and the ack closes
	r.must(store.AddStep(sprint.AddReq{Stream: "f1", Cards: []sprint.CardAdd{{ID: "f1-3", Brief: friendsBrief("friend " + idle)}}}))
	fresh()
	r.tick(time.Second)
	require.NotZero(t, empty(), "a card is dealt to her")
	assert.Nil(t, r.open(sprint.NFriendEmpty, row), "her row is not empty: the judgment stays closed")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NFriendEmpty, "friend "+idle))

	// the card is finished and her row is empty again: a new episode is judged
	r.startFriends()
	s = r.snap()
	wc := s.Fleet.Card(s.Work.Card("f1-3").F("work"))
	require.NotNil(t, wc)
	r.must(store.FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Head: "abc", Who: wc.Row}))
	require.Zero(t, empty(), "her row is empty again")
	fresh()
	r.tick(time.Second)
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NFriendEmpty, "friend "+idle), "the clock starts afresh")
	fresh()
	r.tick(sprint.EmptyRowAfter)
	assert.Equal(t, 2, r.count(sprint.Judgment, sprint.NFriendEmpty, "friend "+idle), "a new episode past EmptyRowAfter")
	assert.NotNil(t, r.open(sprint.NFriendEmpty, row), "the new judgment is open")
}

// A named pin the deal places on another friend's row is a judgment in that
// step: why the pinned friend did not take it, and whose row holds the card.
// The pass keeps that one note, raises it again in place, and closes it when
// the card is finished. A hard pin is not rotated and is not this judgment.
func TestAPinnedCardRotatedOffItsFriendIsJudgedOnce(t *testing.T) {
	t.Parallel()
	r := newPassRig(t)
	r.hold(sprint.HoldReq{Names: []string{"amy"}, Reason: "she is away"})
	r.must(store.AddStep(sprint.AddReq{Stream: "f1", Cards: []sprint.CardAdd{{ID: "f1-2", Brief: friendsBrief("friend amy")}}}))
	r.pongs["bob"] = r.clock()
	r.tick(time.Second)
	s := r.snap()
	wc := s.Fleet.Card(s.Work.Card("f1-2").F("work"))
	require.NotNil(t, wc, "the pin was dealt")
	require.Equal(t, sprint.FriendRow("bob"), wc.Row, "amy is held, so the pin rotates to bob")
	j := r.open(sprint.NPinIgnored, "f1-2")
	require.NotNil(t, j, "a pin placed on someone else's row is a judgment")
	assert.Contains(t, j.What, wc.ID)
	assert.Contains(t, j.What, "pinned to amy")
	assert.Contains(t, j.What, "friend is held")
	assert.Contains(t, j.What, wc.Row+":"+wc.Col)
	assert.Contains(t, j.Decisions, "friend take bob "+wc.ID+" --reason pinned to amy")
	assert.Contains(t, j.Decisions, "keep")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NPinIgnored, "f1-2"))

	r.must(store.AddStep(sprint.AddReq{Stream: "f1", Cards: []sprint.CardAdd{{ID: "f1-3", Brief: friendsBrief("only friend amy")}}}))
	r.pongs["bob"] = r.clock()
	r.tick(time.Second)
	assert.Equal(t, sprint.Ready, r.snap().Work.Card("f1-3").Col, "a hard pin waits for her")
	assert.Nil(t, r.open(sprint.NPinIgnored, "f1-3"), "a hard pin is not rotated, and is not this judgment")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NPinIgnored, ""), "the rotated pin is still the one judgment")

	// the hard-pin tick above already took one second of the ten minutes
	r.pongs["bob"] = r.clock()
	r.tick(9*time.Minute + 58*time.Second)
	assert.Equal(t, 0, r.count(sprint.Happened, sprint.NRaisedAgain, sprint.NPinIgnored))
	r.pongs["bob"] = r.clock()
	r.tick(time.Second)
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NPinIgnored, "f1-2"), "raised again in place")
	assert.Equal(t, 1, r.count(sprint.Happened, sprint.NRaisedAgain, sprint.NPinIgnored))
	again := r.open(sprint.NPinIgnored, "f1-2")
	require.NotNil(t, again)
	assert.Equal(t, 1, again.Before)

	r.startFriends()
	s = r.snap()
	wc = s.Fleet.Card(wc.ID)
	require.NotNil(t, wc)
	r.must(store.FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Head: "abc", Who: wc.Row}))
	r.pongs["bob"] = r.clock()
	r.tick(time.Second)
	assert.Nil(t, r.open(sprint.NPinIgnored, "f1-2"), "the card is finished: the judgment closes")
}

// A named pin the friends do not take, so the fleet deals it, is still a
// judgment. The deal writes no friend unit for it; the pass raises the one
// note from the row the card landed on.
func TestAPinnedCardTheFleetTookIsJudgedOnce(t *testing.T) {
	t.Parallel()
	r := newPassRig(t)
	r.hold(sprint.HoldReq{Names: []string{"amy", "bob"}, Reason: "both away"})
	r.must(store.AddStep(sprint.AddReq{Stream: "f1", Cards: []sprint.CardAdd{{ID: "f1-9", Brief: friendsBrief("friend amy")}}}))
	r.tick(time.Second)
	s := r.snap()
	wc := s.Fleet.Card(s.Work.Card("f1-9").F("work"))
	require.NotNil(t, wc, "no friend is up, so the fleet deals the pin")
	require.False(t, sprint.IsFriendRow(wc.Row), "it sits on a machine, %s", wc.Row)
	j := r.open(sprint.NPinIgnored, "f1-9")
	require.NotNil(t, j, "the pass tells the coordinator the pin was dealt away")
	assert.Contains(t, j.What, wc.ID)
	assert.Contains(t, j.What, "pinned to amy")
	assert.Contains(t, j.What, "friend is held")
	assert.Contains(t, j.What, wc.Row+":"+wc.Col)
	assert.NotContains(t, j.Decisions, "friend take")
	assert.Contains(t, j.Decisions, "keep")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NPinIgnored, "f1-9"))
	r.tick(time.Second)
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NPinIgnored, "f1-9"), "the pass keeps the one note")
}

func TestWorkIdleEpisodeRepeatsAndClosesOnFinish(t *testing.T) {
	r := newPassRig(t)
	s := r.snap()
	fc := s.Fleet.Card(s.Work.Card("f1-1").F("work"))
	require.NotNil(t, fc)
	r.tick(31 * time.Minute)
	idle := r.open(sprint.NFriendIdle, fc.Row)
	require.NotNil(t, idle)
	r.tick(10 * time.Minute)
	require.Equal(t, 1, r.count(sprint.Happened, sprint.NRaisedAgain, sprint.NFriendIdle))
	s = r.snap()
	fc = s.Fleet.Card(fc.ID)
	r.must(store.FinishStep(sprint.FinishReq{As: fc.Row, Sel: sprint.Sel{IDs: []string{fc.ID}}, Gens: map[string]int{fc.ID: fc.Int("gen")}, Head: "abc", Who: fc.Row}))
	r.tick(time.Second)
	require.Nil(t, r.open(sprint.NFriendIdle, fc.Row))
}
