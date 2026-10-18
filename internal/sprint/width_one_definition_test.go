package sprint

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One definition of width (the owner, 2026-10-18): "every fleet machine and every friend
// must follow the one true definition of width, in nova-config. No exceptions." A width
// is the most cards a row runs at once: every card in its working column counts one, a
// read card the same as a work card, on a member's row and a friend's alike, and a
// member's width is its nova-config machine row's. tla/WidthHard.tla: WidthHeld,
// OneSource; the witnesses MCWidthHardBrokenHalfRead (a read at half a slot: width 16
// ran 20 on hetzner1), MCWidthHardBrokenFriendNext (a friend's next with no check) and
// MCWidthHardBrokenSecondSource (fleet up writing a width of its own).

// widthClock is the test's clock: 1000 s from the epoch, never the wall's.
var widthClock = time.Unix(1000, 0).UTC()

// oneWidthWorld is one member m1 of the width, up, read cards on, with work work cards and
// reads read cards ready on its row.
func oneWidthWorld(t *testing.T, width, work, reads int) *world {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = widthClock
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: width}))
	require.Equal(t, width, w.s.Width("m1"))
	for i := 1; i <= work; i++ {
		w.s.Fleet.Put(&Card{ID: fmt.Sprintf("w%d.w1", i), Row: "m1", Col: Ready, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "work", "stream": "s1", "attempt": "1", "gen": "1", "member": "m1"}})
	}
	for i := 1; i <= reads; i++ {
		w.s.Fleet.Put(&Card{ID: ReadCardID(fmt.Sprintf("p%d", i), 1, "m1"), Row: "m1", Col: Ready, Score: float64(100 + i), Rev: 1,
			Fields: map[string]string{"kind": "read", "stream": "s1", "attempt": "1", "gen": "1", "reader": "m1", FieldReadCard: "1"}})
	}
	w.s.Fleet.cells, w.s.Fleet.byPrimary = nil, nil
	require.True(t, w.s.ReadCardsOn(), "read cards are on, the default")
	return w
}

// readyOn is the ids of the row's ready cards.
func readyOn(s *Snapshot, row string) []string {
	var ids []string
	for _, c := range s.Fleet.Cell(row, Ready) {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestWorkingNeverPassesWidth(t *testing.T) {
	t.Parallel()
	w := oneWidthWorld(t, 4, 4, 6)
	// a take by count asks for every ready card: the room is the width less the cards
	// working, one a card (at BASE the half-slot room let it take 6 reads and a work card:
	// 7 working of width 4)
	w.do(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: -1}}))
	require.LessOrEqual(t, w.s.Fleet.Count("m1", Working), 4, "a take by count: %d working of width 4", w.s.Fleet.Count("m1", Working))
	// a take by id of every card still ready is refused at the width
	ids := readyOn(w.s, "m1")
	require.NotEmpty(t, ids)
	p := Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: ids}, Gens: gensOf(w.s, ids...)})
	w.do(p)
	require.LessOrEqual(t, w.s.Fleet.Count("m1", Working), 4, "a take by id: %d working of width 4", w.s.Fleet.Count("m1", Working))
	// a lane take, by count and by id
	w.do(LaneTake(w.s, TakeReq{As: "m1", Lane: 1}))
	require.LessOrEqual(t, w.s.Fleet.Count("m1", Working), 4, "a lane take: %d working of width 4", w.s.Fleet.Count("m1", Working))
	ids = readyOn(w.s, "m1")
	w.do(NamedLaneTake(w.s, TakeReq{As: "m1", Lane: 2, Sel: Sel{IDs: ids[:1]}, Gens: gensOf(w.s, ids[0]), LaneByID: true}))
	require.LessOrEqual(t, w.s.Fleet.Count("m1", Working), 4, "a lane take by id: %d working of width 4", w.s.Fleet.Count("m1", Working))
	// and the store's check holds it on any path: a plan moving every ready card into
	// working applies none past the width
	var units []Unit
	for _, c := range w.s.Fleet.Cell("m1", Ready) {
		units = append(units, Unit{Key: c.ID, Changes: []Change{change(Fleet, moveEntry(c, "m1", Working, nil))}, Moved: c.ID + " ready -> working"})
	}
	held := HoldWidths(w.s, Plan{Units: units}, nil)
	w.do(Plan{Units: held.Units})
	assert.Equal(t, 4, w.s.Fleet.Count("m1", Working), "working reaches the width and never passes it")
	assert.Len(t, held.Refused, len(units)-len(held.Units), "one refusal a unit skipped")
}

func TestAReadTakesAWholeSlot(t *testing.T) {
	t.Parallel()
	w := oneWidthWorld(t, 2, 1, 3)
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{"w1.w1"}}, Gens: gensOf(w.s, "w1.w1")}))
	require.Equal(t, 1, w.s.Fleet.Count("m1", Working))
	// one work card working of width 2: one slot is left, and a read takes it whole
	// (at BASE a read took half a slot: two reads were taken, three working of width 2)
	p := w.do(Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: -1}}))
	assert.Len(t, p.Units, 1, "exactly one read is taken: %v", p.Units)
	work, reads := rowWorking(w.s, "m1")
	assert.Equal(t, 1, work)
	assert.Equal(t, 1, reads)
	assert.Equal(t, 2, w.s.Fleet.Count("m1", Working))
	// the read ask counts a member's read whole too, as a friend's
	for _, u := range readUnitsOf(w.s, nil) {
		if u.row == "m1" {
			assert.Equal(t, 0, u.idle, "no free lane: two working of width 2, and the two reads ready")
		}
	}
	assert.Equal(t, 4, slotLoad(rowLoad(w.s, "m1")), "every card on the row is one slot: 1 work and 3 reads")
}

func TestFriendNextHoldsTheWidth(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend"), friendBrief("friend"), friendBrief("friend"), friendBrief("friend"))
	w.s.Now = widthClock
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
	dealStarted(w, amy)
	row := FriendRow("amy")
	require.Equal(t, 2, w.s.Fleet.Count(row, Working), "two working of width 2")
	require.Equal(t, 2, w.s.Fleet.Count(row, Ready), "two ready behind them")
	// friend sync lowers her width to 1 (her nova-config row): her finish frees one lane,
	// and she still holds one working, at her width: no next is promoted (at BASE
	// friendNext promoted one with no check, two working of width 1)
	lowered := amy
	lowered.Width = 1
	w.s.Friends = []FriendSeat{lowered}
	wc := w.s.Fleet.Cell(row, Working)[0]
	p := Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, As: row, Gens: gensOf(w.s, wc.ID), Head: "abc"})
	for _, u := range p.Units {
		assert.NotContains(t, u.Moved, "taken now", "no next is promoted at her width: %s", u.Moved)
	}
	w.must(p)
	assert.Equal(t, 1, w.s.Fleet.Count(row, Working), "one working of width 1")
	// under her width, her next finish promotes one again
	w.s.Friends = []FriendSeat{amy}
	wc = w.s.Fleet.Cell(row, Working)[0]
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, As: row, Gens: gensOf(w.s, wc.ID), Head: "abd"}))
	assert.Equal(t, 1, w.s.Fleet.Count(row, Working), "her finish frees a lane of width 2 and her next takes it")

	// her start receipt is her fact, never cut: her beat names a card running, so it is
	// counted working even at her width (friendStartUnits; a cut would leave a card she
	// runs ready for the level to move)
	w.s.Friends = []FriendSeat{lowered}
	rc := w.s.Fleet.Cell(row, Ready)[0]
	set, unset := friendStartSet(w.s, rc, "amy")
	start := Plan{Units: []Unit{{Key: rc.ID, Changes: []Change{change(Fleet, moveEntry(rc, row, Working, set, unset...))}, Moved: rc.ID + " started"}}}
	held := HoldWidths(w.s, start, nil)
	assert.Empty(t, held.Refused, "a start receipt is not cut")
	assert.Len(t, held.Units, 1)
}

func TestOverWidthDealSkipsToTheNextMember(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = widthClock
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 1}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2", Width: 1}))
	w.must(Add(w.s, AddReq{Brief: proBrief, Stream: "s1", Count: 3}))
	// the first card's member takes it: that member is at its width, one working of 1
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	full := w.s.Fleet.Card(w.s.Work.Card("s1-1").F("work")).Row
	other := map[string]string{"m1": "m2", "m2": "m1"}[full]
	w.must(Take(w.s, TakeReq{As: full, Sel: Sel{Limit: 1}}))
	require.Equal(t, 1, w.s.Fleet.Count(full, Working))
	// the deal finds that member at its width and places the next card on the other:
	// nothing is refused
	p := w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-2"}}}))
	assert.Empty(t, p.Refused)
	require.Equal(t, other, w.s.Fleet.Card(w.s.Work.Card("s1-2").F("work")).Row, "the deal skips the member at its width to the next with a free lane")
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-3"}}}))
	// one plan taking for both: the unit putting a card into the full member's working is
	// skipped alone with one refusal naming it; the other member's applies; nothing else is
	// refused
	a := w.s.Fleet.Card(w.s.Work.Card("s1-3").F("work"))
	b := w.s.Fleet.Card(w.s.Work.Card("s1-2").F("work"))
	plan := Plan{Units: []Unit{
		{Key: a.ID, Changes: []Change{change(Fleet, moveEntry(a, full, Working, nil))}, Moved: a.ID + " ready -> working"},
		{Key: b.ID, Changes: []Change{change(Fleet, moveEntry(b, other, Working, nil))}, Moved: b.ID + " ready -> working"},
	}}
	held := HoldWidths(w.s, plan, nil)
	require.Len(t, held.Refused, 1, "one refusal: %v", held.Refused)
	assert.Equal(t, a.ID, held.Refused[0].Key)
	assert.Contains(t, held.Refused[0].Why, full+" is at its width (1 working of 1)")
	require.Len(t, held.Units, 1)
	assert.Equal(t, b.ID, held.Units[0].Key, "the other member's unit applies")
	w.must(Plan{Units: held.Units})
	assert.Equal(t, 1, w.s.Fleet.Count(full, Working))
	assert.Equal(t, 1, w.s.Fleet.Count(other, Working))
}

func TestMemberWidthIsNovaConfigs(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.s.Now = widthClock
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "hetzner1", Width: 16}))
	require.Equal(t, 16, w.s.Width("hetzner1"), "the control card says 16")
	cfg := map[string]int{"hetzner1": 8} // the config twin: nova-config machine set hetzner1 --width 8
	got, ok := RowWidth(w.s, "hetzner1", cfg)
	require.True(t, ok)
	assert.Equal(t, 8, got, "the step's width is nova-config's, not the copy's")
	// the check names the drift, one line with both values, as a violation
	drift := WidthDrift(w.s, cfg)
	require.Len(t, drift, 1)
	assert.Contains(t, drift[0], "hetzner1: sprint width 16, nova-config width 8")
	checks := DoctorFleetRows(DoctorInput{Snap: w.s, Machines: []DoctorMachine{{Name: "hetzner1", Width: 8}}})
	var fails []string
	for _, c := range checks {
		if c.Status == DoctorFail {
			fails = append(fails, c.Evidence)
		}
	}
	require.Len(t, fails, 1, "the width drift is a FAIL: %v", checks)
	assert.Contains(t, fails[0], "sprint width 16, nova-config width 8")
	// the refresh writes the copy back to nova-config's, and the drift is gone
	assert.Empty(t, WidthRefresh(w.s, Plan{}, cfg).Units, "a step that writes nothing writes no refresh")
	writes := Plan{Units: []Unit{{Key: "some-step"}}}
	p := WidthRefresh(w.s, writes, cfg)
	require.Len(t, p.Units, 2)
	assert.Contains(t, p.Units[1].Moved, "hetzner1 width=8 (was 16): read from nova-config")
	w.must(p)
	assert.Equal(t, 8, w.s.Width("hetzner1"))
	assert.Empty(t, WidthDrift(w.s, cfg))
	assert.Len(t, WidthRefresh(w.s, writes, cfg).Units, 1, "a refresh after a refresh writes nothing")
	// a member that drains keeps its drain: the refresh never lifts it
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "hetzner1", Drain: true}))
	require.Equal(t, 0, w.s.Width("hetzner1"))
	assert.Len(t, WidthRefresh(w.s, writes, cfg).Units, 1, "the drain is never lifted by a refresh")
	got, _ = RowWidth(w.s, "hetzner1", cfg)
	assert.Equal(t, 0, got, "a draining member takes nothing whatever the config says")
	// a create in working past the width is made ready instead, with its refusal
	unit := Unit{Key: "x", Changes: []Change{{Table: Fleet, Entry: ntable.BatchMemberEntry{ID: "x", Create: &ntable.MemberCreateOp{Row: "hetzner1", Col: Working}}}}, Moved: "x created"}
	held := HoldWidths(w.s, Plan{Units: []Unit{unit}}, cfg)
	require.Len(t, held.Units, 1)
	assert.Equal(t, Ready, held.Units[0].Changes[0].Entry.Create.Col)
	assert.True(t, strings.Contains(held.Units[0].Moved, "made ready, not working"))
	require.Len(t, held.Refused, 1)
}
