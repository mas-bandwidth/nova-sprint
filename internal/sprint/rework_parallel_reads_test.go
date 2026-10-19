package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTwoReadsReworkWaitsForBoth pins Change 2 of sprint-spec-fix-retired-read-keeps-findings:
// with two reads of an attempt dealt in parallel, the read-broken rule does not rework at the
// first broken verdict while the other read is open within its deadline. The primary stays in
// review with the broken read standing, and the rework runs when the other read reports ok or
// broken (carrying both readers' findings, a line each naming its reader), is handed back, or
// passes its deadline (the snapshot's clock: never forever); a sole broken finding keeps its
// reader's name whatever the other read did. The read-broken rule's PATHS widen waits the same
// way and carries both readers' words. With one read (--reads 1), a broken read reworks at
// once. tla/ReadCards.tla Rework, Widen, ReadDeadline, NoFindingLost, BugReworkAtFirstBroken,
// BugWidenAtFirstBroken.
func TestTwoReadsReworkWaitsForBoth(t *testing.T) {
	t.Parallel()

	const firstFinding = "a.go:1: the first reader's finding"

	// the first read found it broken; the rule's tick does not rework while the second works
	waiting := func(t *testing.T) (w *world, first, second *Card) {
		t.Helper()
		w = lateReadsWorld(t)
		first, second = twoReadsTaken(t, w, "s1-1", 1)
		w.must(readBy(w, first, "broken", firstFinding))
		require.Len(t, openOf(w, NReadBroken, "s1-1"), 1, "the broken read's judgment is open")
		p, _ := TickRuleRework(w.s, on())
		require.Empty(t, p.Units, "no rework while the other read is open: %+v", p)
		require.Empty(t, p.Refused)
		require.Equal(t, Review, w.state("s1-1"), "the primary stays in review")
		require.True(t, w.s.Fleet.Card(second.ID).Placed(), "the other read is kept, working")
		require.Equal(t, 1, w.s.Work.Card("s1-1").Int("attempt"))
		require.Equal(t, firstFinding, brokenFindings(w.s, w.s.Work.Card("s1-1")), "the broken read's finding stands")
		return w, first, w.s.Fleet.Card(second.ID)
	}
	reworked := func(t *testing.T, w *world) Packet {
		t.Helper()
		w.must(w.ruleRework(t))
		require.Equal(t, 2, w.s.Work.Card("s1-1").Int("attempt"), "reworked by the rule")
		require.Empty(t, openOf(w, NReadBroken, "s1-1"), "the judgment is answered")
		return nextPacket(t, w, "s1-1", 2)
	}

	t.Run("the second reports ok: the rework carries both readers' words", func(t *testing.T) {
		t.Parallel()
		w, first, second := waiting(t)
		w.must(readBy(w, second, "ok", "b.go:2 reads right to me"))
		pk := reworked(t, w)
		require.Contains(t, pk.Finding, "read by "+first.F("reader")+": broken: "+firstFinding)
		require.Contains(t, pk.Finding, "read by "+second.F("reader")+": ok: b.go:2 reads right to me")
	})

	t.Run("the second reports broken: the rework carries both findings", func(t *testing.T) {
		t.Parallel()
		w, first, second := waiting(t)
		w.must(readBy(w, second, "broken", "b.go:2: the second reader's finding"))
		pk := reworked(t, w)
		require.Contains(t, pk.Finding, "read by "+first.F("reader")+": broken: "+firstFinding)
		require.Contains(t, pk.Finding, "read by "+second.F("reader")+": broken: b.go:2: the second reader's finding")
		require.Contains(t, pk.Fix, firstFinding, "the fix is every broken finding")
		require.Contains(t, pk.Fix, "b.go:2: the second reader's finding")
		pr := w.s.Work.Card("s1-1")
		require.NotContains(t, pr.F("finding"), "read by ", "the primary keeps the plain finding the bounds compare")
		require.NotContains(t, pr.F(FieldFindings), "read by ")
	})

	t.Run("the second is handed back: the rework runs", func(t *testing.T) {
		t.Parallel()
		w, first, second := waiting(t)
		w.must(Read(w.s, ReadReq{Sel: Sel{IDs: []string{second.ID}}, As: second.F("reader"), Return: true, Reason: "cannot read it here"}))
		pk := reworked(t, w)
		require.Contains(t, pk.Finding, "read by "+first.F("reader")+": broken: "+firstFinding, "the sole broken finding keeps its reader")
	})

	t.Run("the second reports ok with no words: the sole broken finding keeps its reader", func(t *testing.T) {
		t.Parallel()
		w, first, second := waiting(t)
		w.must(readBy(w, second, "ok", ""))
		pk := reworked(t, w)
		require.Contains(t, pk.Finding, "read by "+first.F("reader")+": broken: "+firstFinding)
		require.NotContains(t, pk.Finding, ": ok:", "an ok with no words gives no line")
	})

	t.Run("a rule's PATHS widen waits for the other read too, and carries both", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\nREPO: mas-bandwidth/nova-tools\nPATHS: internal/x/a.go\n\nThe task.\n", "m1", 1)
		dealReads(t, w, nil)
		reads := readCardsOf(w, "s1-1")
		require.Len(t, reads, 2)
		for _, rc := range reads {
			w.must(Take(w.s, TakeReq{As: rc.Row, Sel: Sel{IDs: []string{rc.ID}}, Gens: gensOf(w.s, rc.ID)}))
		}
		first, second := w.s.Fleet.Card(reads[0].ID), w.s.Fleet.Card(reads[1].ID)
		const outside = "internal/y/b.go:1: the fix belongs outside PATHS"
		w.must(readBy(w, first, "broken", outside))
		p, _ := TickRuleWidenRead(w.s, on())
		require.Empty(t, p.Units, "no widen while the other read is open: its Brief would retire it")
		require.True(t, w.s.Fleet.Card(second.ID).Placed())
		require.Equal(t, Review, w.state("s1-1"))
		w.must(readBy(w, second, "ok", "b.go reads right otherwise"))
		p, _ = TickRuleWidenRead(w.s, on())
		require.NotEmpty(t, p.Units, "the widen runs once the other read reported: %+v", p.Refused)
		w.must(p)
		pr := w.s.Work.Card("s1-1")
		require.Contains(t, pr.F("brief"), "internal/y/b.go", "PATHS widened")
		require.Contains(t, pr.F("why"), "read by "+first.F("reader")+": broken: "+outside)
		require.Contains(t, pr.F("why"), "read by "+second.F("reader")+": ok: b.go reads right otherwise", "the other reader's words reach the widened attempt")
		require.Equal(t, outside, pr.F("finding"), "the finding stays the bounds' plain one")
	})

	t.Run("a rule's widen past the other read's deadline runs, and its late verdict is kept", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\nREPO: mas-bandwidth/nova-tools\nPATHS: internal/x/a.go\n\nThe task.\n", "m1", 1)
		dealReads(t, w, nil)
		reads := readCardsOf(w, "s1-1")
		require.Len(t, reads, 2)
		for _, rc := range reads {
			w.must(Take(w.s, TakeReq{As: rc.Row, Sel: Sel{IDs: []string{rc.ID}}, Gens: gensOf(w.s, rc.ID)}))
		}
		first, second := w.s.Fleet.Card(reads[0].ID), w.s.Fleet.Card(reads[1].ID)
		w.must(readBy(w, first, "broken", "internal/y/b.go:1: outside"))
		w.s.Now = w.s.Now.Add(ReadCardDeadline)
		p, _ := TickRuleWidenRead(w.s, on())
		require.NotEmpty(t, p.Units, "past the open read's deadline the widen runs: %+v", p.Refused)
		w.must(p)
		second = w.s.Fleet.Card(second.ID)
		require.Equal(t, RetiredByBrief, second.F("retired_by"))
		w.must(readBy(w, second, "broken", "g.go:7: past the deadline"))
		require.Contains(t, w.s.Work.Card("s1-1").F("why"), lateLine(second, "broken", "g.go:7: past the deadline"))
	})

	t.Run("the deadline passes: the rework runs and the open read's later verdict is a late read", func(t *testing.T) {
		t.Parallel()
		w, first, second := waiting(t)
		w.s.Now = w.s.Now.Add(ReadCardDeadline - 1)
		p, _ := TickRuleRework(w.s, on())
		require.Empty(t, p.Units, "still within the open read's deadline")
		w.s.Now = w.s.Now.Add(1)
		pk := reworked(t, w)
		require.Contains(t, pk.Finding, "read by "+first.F("reader")+": broken: "+firstFinding, "the sole broken finding keeps its reader")
		second = w.s.Fleet.Card(second.ID)
		require.False(t, second.Placed())
		require.Equal(t, RetiredByRework, second.F("retired_by"))
		w.must(readBy(w, second, "broken", "c.go:3: past the deadline"))
		require.Contains(t, nextPacket(t, w, "s1-1", 2).Finding, lateLine(second, "broken", "c.go:3: past the deadline"))
	})

	t.Run("the seat's rework is a mind's and runs at once", func(t *testing.T) {
		t.Parallel()
		w, _, _ := waiting(t)
		w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Who: "coordinator"}))
		require.Equal(t, 2, w.s.Work.Card("s1-1").Int("attempt"))
	})

	t.Run("--reads 1 reworks at once", func(t *testing.T) {
		t.Parallel()
		w := lateReadsWorld(t)
		putReviewBy(w, "s1-2", "s1-2: work (s1) tier: flash\n", "m1", 2)
		dealReads(t, w, nil)
		one := readCardsOf(w, "s1-2")
		require.Len(t, one, 1, "a flash card's one read")
		w.must(readBy(w, one[0], "broken", "c.go:3: one read"))
		w.must(w.ruleRework(t))
		require.Equal(t, 2, w.s.Work.Card("s1-2").Int("attempt"), "one read: the broken verdict reworks at once")
	})
}

// ruleRework is the tick's rule rework part, which must act.
func (w *world) ruleRework(t *testing.T) Plan {
	t.Helper()
	p, _ := TickRuleRework(w.s, on())
	require.NotEmpty(t, p.Units, "the rule reworks: %+v", p.Refused)
	return p
}
