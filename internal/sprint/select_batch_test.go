package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A selector holds of a card when every field given holds of it (docs/SPEC-SPRINT.md,
// "One selector, one step"): a hard pin is the friend's too, a landed card never.
func TestASelectorHoldsWhenEveryFieldHolds(t *testing.T) {
	t.Parallel()
	amy := &Card{ID: "a", Row: "s1", Col: Ready, Fields: map[string]string{FieldWho: "friend.amy"}}
	only := &Card{ID: "o", Row: "s1", Col: Waiting, Fields: map[string]string{FieldWho: "only.friend.amy"}}
	none := &Card{ID: "n", Row: "s2", Col: Waiting, Fields: map[string]string{FieldHeld: "t"}}
	landed := &Card{ID: "l", Row: "s1", Col: Landed, Fields: map[string]string{FieldWho: "friend.amy"}}
	for _, tc := range []struct {
		q    Selector
		want []*Card
	}{
		{Selector{Who: "friend.amy"}, []*Card{amy, only}},
		{Selector{Who: "amy"}, []*Card{amy, only}},
		{Selector{Who: WhoNone}, []*Card{none}},
		{Selector{Who: "friend.amy", State: "waiting"}, []*Card{only}},
		{Selector{State: "held"}, []*Card{none}},
		{Selector{State: "waiting"}, []*Card{only}},
		{Selector{Stream: "s1", IDs: []string{"a", "n"}}, []*Card{amy}},
	} {
		var got []*Card
		for _, c := range []*Card{amy, only, none, landed} {
			if tc.q.Selects(c) {
				got = append(got, c)
			}
		}
		assert.Equal(t, tc.want, got, "%+v", tc.q)
	}
	assert.Contains(t, Selector{Who: "friend.a b"}.Check(), "--who wants")
	assert.Contains(t, Selector{State: "done"}.Check(), "--state wants")
	assert.Empty(t, Selector{Who: "friend", State: "merging"}.Check())
}

// A brief transform edits the header block alone: the WHO line taken out, the BASE line
// replaced or put under line 1; prose below the header is never touched.
func TestABriefTransformEditsTheHeaderAlone(t *testing.T) {
	t.Parallel()
	brief := "c1: a card\nREPO: r\nWHO: friend amy\nBASE: main\n\nWHO: this prose stays\nBASE: so does this"
	assert.Equal(t, "c1: a card\nREPO: r\nBASE: main\n\nWHO: this prose stays\nBASE: so does this", BriefEdit{DropWho: true}.Apply(brief))
	assert.Equal(t, "c1: a card\nREPO: r\nWHO: friend amy\nBASE: v2\n\nWHO: this prose stays\nBASE: so does this", BriefEdit{SetBase: "v2"}.Apply(brief))
	assert.Equal(t, "c1: a card\nBASE: v2\nREPO: r\n\nprose", BriefEdit{SetBase: "v2"}.Apply("c1: a card\nREPO: r\n\nprose"))
}

// The batch folds a card's later changes into its first, guarded on what the step read: a
// set then a removal is one removal with both; a card changed after it left the table, or
// created twice, is refused.
func TestABatchFoldsOneEntryPerCard(t *testing.T) {
	t.Parallel()
	c := &Card{ID: "x", Row: "s1", Col: Waiting, Rev: 7, Fields: map[string]string{"needs": "a"}}
	units, bad := foldUnits([]Unit{
		{Key: "x", Changes: []Change{change(Work, setEntry(c, map[string]string{"needs": "ab"}))}, Moved: "x needs a -> ab"},
		{Key: "x", Changes: []Change{change(Work, removeEntry(withField(c, "rev", "8"), map[string]string{"outcome": "dropped"}))}, Moved: "x dropped"},
	})
	assert.Empty(t, bad)
	if assert.Len(t, units, 1) {
		e := units[0].Changes[0].Entry
		assert.True(t, e.Remove)
		assert.Equal(t, "7", e.Expect.Revision, "guarded on the revision the step read")
		assert.Equal(t, map[string]string{"needs": "ab", "outcome": "dropped"}, e.Set)
		assert.Equal(t, "x needs a -> ab; x dropped", units[0].Moved)
	}
	_, bad = foldUnits([]Unit{
		{Changes: []Change{change(Work, removeEntry(c, nil))}},
		{Changes: []Change{change(Work, setEntry(c, map[string]string{"k": "v"}))}},
	})
	assert.Contains(t, bad, "after it left the table")
	create := createEntry("n", "s1", Waiting, 1, nil)
	_, bad = foldUnits([]Unit{{Changes: []Change{change(Work, create)}}, {Changes: []Change{change(Work, create)}}})
	assert.Contains(t, bad, "create it")
}

func TestSelectorGivenWordsSelectedAndSelectIDs(t *testing.T) {
	t.Parallel()
	assert.False(t, Selector{}.Given())
	assert.True(t, Selector{Stream: "s1"}.Given())
	assert.True(t, Selector{Who: "amy"}.Given())
	assert.True(t, Selector{State: "ready"}.Given())
	assert.True(t, Selector{IDs: []string{"s1-1"}}.Given())

	q := Selector{Stream: "s1", Who: "amy", State: "ready", IDs: []string{"s1-1", "s1-2"}}
	assert.Equal(t, "--stream s1 --who amy --state ready --ids-file (2 ids)", q.Words())
	assert.Equal(t, "the selector names no card (--stream s1); nothing was changed", NoneSelected(Selector{Stream: "s1"}))
	assert.Contains(t, Selector{IDs: []string{"bad id"}}.Check(), "is no card id")

	assert.False(t, BriefEdit{}.Given())
	assert.True(t, BriefEdit{SetBase: "v2"}.Given())
	assert.True(t, BriefEdit{DropWho: true}.Given())

	w := newWorld(t)
	putCard(w.s.Work, "s2-1", "s2", Ready)
	putCard(w.s.Work, "s1-2", "s1", Ready).Score = 20
	putCard(w.s.Work, "s1-1", "s1", Ready).Score = 10
	putCard(w.s.Work, "s1-1b", "s1", Waiting).Score = 10

	ids, refused := Selected(w.s, Selector{State: string(Ready)})
	assert.Empty(t, refused)
	assert.Equal(t, []string{"s1-1", "s1-2", "s2-1"}, ids, "sorted by stream, score, then id")

	// tie-break on ID when stream and score match
	idsAll, refused := Selected(w.s, Selector{Stream: "s1"})
	assert.Empty(t, refused)
	assert.Equal(t, []string{"s1-1", "s1-1b", "s1-2"}, idsAll)

	// refusals on IDs: duplicate, missing/unplaced, and mismatch with rest of selector
	_, refused = Selected(w.s, Selector{Stream: "s1", IDs: []string{"s1-1", "s1-1", "no-card", "s2-1"}})
	if assert.Len(t, refused, 3) {
		assert.Equal(t, "s1-1", refused[0].Key)
		assert.Contains(t, refused[0].Why, "named twice")
		assert.Equal(t, "no-card", refused[1].Key)
		assert.Equal(t, "s2-1", refused[2].Key)
		assert.Contains(t, refused[2].Why, "the rest of the selector")
	}

	// SelectIDs: refusal passthrough, empty selection refusal, and happy path with Said lines
	p := SelectIDs(w.s, "hold", Selector{Stream: "s9"}, func(got []string) Plan {
		return Plan{}
	})
	assert.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "names no card")

	p = SelectIDs(w.s, "hold", Selector{Stream: "s1", IDs: []string{"no-card"}}, func(got []string) Plan {
		return Plan{}
	})
	assert.Len(t, p.Refused, 1)

	p = SelectIDs(w.s, "hold", Selector{Stream: "s1", State: string(Ready)}, func(got []string) Plan {
		return Plan{Units: []Unit{{Key: got[0], Moved: "held"}}}
	})
	assert.Empty(t, p.Refused)
	assert.Equal(t, []string{
		"hold s1-1: selected",
		"hold s1-2: selected",
		"hold selected=2 by --stream s1 --state ready",
	}, p.Said)
}

func TestBatchApplyPlanAndApplyEntry(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.s.Work.Put(&Card{ID: "s1-1", Row: "s1", Col: Waiting, Rev: 1, Fields: map[string]string{"k": "v1", "drop": "x"}})
	w.s.Fleet.Put(&Card{ID: "m1", Row: "m1", Col: Up, Rev: 1, Fields: map[string]string{DoneOK: "2"}})
	w.s.Readers.Put(&Card{ID: "r1", Row: "r1", Col: Asked, Rev: 1, Fields: map[string]string{}})
	w.s.Merge.Put(&Card{ID: "ctl-s1", Row: "s1", Col: Ctl, Rev: 1, Fields: map[string]string{}})
	openJ := Open{Key: "n1|s1-1", Note: Note{ID: "n1", Kind: Judgment, Type: NWorkFailed, Stream: "s1"}}

	p := Batch(w.s, []func(*Snapshot) Plan{
		func(cur *Snapshot) Plan {
			c := cur.Work.Card("s1-1")
			r := cur.Readers.Card("r1")
			m := cur.Merge.Card("ctl-s1")
			mv := moveEntry(c, "s1", Ready, map[string]string{"k": "v2"}, "drop")
			score := 42.0
			mv.Move.Score = &score
			return Plan{
				Rows:   []RowAdd{{Table: Work, Row: "s1"}, {Table: Work, Row: "s2"}},
				Props:  []PropWrite{{Table: Work, Name: "p1", Value: "first", WasAbsent: true}},
				Closes: []Open{openJ},
				Units: []Unit{{
					Key: "s1-1",
					Changes: []Change{
						change(Work, mv),
						change(Work, createEntry("s1-new", "s1", Waiting, 5, map[string]string{"a": "1"})),
						change(Readers, removeEntry(r, map[string]string{"retired_by": "test"})),
						change(Merge, guardEntry(m)),
					},
					Bumps: []Bump{{Table: Fleet, ID: "m1", Field: DoneOK, Delta: 3}},
					Moved: "step 1",
				}},
			}
		},
		func(cur *Snapshot) Plan {
			// Verify applyPlan applied step 1's changes, bumps, and props onto cur
			c := cur.Work.Card("s1-1")
			assert.Equal(t, Ready, c.Col)
			assert.Equal(t, 42.0, c.Score)
			assert.Equal(t, "v2", c.F("k"))
			assert.Empty(t, c.F("drop"))
			assert.Equal(t, 5, cur.Fleet.Card("m1").Int(DoneOK))
			v, ok := cur.Work.Prop("p1")
			assert.True(t, ok)
			assert.Equal(t, "first", v)
			assert.False(t, cur.Readers.Card("r1").Placed())

			created := cur.Work.Card("s1-new")
			return Plan{
				Rows:   []RowAdd{{Table: Work, Row: "s1"}}, // duplicate row deduplicated
				Props:  []PropWrite{{Table: Work, Name: "p1", Value: "second", Was: "first"}},
				Closes: []Open{openJ}, // duplicate close deduplicated
				Units: []Unit{{
					Key: "s1-new",
					Changes: []Change{
						change(Work, moveEntry(created, "s1", Ready, map[string]string{"b": "2"})),
						change(Work, guardEntry(c)),
					},
					Moved: "step 2",
				}},
			}
		},
	})
	assert.Empty(t, p.Refused)
	assert.Len(t, p.Rows, 2)
	if assert.Len(t, p.Props, 1) {
		assert.Equal(t, "second", p.Props[0].Value)
		assert.True(t, p.Props[0].WasAbsent, "guarded on initial state before batch")
	}
	assert.Len(t, p.Closes, 1)

	// Step refusal aborts the batch
	refusedBatch := Batch(w.s, []func(*Snapshot) Plan{
		func(cur *Snapshot) Plan {
			var q Plan
			q.refuse("s1-1", "cannot proceed")
			return q
		},
	})
	assert.Len(t, refusedBatch.Refused, 1)

	// foldUnits conflict inside Batch returns a batch refusal
	conflictBatch := Batch(w.s, []func(*Snapshot) Plan{
		func(cur *Snapshot) Plan {
			return Plan{Units: []Unit{{Key: "dup", Changes: []Change{change(Work, createEntry("dup", "s1", Waiting, 1, nil))}}}}
		},
		func(cur *Snapshot) Plan {
			return Plan{Units: []Unit{{Key: "dup", Changes: []Change{change(Work, createEntry("dup", "s1", Waiting, 1, nil))}}}}
		},
	})
	assert.Len(t, conflictBatch.Refused, 1)
	assert.Equal(t, "batch", conflictBatch.Refused[0].Key)
}

func TestRecutSelAndBriefSel(t *testing.T) {
	t.Parallel()

	t.Run("BriefSel edits briefs and tiers in one batch and refuses unchanged brief without tier", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 2)
		// selector matching nothing
		p := BriefSel(w.s, BriefSelReq{Sel: Selector{Stream: "no-such-stream"}, Tier: "pro"})
		assert.Len(t, p.Refused, 1)

		// selector with invalid IDs
		p = BriefSel(w.s, BriefSelReq{Sel: Selector{IDs: []string{"no-such-card"}}, Tier: "pro"})
		assert.Len(t, p.Refused, 1)

		// transform that leaves brief unchanged without Tier is refused
		p = BriefSel(w.s, BriefSelReq{
			Sel:  Selector{Stream: "s1", IDs: []string{"s1-1"}},
			Edit: BriefEdit{DropWho: true}, // s1-1 has no WHO: line
		})
		assert.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "leaves the brief of s1-1 as it is")

		// transform that leaves brief unchanged WITH Tier passes over the brief edit and applies tier
		p = BriefSel(w.s, BriefSelReq{
			Sel:  Selector{Stream: "s1", IDs: []string{"s1-1"}},
			Edit: BriefEdit{DropWho: true},
			Tier: "pro",
		})
		w.must(p)
		assert.Equal(t, "pro", w.s.Work.Card("s1-1").F("tier"))

		// transform that modifies brief and sets tier across stream
		p = BriefSel(w.s, BriefSelReq{
			Sel:  Selector{Stream: "s1"},
			Edit: BriefEdit{SetBase: "sprint/v2"},
			Tier: "heavy",
		})
		w.must(p)
		assert.Contains(t, w.s.Work.Card("s1-1").F("brief"), "BASE: sprint/v2")
		assert.Equal(t, "heavy", w.s.Work.Card("s1-2").F("tier"))
	})

	t.Run("RecutSel recuts chained cards in order and refuses unchanged transform without tier", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 2)
		// selector matching nothing
		p := RecutSel(w.s, RecutSelReq{Sel: Selector{Stream: "no-such-stream"}, Tier: "pro"})
		assert.Len(t, p.Refused, 1)

		// selector with invalid IDs
		p = RecutSel(w.s, RecutSelReq{Sel: Selector{IDs: []string{"no-such-card"}}, Tier: "pro"})
		assert.Len(t, p.Refused, 1)

		// transform that leaves brief unchanged with no tier is refused
		p = RecutSel(w.s, RecutSelReq{
			Sel:  Selector{Stream: "s1", IDs: []string{"s1-1"}},
			Edit: BriefEdit{DropWho: true},
		})
		assert.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "its twin would change nothing")

		// RecutSel with Edit and Tier recuts both cards and chains their twins' needs
		w.s.Work.Card("s1-2").Col = Waiting
		w.s.Work.Card("s1-2").Fields["needs"] = "s1-1"
		p = RecutSel(w.s, RecutSelReq{
			Sel:  Selector{Stream: "s1"},
			Edit: BriefEdit{SetBase: "sprint/v2"},
			Tier: "pro",
		})
		w.must(p)
		twin1 := w.s.Work.Placed("s1-1b")
		twin2 := w.s.Work.Placed("s1-2b")
		if assert.NotNil(t, twin1) && assert.NotNil(t, twin2) {
			assert.Contains(t, twin1.F("brief"), "BASE: sprint/v2")
			assert.Equal(t, "s1-1b", twin2.F("needs"), "second twin needs the first twin")
		}
	})
}
