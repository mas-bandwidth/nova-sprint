package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Contains(t, got[0].What, "1 in review and 1 merging")
	assert.Contains(t, got[0].What, "reads out")
	assert.Contains(t, got[0].What, "waiting on the coordinator's judgment: 1")
	assert.Contains(t, got[0].What, "accepted and not merged: 1")
	assert.Contains(t, got[0].What, "the lander's last refusal: s1-2")
	assert.Contains(t, got[0].What, "CI red")
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
	assert.Contains(t, got[0].What, "landed s1-1")
	assert.Contains(t, got[0].What, "stream s1")
	assert.Contains(t, got[0].What, "abcdef0123")
	assert.Contains(t, got[0].What, "the thing is fixed")
	assert.Contains(t, got[0].What, "the fix on two lines")
	assert.Contains(t, got[0].What, "attempts 3")
	assert.Contains(t, got[0].What, "reads ")
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
	assert.Contains(t, got[0].What, "landed s1-1")
	assert.Contains(t, got[0].What, "abcdef0123", "the push reads the work card's head")
	assert.Contains(t, got[0].What, "the thing is fixed", "the push reads the work card's :title")
	assert.Contains(t, got[0].What, "the fix on two lines", "the push reads the work card's :text")
	assert.Contains(t, got[0].What, "attempts 3")
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

// A valid full-bound merge note remains valid when the informational refusal
// adds its card prefix (docs/SPEC-SPRINT.md, the judgments section).
func TestMergeBoundsTheLandingStallRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		kind string
	}{
		{"own head", "file"},
		{"lander", "generated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			accepted(w, "s1-1")
			note := strings.Repeat("x", MaxCardTextBytes)
			w.must(MergeStep(w.s, MergeReq{Stream: "s1", Conflict: "s1-1", ConflictKind: tc.kind, Note: note}))
			v, ok := w.s.Work.Prop(PropLandRefusal)
			require.True(t, ok)
			assert.LessOrEqual(t, len(v), MaxCardTextBytes, "the card prefix must not make a valid note exceed the property bound")
			assert.Contains(t, v, "s1-1: x", "the preview retains the card and reason")
			assert.True(t, strings.HasSuffix(v, "..."), "the informational refusal signals its truncation")
		})
	}
}
