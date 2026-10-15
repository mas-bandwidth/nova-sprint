package sprint

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseBrief is a card brief cut on base, the BASE: line stream set --base reads.
func baseBrief(base string) string {
	return "c: a card tier: flash\nREPO: example.invalid/owner/name\nBASE: " + base + "\nPATHS: cmd/test.go\n\nDo the task.\n"
}

// briefBase is the branch a brief's BASE: line names, for the test's own reads.
func briefBase(brief string) string {
	v, _ := cardhdr.Value(brief, "BASE")
	ref, _, _ := cardhdr.ParseBase(v)
	return ref
}

// setBaseWorld is stream s1 holding one card in each of the six states: waiting,
// ready, working (dealt, attempt 1, its brief carrying a CARRY: line at the attempt's
// head), review, merging (its merge card queued) and landed. The states past ready are
// set on the snapshot as the lifecycle leaves them.
func setBaseWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-ready"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-waiting"}, Brief: baseBrief("old/branch"), Needs: []string{"s1-ready"}, Who: "coordinator"}))
	for _, id := range []string{"s1-working", "s1-review", "s1-merging", "s1-landed"} {
		w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{id}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
	}
	require.Equal(t, Ready, w.s.Work.Placed("s1-ready").Col, "s1-ready is ready")
	require.Equal(t, Waiting, w.s.Work.Placed("s1-waiting").Col, "s1-waiting is waiting")

	working := w.s.Work.Placed("s1-working")
	working.Fields["attempt"] = "1"
	working.Fields["brief"] = strings.Replace(working.F("brief"), "\nPATHS:", "\nCARRY: s1-working attempt 1 head=0123456789abcdef0123456789abcdef01234567\nPATHS:", 1)
	working.Col = Working

	review := w.s.Work.Placed("s1-review")
	review.Fields["attempt"] = "1"
	review.Col = Review

	merging := w.s.Work.Placed("s1-merging")
	merging.Fields["attempt"] = "1"
	merging.Col = Merging
	w.s.Merge.Put(&Card{ID: "s1-merging", Row: "s1", Col: Queued, Rev: 1,
		Fields: map[string]string{"kind": "merge", "primary": "s1-merging", "stream": "s1"}})

	landed := w.s.Work.Placed("s1-landed")
	landed.Fields["attempt"] = "1"
	landed.Col = Landed
	return w
}

// TestStreamSetBaseRepointsEveryUnlandedCard: stream set --base rewrites the BASE:
// line of every card of the stream not yet landed, whatever its column, as a change
// of the work table the store writes; a landed card keeps its base. Before 2026-10-12
// it reached the cards not yet dealt and those queued to merge alone: a card in review
// kept BASE: main and the lander, which merges a card onto its own BASE:, landed it on
// main. Set mutates nothing in place: the plan carries the change, so the store is
// what writes it.
func TestStreamSetBaseRepointsEveryUnlandedCard(t *testing.T) {
	t.Parallel()
	w := setBaseWorld(t)
	unlanded := []string{"s1-ready", "s1-waiting", "s1-working", "s1-review", "s1-merging"}

	before := map[string]string{}
	for _, c := range w.s.Work.Cards() {
		if c.Placed() && c.Row == "s1" {
			before[c.ID] = c.F("brief")
		}
	}

	// the candidates the verb's check would hold to the base, bound to the step
	checked := StreamSetBaseChecks(w.s, []string{"s1"}, "sprint/new-branch")
	require.Len(t, checked, len(unlanded), "every unlanded card is checked, the landed one is not")

	p := Set(w.s, SetReq{Streams: []string{"s1"}, Base: "sprint/new-branch", BaseChecked: checked, Who: "coordinator"})
	require.Empty(t, p.Refused)

	// Set mutates nothing: the rewrite is a change of the work table, not a field
	// written on the snapshot the plan was built over.
	for _, c := range w.s.Work.Cards() {
		if c.Placed() && c.Row == "s1" {
			assert.Equal(t, before[c.ID], c.F("brief"), "%s: Set mutated the snapshot in place", c.ID)
		}
	}

	// the plan carries one change of the work table per re-pointed card, each with
	// the brief revision the replacement is, and one line per card moved
	changes, moved := map[string]string{}, ""
	for _, u := range p.Units {
		if u.Key == CtlID("s1") {
			moved = u.Moved
			continue
		}
		assert.Contains(t, u.Moved, u.Key+" base old/branch -> sprint/new-branch", "the card's move names both bases")
		for _, ch := range u.Changes {
			if ch.Table == Work {
				changes[u.Key] = ch.Entry.Set["brief"]
				assert.Equal(t, w.s.Work.Placed(u.Key).F("attempt"), ch.Entry.Set[FieldBriefAttempt], "%s: the brief revision is the card's attempt", u.Key)
			}
		}
	}
	for _, id := range unlanded {
		require.Contains(t, changes, id, "%s is re-pointed by a change of the work table", id)
		assert.Equal(t, "sprint/new-branch", briefBase(changes[id]), "%s carries the new base", id)
	}
	assert.NotContains(t, changes, "s1-landed", "a landed card keeps its base")
	assert.NotContains(t, moved, "keeps its base", "no unlanded card is kept on the old base")
	// a working card's attempt is cut on the old base: the BASE line alone moves, and the
	// CARRY: line naming the attempt's pushed head, where its next attempt starts, is kept
	assert.Contains(t, changes["s1-working"], "\nCARRY: s1-working attempt 1 head=0123456789abcdef0123456789abcdef01234567\n", "the working card's CARRY line is kept")

	w.must(p)
	for _, id := range unlanded {
		assert.Equal(t, "sprint/new-branch", briefBase(w.s.Work.Placed(id).F("brief")), "%s's BASE moved", id)
	}
	assert.Equal(t, "old/branch", briefBase(w.s.Work.Placed("s1-landed").F("brief")), "the landed card's BASE is kept")
	assert.Equal(t, Working, w.s.Work.Placed("s1-working").Col, "the working card is not re-dealt for the re-point")
	assert.Equal(t, Review, w.s.Work.Placed("s1-review").Col, "the review card keeps its place")
	assert.Equal(t, Merging, w.s.Work.Placed("s1-merging").Col, "the merging card keeps its place")
	assert.Equal(t, Queued, w.s.Merge.Placed("s1-merging").Col, "the merging card's merge card stays queued")
}

// TestStreamSetBaseChecksTheRewrittenBriefs: the check add runs is held over each
// brief the rewrite would carry, in work order, a dealt card's among them.
func TestStreamSetBaseChecksTheRewrittenBriefs(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-1"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-2"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
	w.s.Work.Placed("s1-2").Fields["attempt"] = "1"
	w.s.Work.Placed("s1-2").Col = Working

	checks := StreamSetBaseChecks(w.s, []string{"s1"}, "sprint/new-branch")
	require.Len(t, checks, 2, "the card not yet dealt and the dealt card are both checked")
	assert.Equal(t, "s1", checks[0].Stream)
	assert.Equal(t, "s1-1", checks[0].ID)
	assert.Equal(t, "s1-2", checks[1].ID)
	assert.Equal(t, baseBrief("old/branch"), checks[0].Was, "the check records the brief the card carried")
	assert.Equal(t, "sprint/new-branch", briefBase(checks[0].Brief))
	assert.Equal(t, "sprint/new-branch", briefBase(checks[1].Brief))
	assert.Contains(t, checks[0].Brief, "c: a card tier: flash", "every other byte is kept")
}

// TestStreamSetBaseRefusesWhenTheCheckedCardsMoved: the candidates the check add
// runs read at the base are bound to the write. A card added to the stream, or a
// brief revised, or a card landed after the check, refuses the step whole, nothing
// written: no brief is rewritten without its PATHS held to the base
// (sprint.baseChecksHeld; docs/SPEC-SPRINT.md section 11, stream set --base). A
// card dealt after the check is still re-pointed: the deal changes no brief.
func TestStreamSetBaseRefusesWhenTheCheckedCardsMoved(t *testing.T) {
	t.Parallel()
	const base = "sprint/new-branch"
	req := func(checked []StreamSetBaseCheck) SetReq {
		return SetReq{Streams: []string{"s1"}, Base: base, BaseChecked: checked, Who: "coordinator"}
	}

	// A card the check did not see, added to the stream while it fetched.
	t.Run("a card added", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, "reader-a")
		w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-1"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
		checked := StreamSetBaseChecks(w.s, []string{"s1"}, base)
		w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-2"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
		p := Set(w.s, req(checked))
		require.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "the stream moved while its briefs were held")
		assert.Empty(t, p.Units, "a refusal writes nothing")
	})

	// A brief revised while the check fetched: its PATHS are not the ones checked.
	t.Run("a brief revised", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, "reader-a")
		w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-1"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
		checked := StreamSetBaseChecks(w.s, []string{"s1"}, base)
		c := w.s.Work.Placed("s1-1")
		c.Fields["brief"] = strings.Replace(c.F("brief"), "PATHS: cmd/test.go", "PATHS: cmd/other.go", 1)
		p := Set(w.s, req(checked))
		require.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "changed while its brief was held")
		assert.Empty(t, p.Units, "a refusal writes nothing")
	})

	// A card landed while the check fetched: it is no card the check re-points.
	t.Run("a card landed", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, "reader-a")
		w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-1"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
		checked := StreamSetBaseChecks(w.s, []string{"s1"}, base)
		c := w.s.Work.Placed("s1-1")
		c.Fields["attempt"] = "1"
		c.Col = Landed
		p := Set(w.s, req(checked))
		require.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "the stream moved while its briefs were held")
		assert.Empty(t, p.Units, "a refusal writes nothing")
	})

	// A card whose landing is marked (MarkLanding wrote the landing head on its merge card:
	// the lander is between its mark and its push onto the old base): the whole base change
	// is refused, naming the card, inside the step that writes the base; nothing written,
	// no card of the stream re-pointed, and the verb is run again when the pass has ended.
	t.Run("a card landing", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, "reader-a")
		w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-1"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
		w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-2"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
		landing := w.s.Work.Placed("s1-2")
		landing.Fields["attempt"] = "1"
		landing.Fields["head"] = "0123456789abcdef0123456789abcdef01234567"
		landing.Col = Merging
		w.s.Merge.Put(&Card{ID: "s1-2", Row: "s1", Col: Queued, Rev: 1,
			Fields: map[string]string{"kind": "merge", "primary": "s1-2", "stream": "s1", FieldLandingHead: "0123456789abcdef0123456789abcdef01234567"}})
		checked := StreamSetBaseChecks(w.s, []string{"s1"}, base)
		require.Len(t, checked, 2, "both cards are candidates: the fence is the step's, not the check's")
		p := Set(w.s, req(checked))
		require.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "s1-2 of stream s1 is landing (its landing is marked at head 0123456789abcdef0123456789abcdef01234567)")
		assert.Contains(t, p.Refused[0].Why, "nothing was written")
		assert.Empty(t, p.Units, "a refusal writes nothing: s1-1 is not re-pointed either")
		assert.Equal(t, "old/branch", briefBase(w.s.Work.Placed("s1-1").F("brief")))
		assert.Equal(t, "old/branch", briefBase(w.s.Work.Placed("s1-2").F("brief")))
	})

	// A card dealt while the check fetched: its brief is the one the check read, and it
	// is re-pointed with the rest (its next attempt starts on the new base).
	t.Run("a card dealt", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, "reader-a")
		w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-1"}, Brief: baseBrief("old/branch"), Who: "coordinator"}))
		checked := StreamSetBaseChecks(w.s, []string{"s1"}, base)
		c := w.s.Work.Placed("s1-1")
		c.Fields["attempt"] = "1"
		c.Col = Working
		p := Set(w.s, req(checked))
		require.Empty(t, p.Refused, "a deal changes no brief: the candidates still hold")
		w.must(p)
		assert.Equal(t, base, briefBase(w.s.Work.Placed("s1-1").F("brief")), "the dealt card is re-pointed")
	})
}

// TestStreamSetBaseRefusesAStreamThatIsNoRow: a stream that is no row of either
// table refuses the whole call, nothing written.
func TestStreamSetBaseRefusesAStreamThatIsNoRow(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	p := Set(w.s, SetReq{Streams: []string{"nope"}, Base: "sprint/new-branch", Who: "coordinator"})
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "no stream nope")
	assert.Empty(t, p.Units, "a refusal writes nothing")
	assert.Empty(t, p.Props, "a refusal writes nothing")
}
