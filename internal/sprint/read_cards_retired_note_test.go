package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lateReadsWorld is readCardsWorld with the clock at 1000.0 (time is an argument: the
// snapshot's Now, advanced only by assignment), members m1, m2 and m3 up at width 4.
func lateReadsWorld(t *testing.T) *world {
	t.Helper()
	members := []string{"m1", "m2", "m3"}
	var readers []string
	for _, m := range members {
		readers = append(readers, ReaderPrefix+m)
	}
	w := newWorld(t, readers...)
	w.s.Now = time.Unix(1000, 0).UTC()
	for _, m := range members {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 4}))
	}
	w.s.Work.SetProp(PropReadCards, ReadCardsOnWord)
	return w
}

// twoReadsTaken puts a pro primary in review worked by m1, deals its two read cards and has
// each reader take its own: the first and second read, each working.
func twoReadsTaken(t *testing.T, w *world, id string, score float64) (first, second *Card) {
	t.Helper()
	putReviewBy(w, id, id+": work (s1) tier: pro\n", "m1", score)
	dealReads(t, w, nil)
	reads := readCardsOf(w, id)
	require.Len(t, reads, 2, "a pro card's two reads are dealt in parallel")
	for _, rc := range reads {
		w.must(Take(w.s, TakeReq{As: rc.Row, Sel: Sel{IDs: []string{rc.ID}}, Gens: gensOf(w.s, rc.ID)}))
	}
	return w.s.Fleet.Card(reads[0].ID), w.s.Fleet.Card(reads[1].ID)
}

// readBy reports the read card's verdict as its reader does (read --as <reader>).
func readBy(w *world, rc *Card, verdict, finding string) Plan {
	return Read(w.s, ReadReq{Sel: Sel{IDs: []string{rc.ID}}, As: rc.F("reader"), Verdict: verdict, Finding: finding})
}

// lateLine is the line a late read writes: "late read by <reader> of attempt <n> at
// <head12>: <verdict>: <finding>".
func lateLine(rc *Card, verdict, finding string) string {
	head := rc.F("head")
	if len(head) > 12 {
		head = head[:12]
	}
	return "late read by " + rc.F("reader") + " of attempt " + rc.F("attempt") + " at " + orDash(head) + ": " + verdict + ": " + finding
}

// TestLateReadOfAReworkedAttemptIsANote pins Change 1 of
// sprint-spec-fix-retired-read-keeps-findings: a verdict reported on a read card a rework
// retired (retired_by rework) is recorded, never refused and never dropped. It is a note on
// the primary and a line in the next attempt's brief (its packet's finding), "late read by
// <reader> of attempt <n> at <head12>: <verdict>: <finding>"; it never counts as a standing
// read; the reader is told "recorded as a late read of <primary>"; a late read the next
// attempt was already running for is handed to that attempt's reads (the primary's fix every
// read packet carries) and carried by the rework after it; the same report twice is one
// record; a late verdict is on the reads ledger; a read retired by a brief edited in place (a
// PATHS widen) or by the live reconcile (lost) keeps its verdict too; an outbox report naming
// no read card is refused, never guessed; and a late read of a card dropped or landed is
// still refused. tla/ReadCards.tla LateClose, NoFindingLost, BugDropLate,
// BugLateMissesRunning.
func TestLateReadOfAReworkedAttemptIsANote(t *testing.T) {
	t.Parallel()

	// the coordinator reworks at the first broken read (a mind's rework runs at once), so the
	// second read is retired while its reader still works
	reworkedAtFirstBroken := func(t *testing.T, w *world, id string, score float64) (second *Card) {
		t.Helper()
		first, second := twoReadsTaken(t, w, id, score)
		w.must(readBy(w, first, "broken", "a.go:1: the first reader's finding"))
		w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{id}}, Who: "coordinator"}))
		second = w.s.Fleet.Card(second.ID)
		require.False(t, second.Placed(), "the rework retired the open read")
		require.Equal(t, RetiredByRework, second.F("retired_by"))
		require.Equal(t, 2, w.s.Work.Card(id).Int("attempt"))
		return second
	}

	t.Run("a late broken read is a note and in the next attempt's brief", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		second := reworkedAtFirstBroken(t, w, "s1-1", 1)
		p := w.must(readBy(w, second, "broken", "b.go:2: the second reader's finding"))
		require.Len(t, p.Units, 1)
		require.Contains(t, p.Units[0].Moved, "recorded as a late read of s1-1")
		want := lateLine(second, "broken", "b.go:2: the second reader's finding")
		notes := w.notesOf(NLateRead)
		require.Len(t, notes, 1, "the late read is a note on the primary")
		require.Equal(t, second.F("reader"), notes[0].Who)
		require.Equal(t, want, notes[0].What)
		require.Equal(t, []string{"s1-1"}, notes[0].Primaries)
		pk := nextPacket(t, w, "s1-1", 2)
		require.Contains(t, pk.Finding, want, "the next attempt's brief carries the late read")
		require.Contains(t, pk.Finding, "a.go:1: the first reader's finding", "and the rework's own finding")
		require.Contains(t, p.Units[0].Moved, "in the brief of attempt 2, not yet taken")
		require.Empty(t, w.s.Work.Card("s1-1").F(FieldLateReads), "in the brief it reaches: not carried a second time")
		require.NotContains(t, w.s.Work.Card("s1-1").F(FieldFindings), "late read", "never on the bounds' one line per attempt")
		require.Empty(t, openOf(w, NReadBroken, "s1-1"), "a late broken read raises no judgment: the attempt it read is gone")
		ledger, _ := w.s.Fleet.Prop(PropReadsWindow)
		require.Contains(t, ledger, second.Row+"\tbroken\t", "the late verdict is on the reads ledger as any read's is")

		// the same report twice is one record
		again := readBy(w, second, "broken", "b.go:2: the second reader's finding")
		require.Empty(t, again.Units)
		require.NotEmpty(t, again.Refused)
		require.Contains(t, again.Refused[0].Why, "recorded as a late read of s1-1 already")
	})

	t.Run("a late ok is a note and counts toward no read", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		second := reworkedAtFirstBroken(t, w, "s1-1", 1)
		w.must(readBy(w, second, "ok", "looks right at its head"))
		want := lateLine(second, "ok", "looks right at its head")
		require.Len(t, w.notesOf(NLateRead), 1)
		require.Contains(t, nextPacket(t, w, "s1-1", 2).Finding, want)
		pr := w.s.Work.Card("s1-1")
		standing, broken := readCardsStanding(w.s, pr, nil)
		require.Zero(t, standing, "a late ok never stands as a read of the next attempt")
		require.False(t, broken)
		require.Empty(t, okReaders(w.s, pr), "nor counts toward the accept")
		require.Equal(t, RetiredByRework, w.s.Fleet.Card(second.ID).F("retired_by"), "the read card stays as the rework retired it")
	})

	t.Run("a late read the next attempt already runs for is handed to its reads and carried by the rework after it", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		second := reworkedAtFirstBroken(t, w, "s1-1", 1)
		wc := w.s.Fleet.Card(WorkCardID("s1-1", 2))
		w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
		p := w.must(readBy(w, second, "broken", "c.go:3: found while attempt 2 ran"))
		want := lateLine(second, "broken", "c.go:3: found while attempt 2 ran")
		require.Contains(t, p.Units[0].Moved, "handed to the reads of attempt 2, which already runs")
		require.Contains(t, w.s.Work.Card("s1-1").F(FieldLateReads), want, "kept on the primary for the next rework")
		require.Contains(t, w.s.Work.Card("s1-1").F("fix"), want, "the running attempt's reads are told: every read packet of it carries the primary's fix")
		require.Contains(t, w.s.Fleet.Card(wc.ID).F("finding"), want, "and a redeal of the running attempt")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Head: carryHead2, Report: "done"}))
		require.Equal(t, Review, w.state("s1-1"))
		dealReads(t, w, nil)
		reads := readCardsOf(w, "s1-1")
		require.NotEmpty(t, reads, "attempt 2 is read")
		for _, rc := range reads {
			rp := PacketOf("sprint", 1, rc, w.s.Work.Card("s1-1"), nil, w.s.Fleet.Card(wc.ID))
			require.Contains(t, rp.Fix, want, "%s reads attempt 2 against the late read", rc.ID)
		}
		w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "and the late read's finding", Who: "coordinator"}))
		require.Contains(t, nextPacket(t, w, "s1-1", 3).Finding, want, "attempt 3 carries it")
		require.Empty(t, w.s.Work.Card("s1-1").F(FieldLateReads), "carried once, then cleared")
	})

	t.Run("an outbox report on a retired read is a late read too", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		second := reworkedAtFirstBroken(t, w, "s1-1", 1)
		w.must(FriendReadCloseChecked(w.s, second.F("reader"), "s1-1", second.ID, "Verdict: HOLD\n- d.go:4 the outbox finding\n", nil))
		require.Len(t, w.notesOf(NLateRead), 1)
		require.Contains(t, nextPacket(t, w, "s1-1", 2).Finding, "late read by "+second.F("reader")+" of attempt 1")
		require.Contains(t, nextPacket(t, w, "s1-1", 2).Finding, "d.go:4 the outbox finding")
	})

	t.Run("an outbox report naming no read card is refused, never guessed", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		second := reworkedAtFirstBroken(t, w, "s1-1", 1)
		p := FriendReadCloseChecked(w.s, second.F("reader"), "s1-1", "", "Verdict: HOLD\n- d.go:4 which read?\n", nil)
		require.Empty(t, p.Units)
		require.NotEmpty(t, p.Refused)
		require.Contains(t, p.Refused[0].Why, "a late read names its read card's id")
		require.Empty(t, w.notesOf(NLateRead))
	})

	t.Run("a read retired by a brief edited in place (a PATHS widen) keeps its verdict", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		first, second := twoReadsTaken(t, w, "s1-1", 1)
		w.must(readBy(w, first, "broken", "internal/y/b.go:1: outside PATHS"))
		pr := w.s.Work.Card("s1-1")
		w.must(Brief(w.s, BriefReq{ID: "s1-1", Brief: pr.F("brief") + "PATHS: internal/x/a.go,internal/y/b.go\n", Who: "coordinator"}))
		second = w.s.Fleet.Card(second.ID)
		require.False(t, second.Placed())
		require.Equal(t, RetiredByBrief, second.F("retired_by"))
		p := w.must(readBy(w, second, "broken", "e.go:5: read before the widen"))
		want := lateLine(second, "broken", "e.go:5: read before the widen")
		require.Contains(t, p.Units[0].Moved, "recorded as a late read of s1-1")
		require.Len(t, w.notesOf(NLateRead), 1)
		dealt := w.s.Work.Card("s1-1")
		require.Contains(t, dealt.F("why"), want, "the widened attempt, not yet dealt, carries it on its why")
		require.NotContains(t, dealt.F(FieldFindings), "late read")
	})

	t.Run("a read the live reconcile retired lost keeps its verdict for the attempt's rework", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		first, second := twoReadsTaken(t, w, "s1-1", 1)
		w.must(Plan{Units: []Unit{readLostUnit(w.s, second, second.Int("gen"), "not named for the grace", false)}})
		second = w.s.Fleet.Card(second.ID)
		require.Equal(t, RetiredByLost, second.F("retired_by"))
		w.must(readBy(w, second, "broken", "f.go:6: read while the reconcile took it"))
		want := lateLine(second, "broken", "f.go:6: read while the reconcile took it")
		pr := w.s.Work.Card("s1-1")
		require.Equal(t, 1, pr.Int("attempt"))
		require.Contains(t, pr.F(FieldLateReads), want, "kept for the rework of the attempt it read")
		standing, broken := readCardsStanding(w.s, pr, nil)
		require.False(t, broken, "a late broken never stands as a read")
		require.LessOrEqual(t, standing, 1, "only the first read's card stands")
		w.must(readBy(w, first, "broken", "a.go:1: the first reader's finding"))
		w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Who: "coordinator"}))
		require.Contains(t, nextPacket(t, w, "s1-1", 2).Finding, want, "the rework carries it")
	})

	refusedOff := func(t *testing.T, w *world, second *Card) {
		t.Helper()
		p := readBy(w, second, "ok", "late")
		require.Empty(t, p.Units, "nothing is recorded")
		require.NotEmpty(t, p.Refused)
		require.True(t, strings.Contains(p.Refused[0].Why, "off the table") && strings.Contains(p.Refused[0].Why, "nothing to report"), "%v", p.Refused)
		require.Empty(t, w.notesOf(NLateRead))
	}

	t.Run("a late read of a dropped card is still refused", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		second := reworkedAtFirstBroken(t, w, "s1-1", 1)
		w.must(Drop(w.s, DropReq{Sel: Sel{IDs: []string{"s1-1"}}, Reason: "not wanted"}))
		refusedOff(t, w, second)
	})

	t.Run("a late read of a landed card is still refused", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		second := reworkedAtFirstBroken(t, w, "s1-1", 1)
		// the record a landing leaves: off the table, its outcome landed
		pr := w.s.Work.Card("s1-1")
		pr.Row, pr.Col, pr.Fields["outcome"] = "", "", "landed"
		w.s.Work.cells, w.s.Work.byPrimary = nil, nil
		refusedOff(t, w, second)
	})
}
