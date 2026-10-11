package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// add admits a card based on dev only into the promotion stream, the stream the
// coordinator's `stream set --land-protected` marked (docs/SPEC-SPRINT.md section 7, the
// sprint branch): in any other stream, a new one included, the card is refused naming
// the sprint branch, every card of the add alike; a card on the sprint branch, or naming
// no BASE, is admitted anywhere; `--land-protected default` makes the stream ordinary again.
func TestAddAdmitsADevCardOnlyIntoThePromotionStream(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, stream, base string
		marks              []string
		refused            bool
	}{
		{"dev in a stream", "s1", DevBranch, nil, true},
		{"dev in a new stream", "s9", DevBranch, nil, true},
		{"dev in the promotion stream", "s1", DevBranch, []string{LandProtectedAny}, false},
		{"dev in a stream marked for a repository", "s1", DevBranch, []string{"mas-bandwidth/nova-tools"}, false},
		{"dev in a stream whose mark was taken off", "s1", DevBranch, []string{LandProtectedAny, ReadTierDefault}, true},
		{"the sprint branch", "s1", "sprint/mechanical-2026-10-02", nil, false},
		{"no BASE line", "s1", "", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := streamsWorld(t, "s1")
			for _, m := range c.marks {
				w.must(Set(w.s, SetReq{Streams: []string{"s1"}, LandProtected: m, Who: "coordinator"}))
			}
			require.Equal(t, len(c.marks) > 0 && c.marks[len(c.marks)-1] != ReadTierDefault, IsPromotionStream(w.s, "s1"), "the mark is what stream set wrote")
			for _, r := range []AddReq{
				{Stream: c.stream, IDs: []string{"x1", "x2"}, Base: c.base, Who: "coordinator"},
				{Stream: c.stream, Cards: []CardAdd{{ID: "x1", Base: c.base}, {ID: "x2", Base: c.base}}, Who: "coordinator"},
			} {
				p := Add(w.s, r)
				if !c.refused {
					assert.Empty(t, p.Refused)
					continue
				}
				require.Len(t, p.Refused, 2, "every card of the add: %v", p.Refused)
				assert.Contains(t, p.Refused[0].Why, "card x1 is cut on dev, and stream "+c.stream+" is not the promotion stream")
				assert.Contains(t, p.Refused[0].Why, "BASE: <the sprint branch>")
				assert.Contains(t, p.Refused[0].Why, "run: nova-sprint stream set "+c.stream+" --land-protected <owner/name,...|any>")
			}
		})
	}
}

// Once the sprint's base is recorded (set --base, PropSprintBase), add admits a card only
// when its BASE is that branch or names none, outside the promotion stream: a card cut on
// a side branch, a personal one or dev is refused naming the card, its BASE, the sprint's
// base and the remedy, every card of the add alike; the promotion stream admits any base
// (docs/SPEC-SPRINT.md section 7, the sprint branch). Found 2026-10-04: cards cut on a
// temporary branch and on personal ones landed there and were folded back by hand.
func TestAddAdmitsOnlyTheSprintBaseOutsideThePromotionStream(t *testing.T) {
	t.Parallel()
	const base = "sprint/one"
	for _, c := range []struct {
		name, stream, card string
		promotion, refused bool
	}{
		{"a side branch", "s1", "sprint/mechanical-2026-10-02", false, true},
		{"a personal branch", "s1", "rowan/friend-health", false, true},
		{"dev", "s1", DevBranch, false, true},
		{"a side branch in a new stream", "s9", "stella/friend-activity-followup", false, true},
		{"the sprint base", "s1", base, false, false},
		{"no BASE line", "s1", "", false, false},
		{"dev in the promotion stream", "s1", DevBranch, true, false},
		{"a side branch in the promotion stream", "s1", "rowan/friend-health", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := streamsWorld(t, "s1")
			w.must(WithSprintBase(Set(w.s, SetReq{Who: "coordinator"}), w.s, base))
			require.Equal(t, base, SprintBase(w.s), "set --base records the sprint's base")
			if c.promotion {
				w.must(Set(w.s, SetReq{Streams: []string{"s1"}, LandProtected: LandProtectedAny, Who: "coordinator"}))
			}
			for _, r := range []AddReq{
				{Stream: c.stream, IDs: []string{"x1", "x2"}, Base: c.card, Who: "coordinator"},
				{Stream: c.stream, Cards: []CardAdd{{ID: "x1", Base: c.card}, {ID: "x2", Base: base}}, Who: "coordinator"},
			} {
				p := Add(w.s, r)
				if !c.refused {
					assert.Empty(t, p.Refused)
					continue
				}
				require.NotEmpty(t, p.Refused, "a card off the sprint's base is refused")
				assert.Equal(t, "x1", p.Refused[0].Key)
				assert.Contains(t, p.Refused[0].Why, "card x1 is cut on "+c.card+", not the sprint's base "+base+", and stream "+c.stream+" is not the promotion stream")
				assert.Contains(t, p.Refused[0].Why, "nothing was written; re-cut the card with BASE: "+base)
				assert.Contains(t, p.Refused[0].Why, "run: nova-sprint stream set "+c.stream+" --land-protected <owner/name,...|any>")
			}
		})
	}
}

// The sprint's base is a branch name the coordinator records: dev and main are refused
// (promotion alone reaches them, and a stream that lands there is marked), as is a word
// that is no branch name; default takes it off, and add holds dev alone again.
func TestSetBaseRecordsTheSprintBase(t *testing.T) {
	t.Parallel()
	w := streamsWorld(t, "s1")
	assert.Equal(t, "", SprintBase(w.s), "no base recorded")
	for _, bad := range []string{DevBranch, "main", "-x", "a b", "sprint/one@0123"} {
		p := WithSprintBase(Set(w.s, SetReq{Who: "coordinator"}), w.s, bad)
		require.Len(t, p.Refused, 1, bad)
		assert.Contains(t, p.Refused[0].Why, "--base wants the sprint's base branch", bad)
	}
	p := WithSprintBase(Set(w.s, SetReq{Who: "reader-a"}), w.s, "sprint/one")
	assert.NotEmpty(t, p.Refused, "the coordinator's alone")
	w.must(WithSprintBase(Set(w.s, SetReq{Who: "coordinator"}), w.s, "sprint/one"))
	assert.Equal(t, "sprint/one", SprintBase(w.s))
	w.must(WithSprintBase(Set(w.s, SetReq{Who: "coordinator"}), w.s, ReadTierDefault))
	assert.Equal(t, "", SprintBase(w.s), "default takes it off")
	assert.Empty(t, Add(w.s, AddReq{Stream: "s1", IDs: []string{"x1"}, Base: "rowan/friend-health", Who: "coordinator"}).Refused, "with no base recorded, dev alone is held")
}

// brief holds a new BASE to the rule add holds it to: a brief that moves a card off the
// sprint's base outside the promotion stream is refused, every card of the call alike;
// a brief that keeps its BASE, or moves onto the sprint's base, is taken.
func TestBriefHoldsANewBaseToTheSprintBase(t *testing.T) {
	t.Parallel()
	w := streamsWorld(t, "s1")
	w.must(WithSprintBase(Set(w.s, SetReq{Who: "coordinator"}), w.s, "sprint/one"))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"x1", "x2"}, Base: "sprint/one", Who: "coordinator"}))
	baseOf := map[string]string{"old": "sprint/one", "side": "rowan/friend-health", "kept": "sprint/one"}
	req := BriefReq{Cards: []BriefCard{{ID: "x1", Brief: "side"}, {ID: "x2", Brief: "kept"}}, Who: "coordinator"}
	p := WithBriefBases(Brief(w.s, req), w.s, req, func(b string) string { return baseOf[b] })
	require.Len(t, p.Refused, 1)
	assert.Equal(t, "x1", p.Refused[0].Key)
	assert.Contains(t, p.Refused[0].Why, "card x1 is cut on rowan/friend-health, not the sprint's base sprint/one, and stream s1 is not the promotion stream")
	req.Cards = req.Cards[1:]
	assert.Empty(t, WithBriefBases(Brief(w.s, req), w.s, req, func(b string) string { return baseOf[b] }).Refused)
}
