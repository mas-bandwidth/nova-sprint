package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eReadersUp sets the readers table's rows and every one of them up.
func eReadersUp(w *world, rows ...string) {
	w.s.Readers.SetRows(rows)
	w.s.ReaderStates = map[string]string{}
	for _, rd := range rows {
		w.s.ReaderStates[rd] = ReaderUp
	}
}

// eRead puts the read card id (of primary at attempt 1, by reader) on the readers table:
// retired by `by` and unplaced when by is set, else live in col, asked now, at head when
// one is given.
func eRead(w *world, id, primary, reader, col, by, head string) {
	f := map[string]string{"kind": "read", "primary": primary, "attempt": "1", "reader": reader, "stream": "s1"}
	if by != "" {
		f["retired"], f["retired_by"] = stamp(w.s.Now), by
	} else {
		f["asked"] = stamp(w.s.Now)
		if head != "" {
			f["head"] = head
		}
	}
	w.s.Readers.Put(&Card{ID: id, Rev: 1, Row: reader, Col: col, Fields: f})
}

// eAwayAndLive puts reader's plain read of s1-1 retired by away and its second identity
// live in col at head.
func eAwayAndLive(w *world, reader, col, head string) {
	eRead(w, ReadCardID("s1-1", 1, reader), "s1-1", reader, "", "away", "")
	eRead(w, ReadCardSecondID("s1-1", 1, reader), "s1-1", reader, col, "", head)
}

// eCreates fails unless the plan carries a readers change for id, and checks that a create
// of it is asked on the reader.
func eCreates(t *testing.T, p Plan, id, reader, msg string) {
	t.Helper()
	found := false
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Table == Readers && ch.Entry.ID == id {
				found = true
				if ch.Entry.Create != nil {
					assert.Equal(t, Asked, ch.Entry.Create.Col)
					assert.Equal(t, reader, ch.Entry.Create.Row)
				}
			}
		}
	}
	assert.True(t, found, msg)
}

// TestAReadTakenBackByTheAwaySweepCanBeAskedAgainAtTheSameAttempt pins that
// when a reader's read card is taken back by sweepReads (retired_by away),
// and the reader comes back up, ticking the ask path (Ask / TickAsk) asks the
// read of it again at the same attempt using a second identity (generation suffix .g1).
// Under the interim rule (ReadCardForAsk) a read retired by the level, with no verdict,
// leaves its reader askable under .g1 too; a reader away is not asked.
func TestAReadTakenBackByTheAwaySweepCanBeAskedAgainAtTheSameAttempt(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	rows := []string{"reader-a", "reader-b", "reader-c", "reader-d"}
	eReadersUp(w, rows...)
	toReview(w, "s1-1")
	// a card's reads are asked together (ReadsWanted): both in one ask, and the
	// second comes back ok
	w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	firstReads := readsAt(w.s, w.s.Work.Card("s1-1"), 1)
	require.Len(t, firstReads, 2)
	r1, okReader := firstReads[0].F("reader"), firstReads[1].F("reader")
	w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: okReader, Verdict: "ok", Sel: Sel{IDs: []string{firstReads[1].ID}}}))

	// another reader holds a read of the attempt retired by the level, not by
	// the away sweep: askable again under .g1 (the interim rule), but away below.
	otherReader := ""
	for _, rd := range rows {
		if rd != r1 && rd != okReader {
			otherReader = rd
			break
		}
	}
	levelCardID := ReadCardID("s1-1", 1, otherReader)
	eRead(w, levelCardID, "s1-1", otherReader, "", RetiredByLevel, "")
	// Set r1 away so sweepReads takes its read back; another reader is up to
	// take it, so it is retired by away.
	w.s.ReaderStates[r1] = ReaderAway
	var sweepPlan Plan
	sweepReads(w.s, &sweepPlan)
	require.NotEmpty(t, sweepPlan.Units, "sweepReads takes back r1's read")
	w.must(sweepPlan)

	oldCard := w.s.Readers.Card(ReadCardID("s1-1", 1, r1))
	require.NotNil(t, oldCard)
	assert.False(t, oldCard.Placed())
	assert.Equal(t, "away", oldCard.F("retired_by"))

	// A pro card needs two different readers: okReader has read it ok, so one
	// more read is wanted (ReadsWanted). The rest go away, so r1 is the one
	// reader the re-ask can go to; bring r1 back up.
	for _, rd := range rows {
		if rd != r1 {
			w.s.ReaderStates[rd] = ReaderAway // otherReader too: askable under .g1, but away
		}
	}
	w.s.ReaderStates[r1] = ReaderUp

	// Drive the tick's ask path!
	askPlan := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.NotEmpty(t, askPlan.Units, "ask plan must produce units to re-ask r1")
	reaskID := ReadCardSecondID("s1-1", 1, r1)
	eCreates(t, askPlan, reaskID, r1, "ask unit created by the tick must carry the .g1 identity ("+reaskID+")")
	for _, u := range askPlan.Units {
		for _, ch := range u.Changes {
			assert.NotEqual(t, levelCardID, ch.Entry.ID, "otherReader, away, is not asked")
			assert.NotEqual(t, ReadCardSecondID("s1-1", 1, otherReader), ch.Entry.ID)
		}
	}
	w.must(askPlan)

	// After placing, the .g1 card is in Asked on r1.
	reaskCard := w.s.Readers.Placed(reaskID)
	require.NotNil(t, reaskCard)
	assert.Equal(t, Asked, reaskCard.Col)
	assert.Equal(t, r1, reaskCard.Row)
	assert.True(t, strings.HasSuffix(reaskCard.ID, ".g1"))

	// otherReader, retired by level with no verdict, was not asked (away) and is eligible
	// under the second identity (the interim rule).
	otherID, otherOK := ReadCardForAsk(w.s, "s1-1", 1, otherReader)
	assert.True(t, otherOK, "otherReader's levelled read has no verdict: askable again under .g1")
	assert.Equal(t, ReadCardSecondID("s1-1", 1, otherReader), otherID)

	// the primary now has the ok read and the re-asked read, the second identity.
	live := liveReadsAt(w.s, w.s.Work.Card("s1-1"), 1)
	require.Len(t, live, 2, "primary has both reads live")
	assert.Contains(t, []string{live[0].ID, live[1].ID}, reaskID)

	// Once the second identity card exists, ticking again does not ask r1 a third time.
	repeatPlan := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}})
	for _, u := range repeatPlan.Units {
		for _, ch := range u.Changes {
			if ch.Table == Readers && ch.Entry.Create != nil {
				assert.NotEqual(t, r1, ch.Entry.Create.Row, "r1 must not be asked a third time at same attempt")
			}
		}
	}
	_, reaskAgainOK := ReadCardForAsk(w.s, "s1-1", 1, r1)
	assert.False(t, reaskAgainOK, "r1 cannot be asked a third time at the same attempt")

	// ReadCardIDs returns both identities for per-tick record loads without extra round trips.
	assert.Equal(t, []string{"s1-1.r1." + r1, "s1-1.r1." + r1 + ".g1"}, ReadCardIDs("s1-1", 1, r1))
}

// TestAskDoesNotOveraskBesideALiveG1Read pins that readsAt enumerates both plain
// and second identities so Ask's classification loop treats a live .g1 read as
// kept, want does not overcount, and Ask does not over-ask beside a live .g1 read.
func TestAskDoesNotOveraskBesideALiveG1Read(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	eReadersUp(w, "reader-a", "reader-b", "reader-c")
	toReview(w, "s1-1") // pro card needing 2 readers
	// reader-a has plain read retired by away, and live .g1 read, ok: it counts toward
	// the two the card needs; reader-b had plain read retired by away
	eAwayAndLive(w, "reader-a", OK, "h1")
	eRead(w, ReadCardID("s1-1", 1, "reader-b"), "s1-1", "reader-b", "", "away", "")

	// Run Ask: needs 2 readers total, already has reader-a.g1, so want must be 1.
	plan := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Len(t, plan.Units, 1)
	// Must create only ONE new read (reader-b.g1), NOT both reader-b and reader-c
	var createdReaders []string
	for _, ch := range plan.Units[0].Changes {
		if ch.Table == Readers && ch.Entry.Create != nil {
			createdReaders = append(createdReaders, ch.Entry.Create.Row)
		}
	}
	assert.Len(t, createdReaders, 1, "must ask exactly 1 additional reader, not overcount beside live .g1 read")
	w.must(plan)
	// Placed live reads must be exactly 2 (reader-a and the newly asked reader)
	assert.Len(t, liveReadsAt(w.s, w.s.Work.Card("s1-1"), 1), 2)
}

// TestReaskedG1OkCountsTowardAcceptance pins that a .g1 read reporting ok
// counts toward okReaders, acceptable, and allows Accept to move the primary to merging.
func TestReaskedG1OkCountsTowardAcceptance(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	eReadersUp(w, "reader-a", "reader-b")
	toReview(w, "s1-1") // pro card needing 2 readers
	head := w.s.Work.Card("s1-1").F("head")
	// reader-a has plain read retired by away, and second identity .g1 that said ok;
	// reader-b has plain read that said ok
	eAwayAndLive(w, "reader-a", OK, head)
	eRead(w, ReadCardID("s1-1", 1, "reader-b"), "s1-1", "reader-b", OK, "", head)

	pr := w.s.Work.Card("s1-1")
	assert.Len(t, okReaders(w.s, pr), 2, "okReaders must see both reader-b plain ok and reader-a .g1 ok")
	assert.True(t, acceptable(w.s, pr), "acceptable must be true with .g1 ok read")
	plan := Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"s1-1"}}})
	assert.Empty(t, plan.Refused, "Accept must not refuse when .g1 read has ok")
	require.NotEmpty(t, plan.Units, "Accept must produce units")
	w.must(plan)
	assert.Equal(t, Merging, w.s.Work.Card("s1-1").Col)
}

// TestReviewJudgmentSeesG1ReadAndAvoidsSpuriousReadsExhausted pins that
// reviewJudgment sees live .g1 cards and does not emit spurious reads exhausted
// while a .g1 read is outstanding, and emits ready to accept once both say ok.
func TestReviewJudgmentSeesG1ReadAndAvoidsSpuriousReadsExhausted(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	eReadersUp(w, "reader-a", "reader-b")
	toReview(w, "s1-1") // pro card needing 2 readers
	head := w.s.Work.Card("s1-1").F("head")
	// reader-a has plain read retired by away, and live .g1 card in Asked; reader-b has
	// plain card in Reading
	eAwayAndLive(w, "reader-a", Asked, head)
	eRead(w, ReadCardID("s1-1", 1, "reader-b"), "s1-1", "reader-b", Reading, "", head)

	// reader-b reports ok via ReadStep
	w.must(Read(w.s, ReadReq{As: "reader-b", Verdict: "ok", Sel: Sel{IDs: []string{ReadCardID("s1-1", 1, "reader-b")}}}))
	// Since reader-a.g1 is still in Asked (outstanding), no NReadsExhausted judgment must be emitted!
	assert.Empty(t, w.notesOf(NReadsExhausted), "must not emit spurious reads exhausted while .g1 read is outstanding")
	// Now reader-a reports ok on its .g1 card
	w.must(Read(w.s, ReadReq{As: "reader-a", Verdict: "ok", Sel: Sel{IDs: []string{ReadCardSecondID("s1-1", 1, "reader-a")}}}))
	// Both readers have said ok: the tick accepts it (since 2026-10-06 no ready to
	// accept judgment, no hand step)
	assert.Empty(t, w.notesOf(NReadyToAccept), "no ready to accept judgment for a primary the tick accepts")
	assert.True(t, tickTakes(w, "s1-1"), "the tick accepts it after both readers (including .g1) say ok")
}

// TestTickLevelReadsCanMoveToReturnedReaderWithSecondIdentity pins that
// TickLevelReads uses ReadCardForAsk so a reader whose read was taken back by
// away sweep can receive a leveled read at the same attempt under second identity .g1.
func TestTickLevelReadsCanMoveToReturnedReaderWithSecondIdentity(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	eReadersUp(w, "reader-a", "reader-b", "reader-c")
	// reader-a has an away-retired card for s1-1 at attempt 1
	eRead(w, ReadCardID("s1-1", 1, "reader-a"), "s1-1", "reader-a", "", "away", "")
	// reader-a has a level-retired card for s1-2 at attempt 1, and (below) its second
	// identity spent too, so s1-2 cannot be moved to reader-a (the interim rule,
	// ReadCardForAsk: a read retired with no verdict leaves its reader askable once more)
	eRead(w, ReadCardID("s1-2", 1, "reader-a"), "s1-2", "reader-a", "", RetiredByLevel, "")
	toReview(w, "s1-1", "s1-2", "s1-3", "s1-4")
	spendSecond(w, "s1-2", 1, "reader-a")
	// reader-b holds both s1-1 and s1-2 in Asked (held=2), while reader-a holds 0 (held=0).
	// reader-c also holds 2 cards so it is not the recipient of the level.
	for rd, ids := range map[string][]string{"reader-b": {"s1-1", "s1-2"}, "reader-c": {"s1-3", "s1-4"}} {
		for _, id := range ids {
			eRead(w, ReadCardID(id, 1, rd), id, rd, Asked, "", "")
		}
	}

	// TickLevelReads runs the level part of the tick
	plan, _ := TickLevelReads(w.s, TickReq{})
	require.NotEmpty(t, plan.Units, "TickLevelReads must produce units evening loads")
	// Verify that s1-1 moved to reader-a with .g1 identity
	targetID := ReadCardSecondID("s1-1", 1, "reader-a")
	eCreates(t, plan, targetID, "reader-a", "TickLevelReads must create unit with second identity (.g1) on reader-a")
	w.must(plan)
	placed := w.s.Readers.Placed(targetID)
	require.NotNil(t, placed, "placed card with second identity must exist on reader-a")
	assert.Equal(t, Asked, placed.Col)
	assert.Equal(t, "reader-a", placed.Row)
}

// TestReadCardSecondIDAndGenerationSuffix pins that ReadCardSecondID and ReadCardIDs
// generate the correct second identity (.g1) and identity pairs.
func TestReadCardSecondIDAndGenerationSuffix(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "s1-1.r1.reader-a", ReadCardID("s1-1", 1, "reader-a"))
	assert.Equal(t, "s1-1.r1.reader-a.g1", ReadCardSecondID("s1-1", 1, "reader-a"))
	require.Equal(t, []string{"s1-1.r1.reader-a", "s1-1.r1.reader-a.g1"}, ReadCardIDs("s1-1", 1, "reader-a"))
}

// TestValidCardIDWithGenerationSuffix pins that ValidCardID accepts read cards
// with valid generation suffixes (.g1, .g2) and rejects malformed or non-read cards.
func TestValidCardIDWithGenerationSuffix(t *testing.T) {
	t.Parallel()
	for id, msg := range map[string]string{
		// Valid read cards with generation suffixes, and valid 3-part cards
		"s1-1.r1.reader-a.g1": "", "s1-1.r1.reader-a.g2": "", "s1-1.r2.reader-b.g10": "",
		"s1-1.r1.reader-a": "", "s1-1.w1": "", "s1-1": "",
	} {
		assert.True(t, ValidCardID(id), "%s %s", id, msg)
	}
	for id, msg := range map[string]string{
		// Invalid generation suffixes or card structures
		"s1-1.r1.reader-a.g0":       "g0 is invalid (generation starts at 1)",
		"s1-1.r1.reader-a.g":        "bare g is invalid",
		"s1-1.w1.reader-a.g1":       "non-read card cannot have 4 parts with generation suffix",
		"a.b.c.d":                   "arbitrary 4 parts is invalid",
		"s1-1.r1.reader-a.g1.extra": "5 parts is invalid",
		"":                          "",
	} {
		assert.False(t, ValidCardID(id), "%q %s", id, msg)
	}
}

// TestParseReadCardWithGenerationSuffix pins that ParseReadCard extracts primary,
// attempt, and reader from both plain and generation-suffixed read card identities.
func TestParseReadCardWithGenerationSuffix(t *testing.T) {
	t.Parallel()
	type parsed struct {
		primary string
		attempt int
		reader  string
		ok      bool
	}
	for id, want := range map[string]parsed{
		"s1-1.r1.reader-a":      {"s1-1", 1, "reader-a", true},   // plain read card
		"s1-1.r1.reader-a.g1":   {"s1-1", 1, "reader-a", true},   // second identity with .g1
		"mycard.r3.reader-z.g2": {"mycard", 3, "reader-z", true}, // higher generation
		// invalid read cards
		"s1-1.r1.reader-a.g0": {}, "s1-1.r1.reader-a.g": {}, "s1-1.w1.g1": {}, "s1-1.r0.reader-a": {},
	} {
		var got parsed
		got.primary, got.attempt, got.reader, got.ok = ParseReadCard(id)
		if !want.ok {
			assert.False(t, got.ok, id)
			continue
		}
		assert.Equal(t, want, got, id)
	}
}

// TestAskInsteadCanTakeBackLiveG1Read pins that insteadHeld checks ReadCardIDs
// and recognizes a live .g1 read as live (""), allowing ask --instead to take
// back the .g1 read and ask another reader instead.
func TestAskInsteadCanTakeBackLiveG1Read(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	eReadersUp(w, "reader-a", "reader-b", "reader-c")
	toReview(w, "s1-1") // pro card needing 2 readers
	// reader-a has plain read retired by away, and live .g1 read; reader-b has plain read in Asked
	eAwayAndLive(w, "reader-a", Asked, "h1")
	eRead(w, ReadCardID("s1-1", 1, "reader-b"), "s1-1", "reader-b", Asked, "", "h1")

	// ask --instead reader-a takes back the live .g1 read from reader-a and asks reader-c instead
	plan := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Instead: "reader-a"})
	assert.Empty(t, plan.Refused, "ask --instead must not be refused when reader-a holds a live .g1 read")
	require.Len(t, plan.Units, 1)
	// Verify reader-a's .g1 read was retired by coordinator
	var retiredReaderA, askedReaderC bool
	for _, ch := range plan.Units[0].Changes {
		if ch.Table != Readers {
			continue
		}
		if ch.Entry.ID == ReadCardSecondID("s1-1", 1, "reader-a") && ch.Entry.Remove {
			retiredReaderA = true
			assert.Equal(t, RetiredByCoordinator, ch.Entry.Set["retired_by"])
		}
		if ch.Entry.Create != nil && ch.Entry.Create.Row == "reader-c" {
			askedReaderC = true
			assert.Equal(t, Asked, ch.Entry.Create.Col)
		}
	}
	assert.True(t, retiredReaderA, "reader-a's .g1 read must be retired")
	assert.True(t, askedReaderC, "reader-c must be asked instead")
	w.must(plan)
	assert.Nil(t, w.s.Readers.Placed(ReadCardSecondID("s1-1", 1, "reader-a")))
	assert.NotNil(t, w.s.Readers.Placed(ReadCardID("s1-1", 1, "reader-c")))
}
