package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The interim rules of 2026-10-06 (the owner, 7:25 PM ET: "fix it now, to work around it";
// docs/SPEC-SPRINT.md, the interim rules until read cards): the ask picks from the free
// readers themselves, a read retired without a verdict spends no reader, a pro card is read
// on flash while no enabled route serves pro, and the fleet's and the friends' work each
// have a switch. Every test is on the in-memory world, no store and no socket.

// spendSecond puts the reader's second identity read card of the primary at the attempt,
// retired with no verdict: the reader's one re-ask under the interim rule (ReadCardForAsk)
// is spent.
func spendSecond(w *world, primary string, attempt int, reader string) {
	pr := w.s.Work.Card(primary)
	w.s.Readers.Put(&Card{ID: ReadCardSecondID(primary, attempt, reader), Score: pr.Score, Rev: 1, Fields: map[string]string{
		"kind": "read", "primary": primary, "stream": pr.Row, "reader": reader, "attempt": itoa(attempt), "head": pr.F("head"),
		"retired": stamp(t0), "retired_by": "returned"}})
}

// Six readers free with room and two reads wanted, the round's name index holding one of
// them: two are picked. The index walked alone gave one.
func TestPickByRoomPicksFromTheFreeReaders(t *testing.T) {
	t.Parallel()
	free := []string{"reader-a", "reader-b", "reader-c", "reader-d", "reader-e", "reader-f"}
	room := map[string]readerRoom{}
	for _, rd := range free {
		room[rd] = readerRoom{width: 2, free: 2}
	}
	r := newRound([]string{"reader-d"}, "")
	got := r.pickByRoom(2, free, room)
	require.Len(t, got, 2, "two reads wanted of six readers free: %v", got)
	assert.Equal(t, "reader-d", got[0], "the index's name wins the tie")
	assert.NotEqual(t, got[0], got[1], "two different readers")
}

// A read retired without a verdict (returned, levelled, held) leaves its reader askable at
// the attempt again, under the second identity; a read closed with a verdict does not.
func TestARetiredReadWithoutAVerdictLeavesTheReaderAskable(t *testing.T) {
	t.Parallel()
	for _, by := range []string{"returned", RetiredByLevel, RetiredByHold} {
		t.Run(by, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			toReview(w, "s1-1")
			rc := putRead(w, "s1-1", 1, "reader-a", "")
			rc.Fields["retired"], rc.Fields["retired_by"] = stamp(t0), by
			id, ok := ReadCardForAsk(w.s, "s1-1", 1, "reader-a")
			assert.True(t, ok, "retired by %s with no verdict: reader-a is asked again", by)
			assert.Equal(t, ReadCardSecondID("s1-1", 1, "reader-a"), id)
			assert.Contains(t, w.s.freeReaders(w.s.Work.Card("s1-1"), 1), "reader-a")
			spendSecond(w, "s1-1", 1, "reader-a")
			_, ok = ReadCardForAsk(w.s, "s1-1", 1, "reader-a")
			assert.False(t, ok, "once more only: the second identity is spent")
		})
	}
	t.Run("a closed verdict", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		toReview(w, "s1-1")
		rc := putRead(w, "s1-1", 1, "reader-a", "")
		rc.Fields["retired"], rc.Fields["retired_by"], rc.Fields["verdict"] = stamp(t0), "read", "ok"
		id, ok := ReadCardForAsk(w.s, "s1-1", 1, "reader-a")
		assert.False(t, ok, "a read with a verdict spends its reader at the attempt")
		assert.Equal(t, ReadCardID("s1-1", 1, "reader-a"), id)
		assert.NotContains(t, w.s.freeReaders(w.s.Work.Card("s1-1"), 1), "reader-a")
	})
}

// A pro card is never read on flash (the owner, 2026-10-16: a card is never read below its
// tier; the interim "let flash read pro" of 2026-10-06 is gone): with no enabled route of
// pro and one of flash, its read tier is pro and no read is drawn on the flash route; with
// a pro route enabled it is read on pro.
func TestAProCardIsNeverReadOnFlashWhenNoProRouteIsEnabled(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Work.Card("s1-1").Fields[FieldTierNow] = cardhdr.RoutePro
	toReview(w, "s1-1") // worked before the routes are read: the work's route is not the subject
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Tokens: 1000, Enabled: true},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "q", Tokens: 1000, Enabled: false},
	}
	readersReadEveryTier(w)
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, cardhdr.RoutePro, w.s.readTierOf(pr), "no pro route enabled: still read at pro")
	w.do(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	for _, rc := range readsAt(w.s, pr, 1) {
		assert.NotEqual(t, "flash-a", rc.F(FieldRoute), "never read on the flash route: "+rc.ID)
		assert.NotEqual(t, cardhdr.RouteFlash, rc.F(FieldTier), rc.ID)
	}
	w.s.Routes[1].Enabled = true
	assert.Equal(t, cardhdr.RoutePro, w.s.readTierOf(pr), "a pro route enabled: read on pro")
}

// With the fleet's work off (nova-sprint set --fleet off) the tick deals no work card to a
// machine, the deal verb refuses, and the fleet's idle alarm is not raised; the readers
// still read. On again, the tick deals.
func TestFleetOffDealsNoWorkToMachines(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	toReview(w, "s1-1")
	w.s.Work.Put(&Card{ID: "s1-9", Row: "s1", Col: Waiting, Score: 9, Rev: 1, Fields: map[string]string{"kind": "primary", "attempt": "0", "stream": "s1", "brief": proBrief}})
	w.must(Set(w.s, SetReq{Fleet: SwitchOff, Who: "coordinator"}))
	require.True(t, w.s.FleetOff())
	dealt := func() int { return w.s.Fleet.Count("m1", Ready) + w.s.Fleet.Count("m2", Ready) }
	before := dealt()

	p, _ := tickDealFixture(w.s, TickReq{})
	w.must(p)
	assert.Equal(t, before, dealt(), "no work card is dealt to a machine")
	assert.Empty(t, w.notesOf(NNoMember))
	assert.Empty(t, w.notesOf(NStarving))
	refused := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-2"}}})
	require.Len(t, refused.Refused, 1)
	assert.Contains(t, refused.Refused[0].Why, "--fleet on")
	p, _ = TickIdle(w.s, TickReq{IdleAlarm: true})
	assert.Empty(t, p.Units, "no idle alarm while the fleet's work is off")

	p, _ = TickAsk(w.s, TickReq{})
	w.must(p)
	assert.NotEmpty(t, readsAt(w.s, w.s.Work.Card("s1-1"), 1), "reads flow")

	w.must(Set(w.s, SetReq{Fleet: SwitchOn, Who: "coordinator"}))
	require.False(t, w.s.FleetOff())
	p, _ = TickIdle(w.s, TickReq{IdleAlarm: true})
	assert.NotEmpty(t, p.Units, "on: the idle episode begins")
	p, _ = tickDealFixture(w.s, TickReq{})
	w.must(p)
	assert.Greater(t, dealt(), before, "on: the tick deals")
}

// With the friends' work off (nova-sprint set --friends off) the tick deals no work card to
// a friend, her reads are still placed on her row, and her empty row is no alarm. On again,
// she is dealt work in the room her reads leave.
func TestFriendsOffDealsNoWorkToFriends(t *testing.T) {
	t.Parallel()
	w, amy := priorityWorld(t, 1, 1, 0, 10)
	w.must(Set(w.s, SetReq{Friends: SwitchOff, Who: "coordinator"}))
	require.True(t, w.s.FriendsOff())
	tickDealAndAsk(t, w, amy)
	assert.Equal(t, []string{"s1-1"}, friendReads(w, "amy"), "her read flows")
	assert.Empty(t, friendNewWork(w, "amy"), "no work card is dealt to a friend")
	var empty Plan
	assert.Empty(t, emptyConds(&empty, w.s, TickReq{Friends: workerSeatFixtures([]FriendSeat{amy})}), "no empty-row alarm while the friends' work is off")

	w.must(Set(w.s, SetReq{Friends: SwitchOn, Who: "coordinator"}))
	tickDealAndAsk(t, w, amy)
	assert.Len(t, friendNewWork(w, "amy"), 2, "on: her room left is dealt")
}

// The switches take on and off, are the sprint's alone, and say off on the set line.
func TestTheWorkSwitchesAreTheSprints(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	p := Set(w.s, SetReq{Fleet: "maybe", Who: "coordinator"})
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "--fleet wants on or off")
	p = Set(w.s, SetReq{Streams: []string{"s1"}, Friends: SwitchOff, Who: "coordinator"})
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "--friends is the sprint's")
	p = w.must(Set(w.s, SetReq{Fleet: SwitchOff, Friends: SwitchOff, Who: "coordinator"}))
	assert.Contains(t, p.Units[0].Moved, "fleet off")
	assert.Contains(t, p.Units[0].Moved, "friends off")
	assert.Equal(t, SwitchOff, SwitchWord(map[string]string{PropFleet: SwitchOff}, PropFleet))
	assert.Equal(t, SwitchOn, SwitchWord(nil, PropFriends), "on by default")
}

// A heavy card is never read on pro (the owner, 2026-10-16: a card is never read below its
// tier; the interim "let pro do it" of 2026-10-06 is gone): the heavy friend is asked its
// read at heavy, and the fleet reader of pro, with only flash and pro routes, is asked
// nothing; its second read waits for a reader of heavy or above.
func TestAHeavyCardIsNeverReadOnPro(t *testing.T) {
	t.Parallel()
	const head = "0123456789abcdef0123456789abcdef01234567"
	w := newWorld(t, "reader-m1", "reader-m2")
	w.readersTable() // explicit legacy reader-table fixture
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))
	johnny := workerSeatFixture(FriendSeat{Name: "johnny", Width: 2, Status: Up, Tiers: []string{cardhdr.RouteHeavy}})
	w.s.Friends = workerSeatFixtures([]FriendSeat{johnny})
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Tokens: 1000, Enabled: true},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "q", Tokens: 1000, Enabled: true},
	}
	w.s.Readers.Texts = map[string]map[string]string{"reader-m1": {ReaderTiers: "flash,pro"}, "reader-m2": {ReaderTiers: "flash"}}
	w.s.ReaderStates = map[string]string{"reader-m1": ReaderUp, "reader-m2": ReaderUp}
	putReview(w, "s1-1", "s1-1: heavy work tier: heavy\n\nThe task.", 1, 1, head)
	w.s.Work.Card("s1-1").Fields[FieldTierNow] = cardhdr.RouteHeavy
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, cardhdr.RouteHeavy, w.s.readTierOf(pr), "a heavy card is read at heavy")
	require.Equal(t, 2, ReadsNeeded(pr), "a heavy card needs two readers")

	tickDealAndAsk(t, w, johnny)
	require.NotNil(t, w.s.Fleet.Placed(ReadCardID("s1-1", 1, "johnny")), "the heavy friend is asked")
	for _, rc := range readsAt(w.s, pr, 1) {
		assert.NotEqual(t, "pro-a", rc.F(FieldRoute), "never on the pro route: "+rc.ID)
	}
	assert.Empty(t, readsAt(w.s, pr, 1), "no fleet reader of pro is asked a heavy read")
}

// A card's reads are asked together (Glenn, 2026-10-06 6:02 PM ET: "send out multiple
// consumer cards in ||"): a pro primary with two free readers is asked of both in one ask; a
// read outstanding counts toward the two it needs; once a read finds it broken no more is asked.
func TestACardsReadsAreAskedTogether(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	toReview(w, "s1-1", "s1-2")
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, 2, ReadsWanted(w.s, pr), "both reads wanted at once")
	w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	reads := readsAt(w.s, pr, 1)
	require.Len(t, reads, 2, "two reads asked in one ask")
	assert.NotEqual(t, reads[0].F("reader"), reads[1].F("reader"), "of two different readers")
	assert.Equal(t, 0, ReadsWanted(w.s, pr), "outstanding reads count toward the two it needs")

	other := w.s.Work.Card("s1-2")
	rc := putRead(w, "s1-2", 1, "reader-a", Broken)
	rc.Fields["verdict"] = "broken"
	assert.Equal(t, 0, ReadsWanted(w.s, other), "a broken read stops the rest")
}
