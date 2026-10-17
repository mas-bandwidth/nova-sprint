package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oldSkippedDrain is the 2026-10-17 state, made as v1.5.3's repair made it: the
// machine runs, a deal queues s1-1 and s1-2's moves to working, the tick's
// drain applies them and its release is lost, and the repair (before v1.5.4)
// judges both applied entries skipped (MEMBERREVISION: one revision past what
// they expected, at their target) and releases the drain without their move
// lines. It returns the drain's operation id; check then reports both cards
// under rule 13.
func (h *harness) oldSkippedDrain() string {
	h.t.Helper()
	h.startMachine()
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1", "s1-2"}}}))
	h.m.Fail = func(p string) error {
		if p == "release" {
			return errors.New("lost")
		}
		return nil
	}
	_, err := h.st.Run(h.ctx, DrainStep())
	h.m.Fail = nil
	require.Error(h.t, err, "the drain's release was not lost")
	op := h.m.Pending()
	require.NotNil(h.t, op, "the drain is not pending")
	pinned, err := h.st.pin(h.ctx)
	require.NoError(h.t, err)
	var skips []Skip
	for _, man := range op.Manifests {
		if man.Table != "t-work" {
			continue
		}
		for _, e := range man.Members {
			rs, err := pinned.readSet(h.ctx, man.Table, []string{e.ID})
			require.NoError(h.t, err)
			m, _ := rs.Member(e.ID)
			k := skipOf(man.Table, e, rs)
			k.Refused = fmt.Sprintf("mem: member %s: revision expected %s, observed %d; code=MEMBERREVISION; changed=no; run: nova-sprint check", e.ID, e.Expect.Revision, m.Revision)
			skips = append(skips, k)
		}
	}
	require.Len(h.t, skips, 2, "the drain's work manifest: %+v", op.Manifests)
	require.NoError(h.t, pinned.B.Release(h.ctx, pinned.withSkips(*op, skips), true))
	rep, _, err := h.st.Check(h.ctx, 0)
	require.NoError(h.t, err)
	n := 0
	for _, v := range rep.Violations {
		if v.Rule == 13 {
			n++
		}
	}
	require.Equal(h.t, 2, n, "the old repair's rule 13 violations: %v", rep.Violations)
	return op.ID
}

func (h *harness) skipOpen(id string) bool {
	open, err := h.m.OpenNotes(h.ctx)
	require.NoError(h.t, err)
	for _, o := range open {
		if o.Note.ID == id {
			return true
		}
	}
	return false
}

// repair --skipped (v1.5.4): each skipped card the store holds as the skip
// found it, one past what it expected, at its queued target, gets its missing
// move line back, carrying its queued line's words; the judgment closes; check
// holds; a dry run writes nothing, and the same --op again returns the result
// it recorded and writes nothing. tla/RepairLog.tla Reconcile.
func TestRepairSkippedRestoresTheDroppedLinesAndCloses(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	op := h.oldSkippedDrain()
	id := op + ".skip"
	require.True(t, h.skipOpen(id), "the skip judgment is not open")

	dry, err := h.st.ReconcileSkipped(h.ctx, id, "", true, false)
	require.NoError(t, err)
	assert.True(t, dry.Dry)
	assert.Equal(t, 2, dry.Counts[SkipRestore], "dry: %+v", dry)
	assert.True(t, dry.Closed, "dry: %+v", dry)
	assert.True(t, h.skipOpen(id), "a dry run closed the judgment")
	rep, _, err := h.st.Check(h.ctx, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, rep.Violations, "a dry run wrote the lines")

	res, err := h.st.ReconcileSkipped(h.ctx, id, "reconcile-1", false, false)
	require.NoError(t, err)
	assert.Equal(t, op, res.Op)
	assert.Equal(t, 2, res.Counts[SkipRestore], "%+v", res)
	assert.Zero(t, res.Counts[SkipRefused], "%+v", res)
	assert.True(t, res.Closed, "%+v", res)
	for _, e := range res.Entries {
		assert.Equal(t, "s1:ready", e.Expected, "%+v", e)
		assert.Equal(t, "s1:working", e.Found, "%+v", e)
	}
	assert.False(t, h.skipOpen(id), "the judgment is still open")
	h.clean("after repair --skipped")

	lines, err := h.st.Log(h.ctx)
	require.NoError(t, err)
	restored := 0
	for _, l := range lines {
		if l.Kind == sprint.LineMove && l.Op == op && l.Table == sprint.Work {
			restored++
			assert.Equal(t, sprint.DrainVerb, l.Verb, "%+v", l)
			assert.Equal(t, "s1:ready", l.From, "%+v", l)
			assert.Equal(t, "s1:working", l.To, "%+v", l)
			assert.Contains(t, l.Cause, "(deal", "the queued line's words: %+v", l)
			assert.Contains(t, l.Text["why"], "restored by tester", "%+v", l)
		}
	}
	assert.Equal(t, 2, restored, "restored move lines")

	again, err := h.st.ReconcileSkipped(h.ctx, id, "reconcile-1", false, false)
	require.NoError(t, err)
	assert.True(t, again.Replay, "the same --op did not replay: %+v", again)
	assert.Equal(t, res.Counts, again.Counts)
	_, err = h.st.ReconcileSkipped(h.ctx, id, "", false, false)
	require.Error(t, err, "a closed judgment was reconciled again")
	assert.Contains(t, err.Error(), "no open judgment is "+id)
	lines2, err := h.st.Log(h.ctx)
	require.NoError(t, err)
	assert.Len(t, lines2, len(lines), "a replay wrote lines")
}

// An entry the store does not hold as the skip found it (a writer outside the
// fence moved it, with no line) is refused by name and nothing is written for
// it; the others are restored; the judgment stays open.
func TestRepairSkippedRefusesAnEntryTheStoreDoesNotHoldAsFound(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	op := h.oldSkippedDrain()
	s := h.table()
	p := s.Work.Card("s1-1")
	_, err := h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: "t-work", Epoch: "0", ExpectedTableRevision: fmt.Sprint(s.Work.Revision), OperationID: "outside",
		Members: []ntable.BatchMemberEntry{{ID: p.ID, Expect: &ntable.MemberExpect{Place: &ntable.PlaceExpect{Row: p.Row, Col: p.Col}}, Set: map[string]string{"brief": "outside"}}}})
	require.NoError(t, err)
	res, err := h.st.ReconcileSkipped(h.ctx, op+".skip", "", false, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Counts[SkipRefused], "%+v", res)
	assert.Equal(t, 1, res.Counts[SkipRestore], "%+v", res)
	assert.False(t, res.Closed)
	for _, e := range res.Entries {
		if e.Card == "s1-1" {
			assert.Equal(t, SkipRefused, e.Outcome)
			assert.Contains(t, e.Why, "the store holds revision 3")
		}
	}
	assert.True(t, h.skipOpen(op+".skip"), "the judgment closed with an entry refused")
	assert.NotEmpty(t, res.Rule13, "the replay with the restored lines still breaks rule 13 on s1-1")
	rep, _, err := h.st.Check(h.ctx, 0)
	require.NoError(t, err)
	var named []string
	for _, v := range rep.Violations {
		named = append(named, v.Detail)
	}
	joined := strings.Join(named, "\n")
	assert.Contains(t, joined, "s1-1", "the refused card is no longer reported")
	assert.NotContains(t, joined, "s1-2", "the restored card is still reported")
}

// A card the log moved on, after the repair, from where the repair found it
// is settled with no line (one now would misplace it); a second run after a
// partial one settles the lines it restored.
func TestRepairSkippedSettlesACardTheLogMovedOn(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	op := h.oldSkippedDrain()
	h.stopMachine()
	h.must(DropStep(sprint.DropReq{Sel: sprint.Sel{IDs: []string{"s1-2"}}, Reason: "moved on"}))
	res, err := h.st.ReconcileSkipped(h.ctx, op+".skip", "", false, false)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Counts[SkipRestore], "%+v", res)
	assert.Equal(t, 1, res.Counts[SkipSettled], "%+v", res)
	for _, e := range res.Entries {
		if e.Card == "s1-2" {
			assert.Equal(t, SkipSettled, e.Outcome, "%+v", e)
			assert.Contains(t, e.Why, "moved on from s1:working")
		}
	}
	assert.True(t, res.Closed)
	h.clean("after repair --skipped")
}

// parseSkips reads Skip.String back, a store's text holding "; " included, and
// refuses a text whose entries do not come to its count.
func TestParseSkips(t *testing.T) {
	t.Parallel()
	ks := []Skip{
		{Card: "a", Primary: "a", Table: "work", Expected: "revision 3, at s:ready", Found: "revision 4, at s:working", Refused: "x; code=MEMBERREVISION; changed=no; run: y"},
		{Table: "work", Prop: "stream_index", Refused: "no member change of its manifest applied"},
		{Card: "b", Primary: "b", Table: "work", Expected: "revision 1, at s:review, result=ok", Found: "revision 2, at s:merging, result=ok", Refused: "z"},
	}
	var parts []string
	for _, k := range ks {
		parts = append(parts, k.String())
	}
	what := fmt.Sprintf("repair of %s (%s) skipped %d entries the store refused as recorded: %s", "op-1", "tick drain", len(ks), strings.Join(parts, "; "))
	op, verb, got, err := parseSkips(what)
	require.NoError(t, err)
	assert.Equal(t, "op-1", op)
	assert.Equal(t, sprint.DrainVerb, verb)
	require.Len(t, got, 3)
	assert.Equal(t, parsedSkip{card: "a", table: "work", expected: "revision 3, at s:ready", found: "revision 4, at s:working"}, got[0])
	assert.Equal(t, parsedSkip{prop: "stream_index", table: "work"}, got[1])
	r, p, ok := placeRev(got[2].found)
	assert.True(t, ok)
	assert.Equal(t, uint64(2), r)
	assert.Equal(t, "s:merging", p)
	_, _, _, err = parseSkips(strings.Replace(what, "skipped 3 entries", "skipped 4 entries", 1))
	assert.Error(t, err, "a count the entries do not come to")
	_, _, _, err = parseSkips("something else")
	assert.Error(t, err)
}

// handSkip writes a skip judgment by hand, as a repair of op (verb) would,
// listing the entries given, and returns its id.
func (h *harness) handSkip(op, verb string, entries ...string) string {
	h.t.Helper()
	what := fmt.Sprintf("repair of %s (%s) skipped %d entries the store refused as recorded: %s", op, verb, len(entries), strings.Join(entries, "; "))
	h.must(NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: NRepairSkipped, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: h.now, What: what,
		Decisions: append([]string(nil), RepairSkippedDecisions...)}))
	open, err := h.m.OpenNotes(h.ctx)
	require.NoError(h.t, err)
	for _, o := range open {
		if o.Note.Type == NRepairSkipped {
			return o.Note.ID
		}
	}
	h.t.Fatal("the hand skip judgment is not open")
	return ""
}

// An entry with no queued line naming the change is refused: nothing says the
// drain took the card where the store holds it.
func TestRepairSkippedRefusesAnEntryWithNoQueuedLine(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	c := h.table().Work.Card("s1-1")
	id := h.handSkip("op-x", sprint.DrainVerb, fmt.Sprintf("card s1-1 (primary s1-1) on t-work: expected revision %d, at %s:%s, found revision %d, at %s:%s; the store: x", c.Rev-1, c.Row, c.Col, c.Rev, c.Row, c.Col))
	res, err := h.st.ReconcileSkipped(h.ctx, id, "", false, false)
	require.NoError(t, err)
	require.Len(t, res.Entries, 1)
	assert.Equal(t, SkipRefused, res.Entries[0].Outcome, "%+v", res.Entries[0])
	assert.Contains(t, res.Entries[0].Why, "no queued line")
	assert.False(t, res.Closed)
	assert.True(t, h.skipOpen(id))
}

// Only a tick drain's skips are reconciled: the restored lines carry the
// drain's verb and the machine, so another verb's judgment is refused whole.
func TestRepairSkippedRefusesAnotherVerbsSkips(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	id := h.handSkip("op-y", "deal", "card s1-1 (primary s1-1) on t-work: expected revision 1, at s1:ready, found revision 2, at s1:working; the store: x")
	_, err := h.st.ReconcileSkipped(h.ctx, id, "", true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `the repair of a "deal" operation`)
}

// A table property is unverified, its stored value shown, and holds the
// judgment open; --accept-props settles it and the judgment closes.
func TestRepairSkippedLeavesPropertiesUnverifiedUnlessAccepted(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	id := h.handSkip("op-z", sprint.DrainVerb, "table property stream_index on t-work; the store: no member change of its manifest applied")
	res, err := h.st.ReconcileSkipped(h.ctx, id, "", false, false)
	require.NoError(t, err)
	require.Len(t, res.Entries, 1)
	assert.Equal(t, SkipUnverified, res.Entries[0].Outcome)
	assert.Contains(t, res.Entries[0].Why, "--accept-props")
	assert.False(t, res.Closed)
	assert.True(t, h.skipOpen(id))
	res, err = h.st.ReconcileSkipped(h.ctx, id, "", false, true)
	require.NoError(t, err)
	assert.Equal(t, SkipSettled, res.Entries[0].Outcome)
	assert.Empty(t, res.Rule13)
	assert.True(t, res.Closed, "%+v", res)
	assert.False(t, h.skipOpen(id))
}

// withoutSkipped takes only the skipped cards out of a set line, keeping the
// rest; a line left with none is dropped.
func TestWithoutSkippedKeepsTheRestOfASetLine(t *testing.T) {
	t.Parallel()
	st := &Store{Names: sprint.Names{Prefix: "t-"}}
	set := sprint.Line{Kind: sprint.LineMove, Table: sprint.Work, Card: "a", Cards: []string{"a", "b", "c"}, From: "s:ready", To: "s:working"}
	one := sprint.Line{Kind: sprint.LineMove, Table: sprint.Work, Card: "d", Primary: "d", To: "s:working"}
	pair := sprint.Line{Kind: sprint.LineMove, Table: sprint.Work, Card: "e", Cards: []string{"e", "f"}, To: "s:working"}
	skips := []Skip{{Card: "a", Table: "t-work"}, {Card: "c", Table: "t-work"}, {Card: "d", Table: "t-work"}, {Card: "e", Table: "t-work"}, {Card: "f", Table: "t-work"}}
	got := st.withoutSkipped([]sprint.Line{set, one, pair}, skips)
	require.Len(t, got, 1, "%+v", got)
	assert.Equal(t, "b", got[0].Card, "the first card left")
	assert.Nil(t, got[0].Cards, "one card left is a plain line")
	assert.Equal(t, "b", got[0].Primary)
	got = st.withoutSkipped([]sprint.Line{set}, []Skip{{Card: "b", Table: "t-work"}})
	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].Card)
	assert.Equal(t, []string{"a", "c"}, got[0].Cards, "a skipped card that is not first leaves the line")
}

// cutCause cuts on a UTF-8 boundary.
func TestCutCauseKeepsUTF8Whole(t *testing.T) {
	t.Parallel()
	c := strings.Repeat("a", sprint.MaxCause-4) + "é→" + strings.Repeat("b", 10)
	got := cutCause(c)
	assert.LessOrEqual(t, len(got), sprint.MaxCause)
	assert.True(t, utf8.ValidString(got), "%q", got)
	assert.True(t, strings.HasSuffix(got, "..."))
	assert.Equal(t, "short", cutCause("short"))
}
