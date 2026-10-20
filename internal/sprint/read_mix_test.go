package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The read mix (read_mix.go; tla/ReadMix.tla). These tests name the property and the
// judgment by their words, not their constants, so they compile at the base and fail there
// on behaviour.

// readMixWorld is read cards on, members m1..m4 up with reader rows reading every tier, three
// friends up who read pro, and the pro routes of three models where model A has two routes, so
// a draw that does not avoid a used model lands on A twice (the rotation is ra1, ra2, rb, rc).
func readMixWorld(t *testing.T, mix string) (*world, []FriendSeat) {
	t.Helper()
	w := readCardsWorld(t, 4, "m1", "m2", "m3", "m4")
	readersReadEveryTier(w)
	w.s.Routes = []Route{
		{Name: "ra1", Tier: "pro", Provider: "p", Model: "A", Enabled: true},
		{Name: "ra2", Tier: "pro", Provider: "q", Model: "A", Enabled: true},
		{Name: "rb", Tier: "pro", Provider: "p", Model: "B", Enabled: true},
		{Name: "rc", Tier: "pro", Provider: "p", Model: "C", Enabled: true},
	}
	if mix != "" {
		w.s.Work.SetProp("read_mix", mix)
	}
	seats := workerSeatFixtures([]FriendSeat{
		readerSeat("alex", 4, []string{"pro"}, []string{"reader"}),
		readerSeat("pat", 4, []string{"pro"}, []string{"reader"}),
		readerSeat("kim", 4, []string{"pro"}, []string{"reader"}),
	})
	putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\n", "zoe", 1)
	return w, seats
}

// mixSplit is the primary's placed read cards: the friends' rows and the fleet models.
func mixSplit(w *world, primary string) (friends []string, models []string) {
	for _, c := range readCardsOf(w, primary) {
		if _, ok := FriendOfRow(c.Row); ok {
			friends = append(friends, c.Row)
			continue
		}
		_, m, _ := strings.Cut(c.F(FieldModel), "/")
		models = append(models, m)
	}
	return friends, models
}

func distinct(xs []string) bool {
	seen := map[string]bool{}
	for _, x := range xs {
		if seen[x] {
			return false
		}
		seen[x] = true
	}
	return true
}

// TestReadMixDealsFriendsAndFleetOnDistinctModelsAtOnce: with the mix friends=2,fleet=3 a pro
// card in review is dealt all five reads in one deal, two to distinct friends and three to
// fleet members whose routes are three different models.
func TestReadMixDealsFriendsAndFleetOnDistinctModelsAtOnce(t *testing.T) {
	t.Parallel()
	w, seats := readMixWorld(t, "friends=2,fleet=3")
	dealReads(t, w, seats)
	friends, models := mixSplit(w, "s1-1")
	require.Len(t, friends, 2, "two friends' reads: %v", friends)
	require.True(t, distinct(friends))
	require.Len(t, models, 3, "three fleet reads: %v", models)
	require.True(t, distinct(models), "three different models: %v", models)
	require.Equal(t, 5, ReadsNeededIn(w.s, w.s.Work.Card("s1-1")), "the mix's total is the count it needs")

	// dealt again, nothing more: the five stand
	dealReads(t, w, seats)
	require.Len(t, readCardsOf(w, "s1-1"), 5)
}

// TestReadMixFillsFromWhatIsUpAndSaysSo: with one friend up the deal does not wait for a
// second; it fills from the fleet at once, and one judgment on the primary says the reads
// were dealt short of the mix.
func TestReadMixFillsFromWhatIsUpAndSaysSo(t *testing.T) {
	t.Parallel()
	w, seats := readMixWorld(t, "friends=2,fleet=2")
	seats = seats[:1]
	dealReads(t, w, seats)
	friends, models := mixSplit(w, "s1-1")
	require.Len(t, friends, 1, "the one friend up")
	require.Len(t, models, 3, "the rest filled from the fleet, not waited for")
	askReaders(t, w, seats)
	short := w.notesOf("reads dealt short of the read mix")
	require.Len(t, short, 1, "one judgment says the mix was short")
	require.Contains(t, short[0].Primaries, "s1-1")
	askReaders(t, w, seats)
	require.Len(t, w.notesOf("reads dealt short of the read mix"), 1, "once, never every tick")
}

// TestReadMixNoVerdictReasksTheSameClassOnAnotherModel: a fleet read handed back with no
// verdict is dealt again to the fleet (a friend is free and is not taken), on a model neither
// the read handed back nor the fleet reads standing used.
func TestReadMixNoVerdictReasksTheSameClassOnAnotherModel(t *testing.T) {
	t.Parallel()
	w, seats := readMixWorld(t, "friends=1,fleet=2")
	dealReads(t, w, seats)
	friends, models := mixSplit(w, "s1-1")
	require.Len(t, friends, 1)
	require.Len(t, models, 2)
	require.True(t, distinct(models))
	var back *Card
	for _, c := range readCardsOf(w, "s1-1") {
		if _, ok := FriendOfRow(c.Row); !ok {
			back = c
			break
		}
	}
	gone := back.F(FieldModel)
	back.Fields["retired_by"] = RetiredByReturned
	back.Fields["reason"] = "no verdict (ran=false verdict=\"\"): environment failure"
	w.place(w.s.Fleet, back.ID, "", "")

	dealReads(t, w, seats)
	friends, models = mixSplit(w, "s1-1")
	require.Len(t, friends, 1, "the read goes to its class: no second friend")
	require.Len(t, models, 2, "a fleet read again")
	_, goneModel, _ := strings.Cut(gone, "/")
	require.NotContains(t, models, goneModel, "never the model the read returned on while another serves")
	require.True(t, distinct(models), "nor a model a standing fleet read uses: %v", models)
}

// TestReadMixAcceptsOnlyWhenEveryReadIsOk: a card under a mix of five is not acceptable on
// four ok reads, and is on five; a broken read holds it whatever stands.
func TestReadMixAcceptsOnlyWhenEveryReadIsOk(t *testing.T) {
	t.Parallel()
	w, seats := readMixWorld(t, "friends=2,fleet=3")
	dealReads(t, w, seats)
	cards := readCardsOf(w, "s1-1")
	require.Len(t, cards, 5)
	pr := w.s.Work.Card("s1-1")
	verdict := func(c *Card, v string) {
		c.Fields["verdict"], c.Fields["retired_by"] = v, "read"
		w.place(w.s.Fleet, c.ID, "", "")
	}
	for _, c := range cards[:4] {
		verdict(c, "ok")
	}
	require.False(t, acceptable(w.s, pr), "four ok of five is not enough")
	verdict(cards[4], "ok")
	require.True(t, acceptable(w.s, pr), "every read ok")
	cards[4].Fields["verdict"] = "broken"
	require.False(t, acceptable(w.s, pr), "a broken read holds it")
}

// TestReadsSettingTakesFive: set --reads 5 is a count every card in review needs, and the
// deal asks all five at once (no mix: any reader up).
func TestReadsSettingTakesFive(t *testing.T) {
	t.Parallel()
	w, seats := readMixWorld(t, "")
	w.s.Work.SetProp(PropReadsNeeded, "5")
	require.Equal(t, 5, ReadsNeededIn(w.s, w.s.Work.Card("s1-1")))
	dealReads(t, w, seats)
	require.Len(t, readCardsOf(w, "s1-1"), 5, "all five dealt at once")
}

// TestAskAnotherDealsOneMoreReadCard: with read cards on, ask --another deals one more read
// card on the fleet table at once, never a readers-table read the tick would retire as
// "read cards" (the coordinator, 2026-10-20).
func TestAskAnotherDealsOneMoreReadCard(t *testing.T) {
	t.Parallel()
	w, seats := readMixWorld(t, "")
	dealReads(t, w, seats)
	require.Len(t, readCardsOf(w, "s1-1"), 2, "a pro card's two reads")
	w.s.Friends = seats
	w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Another: true}))
	require.Len(t, readCardsOf(w, "s1-1"), 3, "one more read card")
	for _, c := range w.s.Readers.Cards() {
		require.False(t, c.Placed(), "the readers table is asked nothing")
	}
	// the tick neither takes it back nor deals more: three stand
	dealReads(t, w, seats)
	require.Len(t, readCardsOf(w, "s1-1"), 3)
}

// TestReadMixNeedIsTheSprintsReadersNotWhoIsUp: under the mix a card needs as many reads as
// the sprint has readers for it, up or not (never more than the mix), so a sprint of two
// readers is not held for five, and a reader down never lowers the count (a card accepted is
// never sent back when it comes up again; tla/ReadMix.tla NeedNeverRises).
func TestReadMixNeedIsTheSprintsReadersNotWhoIsUp(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2")
	readersReadEveryTier(w)
	w.s.Work.SetProp("read_mix", "friends=2,fleet=3")
	putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\n", "zoe", 1)
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, 2, ReadsNeededIn(w.s, pr), "two readers: two reads, not five")
	w.s.Friends = workerSeatFixtures([]FriendSeat{
		readerSeat("alex", 4, []string{"pro"}, []string{"reader"}),
		readerSeat("pat", 4, []string{"pro"}, []string{"reader"}),
	})
	w.s.Friends[1].Status = Down
	require.Equal(t, 4, ReadsNeededIn(w.s, pr), "a friend down still counts: the need never moves with who is up")
}

// TestReadMixQueuesOnABusyFriendBeforeTheFleet: a friend at her width is still dealt the
// friends' read, queued on her row, rather than the read going to the fleet.
func TestReadMixQueuesOnABusyFriendBeforeTheFleet(t *testing.T) {
	t.Parallel()
	w, seats := readMixWorld(t, "friends=1,fleet=1")
	seats = []FriendSeat{readerSeat("alex", 1, []string{"pro"}, []string{"reader"})}
	w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), FriendRow("alex")))
	w.s.Fleet.Put(&Card{ID: "busy.w1", Row: FriendRow("alex"), Col: Working, Fields: map[string]string{"kind": "work"}})
	dealReads(t, w, seats)
	friends, models := mixSplit(w, "s1-1")
	require.Equal(t, []string{FriendRow("alex")}, friends, "the busy friend is dealt the friends' read")
	require.Len(t, models, 1)
	for _, c := range readCardsOf(w, "s1-1") {
		if c.Row == FriendRow("alex") {
			require.Equal(t, Ready, c.Col, "queued, not working past her width")
		}
	}
}
