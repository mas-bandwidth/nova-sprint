package sprint

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/testkit/must"
)

// The landing stall (docs/SPEC-SPRINT.md, the judgments section; the owner,
// 2026-10-11: landed +0/30m while cards sat in review and fix and no judgment
// reached the seat). Red first on the twin store: a sprint with cards in
// review or merging and no landing for LandingStallBound raises exactly one
// NLandingStall judgment to the coordinator, with its cause breakdown.

// stallCard puts an existing work card in a column.
func stallCard(w *world, id, col string) {
	c := w.s.Work.Card(id)
	require.NotNil(w.t, c, "stallCard: no %s", id)
	c.Col = col
	c.Rev++
	w.s.Work.cells, w.s.Work.byPrimary = nil, nil
}

func TestLandingStallIsJudgedWhenCardsWaitAndNothingLands(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	stallCard(w, "s1-3", Merging)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	w.s.Work.SetProp(PropLandRefusal, "s1-2: the reader's finding stands")
	w.s.Open = append(w.s.Open, Open{Key: OpenKey("j1", "s1-4"),
		Note: Note{ID: "j1", Kind: Judgment, Type: "a judgment", Primaries: []string{"s1-4"}, At: w.s.Now.Add(-3 * time.Minute)}})
	p, _ := TickLandingStall(w.s, TickReq{})
	w.must(p)
	got := w.notesOf(NLandingStall)
	require.Len(t, got, 1, "one judgment for the stall")
	assert.Equal(t, Judgment, got[0].Kind)
	assert.Equal(t, "coordinator", got[0].To)
	must.ContainsAll(t, got[0].What, "1 in review and 1 merging", "reads out",
		"waiting on the coordinator's judgment: 1", "accepted and not merged: 1",
		"the lander's last refusal: s1-2", "CI green")
}

func TestLandingStallNamesTheRedCI(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	p, _ := TickLandingStall(w.s, TickReq{BaseCI: []BaseCIFacts{{Repo: "mas-bandwidth/nova-sprint", Base: "main", Red: true, Failed: []string{"the gate"}}}})
	w.must(p)
	got := w.notesOf(NLandingStall)
	require.Len(t, got, 1, "one judgment for the stall")
	assert.Contains(t, got[0].What, "CI red: mas-bandwidth/nova-sprint@main (the gate)", "the breakdown names the red check")
}

func TestLandingStallIsReRaisedEachBound(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	p, _ := TickLandingStall(w.s, TickReq{})
	w.must(p)
	p, _ = TickLandingStall(w.s, TickReq{}) // same bound: no second push
	w.must(p)
	require.Len(t, w.notesOf(NLandingStall), 1, "one push within the bound")
	w.tick(2 * LandingStallBound)
	p, _ = TickLandingStall(w.s, TickReq{})
	w.must(p)
	require.Len(t, w.notesOf(NLandingStall), 1, "re-raised in place, one open judgment")
	require.NotEmpty(t, w.updates, "the standing judgment is rewritten with fresh facts")
}

func TestLandingStallClearsOnTheNextLanding(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	p, _ := TickLandingStall(w.s, TickReq{})
	w.must(p)
	require.Len(t, w.notesOf(NLandingStall), 1)
	w.tick(time.Minute)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now))
	p, _ = TickLandingStall(w.s, TickReq{})
	w.must(p)
	again := w.notesOf(NLandingAgain)
	require.Len(t, again, 1, "a landing clears the episode")
	assert.Equal(t, Happened, again[0].Kind)
	assert.Contains(t, again[0].What, "landing again after")
	since, _ := w.s.Work.Prop(PropLandingStallSince)
	assert.Empty(t, since, "the episode is cleared")
}

func TestNoStallWithNothingWaiting(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	p, _ := TickLandingStall(w.s, TickReq{})
	w.must(p)
	assert.Empty(t, w.notesOf(NLandingStall), "nothing in review or merging: no stall")
}

func TestLandingStallClosesItsJudgmentWhenWaitingClears(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	p, _ := TickLandingStall(w.s, TickReq{})
	w.must(p)
	require.Len(t, w.notesOf(NLandingStall), 1)
	stallCard(w, "s1-1", Merged)
	p, _ = TickLandingStall(w.s, TickReq{})
	assert.Len(t, p.Closes, 1, "clearing the episode closes the standing judgment")
	w.must(p)
	_, open := openJudgmentOf(w.s, NLandingStall)
	assert.False(t, open, "no stale landing-stall judgment remains open")
}

func TestLandingReadCountsCombineFleetAndLegacyConsumersOnce(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	c := w.s.Work.Card("s1-1")
	require.NotNil(t, c)
	c.Fields["attempt"], c.Fields["head"] = "1", "h1"
	w.s.Readers.Put(&Card{ID: "legacy-jon", Row: "jon", Col: Asked, Fields: map[string]string{
		"primary": c.ID, "reader": "jon", "attempt": "1", "head": "h1", "asked": stamp(w.s.Now.Add(-time.Minute)),
	}})
	w.s.Readers.Put(&Card{ID: "legacy-kim", Row: "kim", Col: Asked, Fields: map[string]string{
		"primary": c.ID, "reader": "kim", "attempt": "1", "head": "h1", "asked": stamp(w.s.Now.Add(-2 * time.Minute)),
	}})
	w.s.Fleet.Put(&Card{ID: "read-jon", Row: "reader-jon", Col: Working, Fields: map[string]string{
		"primary": c.ID, "reader": "jon", "attempt": "1", "head": "h1", FieldReadCard: "1", "asked": stamp(w.s.Now.Add(-time.Minute)),
	}})
	assert.Len(t, landingReadConsumers(w.s, c.ID, true), 2, "legacy and fleet views of the same read count once")
	n := landingPush(w.s, "s1", "rowan", []*Card{c}, nil, w.s.Now)
	assert.Contains(t, n.What, "reads 2")
}

func TestLandingReadCountsAnActiveUnmarkedFleetRead(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	c := w.s.Work.Card("s1-1")
	require.NotNil(t, c)
	stallCard(w, c.ID, Review)
	c.Fields["attempt"], c.Fields["head"] = "1", "h1"
	w.s.Fleet.Put(&Card{ID: "legacy-friend-read", Row: "friend.stella", Col: Working, Fields: map[string]string{
		"kind": "read", "primary": c.ID, "reader": "stella", "attempt": "1", "head": "h1",
	}})
	assert.Len(t, landingReadConsumers(w.s, c.ID, false), 1, "the off-mode friend ask has no ReadCards marker but remains an active reader")
	assert.Contains(t, landingStallWhat(w.s, TickReq{}, time.Minute), "reads out: stella=1")
}

func TestLandingPushCountsCompletedFleetReadsAndBegunLegacyReads(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	c := w.s.Work.Card("s1-1")
	require.NotNil(t, c)
	c.Fields["attempt"], c.Fields["head"] = "1", "h1"
	w.s.Readers.Put(&Card{ID: "reading-jon", Row: "jon", Col: Reading, Fields: map[string]string{
		"primary": c.ID, "reader": "jon", "attempt": "1", "head": "h1", "asked": stamp(w.s.Now.Add(-time.Minute)),
	}})
	w.s.Readers.Put(&Card{ID: "ok-kim", Row: "kim", Col: OK, Fields: map[string]string{
		"primary": c.ID, "reader": "kim", "attempt": "1", "head": "h1", "asked": stamp(w.s.Now.Add(-2 * time.Minute)),
	}})
	w.s.Fleet.Put(&Card{ID: "done-amy", Row: "reader-amy", Col: DoneOK, Fields: map[string]string{
		"kind": "read", "primary": c.ID, "reader": "amy", "attempt": "1", "head": "h1", FieldReadCard: "1", "asked": stamp(w.s.Now.Add(-3 * time.Minute)),
	}})
	w.s.Fleet.Put(&Card{ID: "done-jon", Row: "reader-jon", Col: DoneFailed, Fields: map[string]string{
		"kind": "read", "primary": c.ID, "reader": "jon", "attempt": "1", "head": "h1", FieldReadCard: "1", "asked": stamp(w.s.Now.Add(-time.Minute)),
	}})
	assert.Len(t, landingReadConsumers(w.s, c.ID, true), 3, "completed fleet and legacy reads count, deduplicated by attempt")
	assert.Len(t, landingReadConsumers(w.s, c.ID, false), 1, "only the begun legacy read remains outstanding")
	n := landingPush(w.s, "s1", "rowan", []*Card{c}, nil, w.s.Now)
	assert.Contains(t, n.What, "reads 3")
}

func TestLandingReadHistoryDeduplicatesOnlyTheSameAttemptAndHead(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	c := w.s.Work.Card("s1-1")
	require.NotNil(t, c)
	c.Fields["attempt"], c.Fields["head"] = "2", "current"
	c.Fields[FieldLandingReadHistory] = "[{\"reader\":\"amy\",\"attempt\":1,\"head\":\"current\"},{\"reader\":\"amy\",\"attempt\":2,\"head\":\"old\"},{\"reader\":\"amy\",\"attempt\":2,\"head\":\"current\"}]"
	w.s.Fleet.Put(&Card{ID: "read-amy", Row: "friend.amy", Col: Working, Fields: map[string]string{
		"kind": "read", "primary": c.ID, "reader": "amy", "attempt": "2", "head": "current", FieldReadCard: "1",
	}})
	assert.Len(t, landingReadConsumers(w.s, c.ID, true), 1, "same consumer dedupes, stale attempts and heads do not count")
}

func TestAStallOfCardsOnTheCoordinatorIsNamedTheSeatsStall(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	w.s.Open = append(w.s.Open, Open{Key: OpenKey("j1", "s1-1"),
		Note: Note{ID: "j1", Kind: Judgment, Type: "a judgment", Primaries: []string{"s1-1"}, At: w.s.Now}})
	p, _ := TickLandingStall(w.s, TickReq{})
	w.must(p)
	got := w.notesOf(NLandingStall)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].What, "the seat's stall")
}

// Every landing is pushed to the coordinator as it happens (docs/SPEC-SPRINT.md,
// the judgments section; the owner, 2026-10-11: "you should also get this push
// notification, so you as coordinator can read the card that landed"). One
// happened note, "landed <card>" a line, with the stream, the head, the
// ledger entry's one-line :title and :text, and the attempts and reads.
func TestEveryLandingIsPushedToTheCoordinator(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	c := w.s.Work.Card("s1-1")
	require.NotNil(t, c)
	c.Fields["head"] = "abcdef0123456789"
	c.Fields[LandingTitleField] = "the thing is fixed"
	c.Fields[LandingTextField] = "the fix\non two lines"
	c.Fields["attempt"] = "3"
	w.must(Plan{Notes: []Note{landingPush(w.s, "s1", "rowan", []*Card{c}, nil, w.s.Now)}})
	got := w.notesOf(NLanding)
	require.Len(t, got, 1, "one push for the batch")
	assert.Equal(t, Happened, got[0].Kind)
	assert.Equal(t, "coordinator", got[0].To)
	must.ContainsAll(t, got[0].What, "landed s1-1", "stream s1", "abcdef0123",
		"the thing is fixed", "the fix on two lines", "attempts 3", "reads ")
}

// The merge step writes the landing-stall facts (docs/SPEC-SPRINT.md, the
// judgments section): the work table's last-landing stamp, so the tick's alarm
// measures from a landing, and the landing push reads the work card (its head,
// the member's ledger :title and :text, and its attempts), not the merge copy.
func TestMergeWritesTheLandingStallFacts(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	accepted(w, "s1-1")
	pr := w.s.Work.Card("s1-1")
	pr.Fields["head"] = "abcdef0123456789"
	pr.Fields[LandingTitleField] = "the thing is fixed"
	pr.Fields[LandingTextField] = "the fix\non two lines"
	pr.Fields["attempt"] = "3"
	w.must(MergeStep(w.s, MergeReq{Stream: "s1", Batch: 1}))
	last, ok := w.s.Work.Prop(PropLastLanding)
	require.True(t, ok, "the merge step stamps the work table's last landing")
	require.NotEmpty(t, last, "the stamp is the landing's time")
	got := w.notesOf(NLanding)
	require.Len(t, got, 1, "one landing push for the batch")
	must.ContainsAll(t, got[0].What, "landed s1-1", "abcdef0123",
		"the thing is fixed", "the fix on two lines", "attempts 3")
}

// The merge step records the lander's last refusal (docs/SPEC-SPRINT.md, the
// judgments section): the card and the reason, for the stall judgment's
// breakdown.
func TestMergeWritesTheLandersLastRefusal(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	accepted(w, "s1-1")
	w.must(MergeStep(w.s, MergeReq{Stream: "s1", Conflict: "s1-1", ConflictKind: "generated", Note: "the ledger could not resolve"}))
	v, ok := w.s.Work.Prop(PropLandRefusal)
	require.True(t, ok, "the merge step records the lander's last refusal")
	assert.Contains(t, v, "s1-1", "the refusal names the card")
	assert.Contains(t, v, "the ledger could not resolve", "the refusal names the reason")
}

// The landing stall is watched from the deadlines part, independent of the idle
// alarm (docs/SPEC-SPRINT.md, the judgments section): TickDeadlines runs
// TickLandingStall, so a stall is raised whether or not the idle alarm is on.
func TestTickDeadlinesRaisesTheLandingStall(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	p, _ := TickDeadlines(w.s, TickReq{})
	w.must(p)
	got := w.notesOf(NLandingStall)
	require.Len(t, got, 1, "the deadlines part raises the landing stall")
	assert.Equal(t, Judgment, got[0].Kind)
	assert.Equal(t, "coordinator", got[0].To)
	assert.Contains(t, got[0].What, "1 in review")
}

// The first stall is measured from when the tick first sees waiting cards, even
// when no card has ever landed (docs/SPEC-SPRINT.md, the judgments section).
func TestLandingStallStartsBeforeTheFirstLanding(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	p, _ := TickLandingStall(w.s, TickReq{})
	w.must(p)
	assert.Empty(t, w.notesOf(NLandingStall), "the first observation starts the running-time bound")
	w.tick(LandingStallBound)
	p, _ = TickLandingStall(w.s, TickReq{})
	w.must(p)
	got := w.notesOf(NLandingStall)
	require.Len(t, got, 1, "waiting before the first landing must raise a stall after the bound")
	assert.Equal(t, Judgment, got[0].Kind)
}

// A stall episode must survive ticks whose cadence is shorter than the bound:
// "the bound has not elapsed yet" is not "the condition cleared", so a
// no-prior-landing sprint whose machine ticks every few seconds still raises
// the judgment once the running-time age reaches the bound (docs/SPEC-SPRINT.md,
// the judgments section).
func TestLandingStallSurvivesTicksShorterThanTheBound(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	for i := 0; i < 10; i++ {
		p, _ := TickLandingStall(w.s, TickReq{})
		w.must(p)
		w.tick(10 * time.Second)
	}
	got := w.notesOf(NLandingStall)
	require.Len(t, got, 1, "a sprint with no landing still raises the stall across sub-bound ticks")
	assert.Equal(t, Judgment, got[0].Kind)
}

// The bound is a sprint setting with a one-minute default (docs/SPEC-SPRINT.md,
// the judgments section): set accepts a duration and default restores the default.
func TestLandingStallBoundIsSettable(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Set(w.s, SetReq{Who: w.s.Coordinator, LandingStallBound: "2m"}))
	got, ok := w.s.Work.Prop(PropLandingStallBound)
	require.True(t, ok)
	assert.Equal(t, "2m", got)
	w.must(Set(w.s, SetReq{Who: w.s.Coordinator, LandingStallBound: ReadTierDefault}))
	assert.Equal(t, LandingStallBound, w.s.landingStallBound())
	plan := Set(w.s, SetReq{Who: w.s.Coordinator, LandingStallBound: "soon"})
	assert.NotEmpty(t, plan.Refused)
}

// A landing that the tick sees only once the stall holds again (a tick gap
// longer than the bound) still ends the old episode first: the coordinator is
// told "landing again" and the old judgment closes, and the stall that holds
// after it is a new episode at the next tick (docs/SPEC-SPRINT.md, the
// judgments section).
func TestALandingSeenLateEndsTheOldStallBeforeANewOne(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	p, _ := TickLandingStall(w.s, TickReq{})
	w.must(p)
	require.Len(t, w.notesOf(NLandingStall), 1)
	w.tick(time.Minute)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now)) // a card lands; no tick sees it in the bound
	w.tick(3 * LandingStallBound)
	p, _ = TickLandingStall(w.s, TickReq{})
	w.must(p)
	again := w.notesOf(NLandingAgain)
	require.Len(t, again, 1, "the landing ends the old episode even when the stall holds again by the next tick")
	assert.Contains(t, again[0].What, "landing again after 1m0s", "measured from the episode's start to the landing")
	_, open := openJudgmentOf(w.s, NLandingStall)
	assert.False(t, open, "the old episode's judgment is closed")
	p, _ = TickLandingStall(w.s, TickReq{})
	w.must(p)
	require.Len(t, w.notesOf(NLandingStall), 2, "the stall that still holds is a new episode, raised at the next tick")
}

// A batch landing push stays inside the field bound however long the ledger
// texts are: a note over the bound would refuse the very landing it reports.
func TestALandingPushOfLongLedgerTextsStaysInsideTheFieldBound(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	var cards []*Card
	ledger := map[string]LandingLedgerEntry{}
	long := strings.Repeat("a long ledger text ", 8000)
	for _, id := range []string{"s1-1", "s1-2", "s1-3", "s1-4"} {
		c := w.s.Work.Card(id)
		require.NotNil(t, c)
		cards = append(cards, c)
		ledger[id] = LandingLedgerEntry{Title: "the title of " + id, Text: long}
	}
	n := landingPush(w.s, "s1", "rowan", cards, ledger, w.s.Now)
	assert.LessOrEqual(t, len(n.What), MaxCardTextBytes, "the push is inside the field bound")
	for _, id := range []string{"s1-1", "s1-2", "s1-3", "s1-4"} {
		assert.Contains(t, n.What, "landed "+id+" ", "every card keeps its line")
		assert.Contains(t, n.What, "the title of "+id)
	}
}

// The store's tick passes over a part whose plan is Empty, and Empty counts no
// property write (internal/sprint/store/tick.go, tickRun.parts): a plan that
// only stamps the episode must still be a plan the tick writes, or a sprint that
// has not yet landed never starts its episode on a real store and never raises
// the stall (docs/SPEC-SPRINT.md, the judgments section).
func TestALandingStallStampAloneIsAPlanTheTickWrites(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	p, _ := TickLandingStall(w.s, TickReq{})
	require.NotEmpty(t, p.Props, "the first observation stamps the episode")
	assert.False(t, p.Empty(), "a stamp-only plan is not passed over by the tick")
	w.must(p)
	stallCard(w, "s1-1", Merged)
	p, _ = TickLandingStall(w.s, TickReq{})
	require.NotEmpty(t, p.Props, "nothing waits: the episode's stamps are cleared")
	assert.False(t, p.Empty(), "a clear-only plan is not passed over by the tick")
	p, _ = TickDeadlines(w.s, TickReq{})
	assert.False(t, p.Empty(), "nor is the deadlines part that carries it")
}

// A batch of a hundred landings with long ledger texts is one push inside the
// field bound in which every card keeps its own line and its ledger title: the
// texts share what the lines' facts leave (docs/SPEC-SPRINT.md, the judgments
// section). Cutting the joined note instead dropped every line past the bound.
func TestALandingPushOfAHundredCardsKeepsEveryCardsLine(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	var cards []*Card
	ledger := map[string]LandingLedgerEntry{}
	long := strings.Repeat("a long ledger text ", 1000)
	for i := 1; i <= 100; i++ {
		id := fmt.Sprintf("s9-%d", i)
		cards = append(cards, &Card{ID: id, Row: "s9", Fields: map[string]string{"head": "abcdef0123456789", "attempt": "1"}})
		ledger[id] = LandingLedgerEntry{Title: "title " + id, Text: long}
	}
	n := landingPush(w.s, "s9", "rowan", cards, ledger, w.s.Now)
	require.LessOrEqual(t, len(n.What), MaxCardTextBytes, "the push is inside the field bound")
	for i := 1; i <= 100; i++ {
		id := fmt.Sprintf("s9-%d", i)
		assert.Contains(t, n.What, "landed "+id+" (stream s9", "%s keeps its line", id)
		assert.Contains(t, n.What, "title "+id+" ", "%s keeps its ledger title", id)
	}
}

// When even the lines' facts do not fit the bound, the lines that fit are kept
// and every other card is named by id on the last line: no landed card is
// dropped from the push.
func TestLandingLinesNameEveryCardWhenTheFactsAloneOverflow(t *testing.T) {
	t.Parallel()
	var lines []landingLine
	for i := 1; i <= 10; i++ {
		lines = append(lines, landingLine{id: fmt.Sprintf("s9-%d", i), stream: "s9", head: "abcdef0123", title: "t", text: "x", attempts: 1})
	}
	got := landingLines(lines, 400)
	require.LessOrEqual(t, len(got), 400)
	assert.Contains(t, got, "more landed:")
	named := map[string]bool{}
	for _, f := range strings.Fields(strings.NewReplacer("(", " ", ")", " ", ",", " ").Replace(got)) {
		named[f] = true
	}
	for i := 1; i <= 10; i++ {
		assert.True(t, named[fmt.Sprintf("s9-%d", i)], "s9-%d is named in %q", i, got)
	}
}

// At a sub-second bound a landing and the stall raised after it fall in one
// second: whole-second stamps read them as one time, and the next tick took the
// old landing for a new one and cleared the stall with no card landed (Stella's
// read of #117). The stamps order to the nanosecond (landingStamp).
func TestASubSecondBoundNeverTakesTheLastLandingForANewOne(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	w.s.Work.SetProp(PropLandingStallBound, "500ms")
	base := w.s.Now.Truncate(time.Second)
	accepted(w, "s1-1")
	w.s.Now = base.Add(100 * time.Millisecond)
	w.must(MergeStep(w.s, MergeReq{Stream: "s1", Batch: 1})) // s1-1 lands at :00.100
	stallCard(w, "s1-2", Review)
	w.s.Now = base.Add(700 * time.Millisecond)
	p, _ := TickLandingStall(w.s, TickReq{})
	w.must(p)
	require.Len(t, w.notesOf(NLandingStall), 1, "600ms with no landing past a 500ms bound: the stall is raised at :00.700")
	w.s.Now = base.Add(800 * time.Millisecond)
	p, _ = TickLandingStall(w.s, TickReq{})
	w.must(p)
	assert.Empty(t, w.notesOf(NLandingAgain), "no card landed after the stall began: no landing again")
	_, open := openJudgmentOf(w.s, NLandingStall)
	assert.True(t, open, "the stall judgment stays open")
}

// When the left-out ids alone pass the bound (700 long ids, no ledger text),
// the overflow line still counts every card left out, lists only whole ids, and
// says the list is partial: no id is cut partway (Stella's read of #117).
func TestALandingOverflowOfIDsPastTheBoundCountsAllAndCutsNoID(t *testing.T) {
	t.Parallel()
	var lines []landingLine
	ids := map[string]bool{}
	for i := 1; i <= 700; i++ {
		id := fmt.Sprintf("s9-%03d-%s", i, strings.Repeat("generated-id-", 15))
		ids[id] = true
		lines = append(lines, landingLine{id: id, stream: "s9", head: "abcdef0123", attempts: 1})
	}
	got := landingLines(lines, MaxCardTextBytes)
	require.LessOrEqual(t, len(got), MaxCardTextBytes, "the push is inside the field bound")
	kept, last := 0, ""
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "landed ") {
			kept++
		}
		last = line
	}
	var left, named int
	_, err := fmt.Sscanf(last, "... and %d more landed (partial list, %d named):", &left, &named)
	require.NoError(t, err, "the last line counts the cards left out and says the list is partial: %.200q", last)
	assert.Equal(t, 700, kept+left, "every landed card is a line or counted on the overflow line")
	tokens := 0
	for _, f := range strings.Fields(strings.NewReplacer("(", " ", ")", " ", ",", " ").Replace(got)) {
		if strings.HasPrefix(f, "s9-") {
			tokens++
			assert.True(t, ids[f], "an id cut partway: %.80q", f)
		}
	}
	assert.Equal(t, kept+named, tokens, "the named count is the ids listed")
}
