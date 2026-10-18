package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-10-18: add records a brief's LAND-AFTER: line on the card (FieldLandAfter and
// FieldLandAfterSet, WithAddSoftDeps), and a brief edit records the needs its DEPENDS-ON:
// line names but never the LAND-AFTER: line, so an edited LAND-AFTER was ignored: the
// stored land_after stood (SoftDepsRaw reads it first) and the card landed in the old order.
// A brief edit writes the soft dependencies of the new text the way add does, and a text
// that drops the line clears them.
func TestABriefEditWritesItsLandAfterLikeAddDoes(t *testing.T) {
	t.Parallel()
	world := func(t *testing.T, set bool, stored string) *world {
		w := newWorld(t, "reader-a", "reader-b")
		w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "one", Who: "coordinator"}))
		w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Brief: "two", Who: "coordinator"}))
		w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"b"}, Brief: "tier: flash\nLAND-AFTER: a-1\n\nthe task", Who: "coordinator"}))
		if set { // what add wrote for the admission text ("" when the text named none)
			if stored != "" {
				w.s.Work.Placed("b").Fields[FieldLandAfter] = stored
			}
			w.s.Work.Placed("b").Fields[FieldLandAfterSet] = "true"
		}
		return w
	}
	for _, c := range []struct {
		name, stored, brief string
		want                []string
	}{
		{"ChangedToAnotherCard", "a-1", "tier: flash\nLAND-AFTER: a-2\n\nthe task", []string{"a-2"}},
		{"DroppedClearsThem", "a-1", "tier: flash\n\nthe task", nil},
		{"AddedWhereAddWroteNone", "", "tier: flash\nLAND-AFTER: a-1,a-2\n\nthe task, reworded", []string{"a-1", "a-2"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := world(t, true, c.stored)
			w.must(Brief(w.s, briefOf("b", c.brief, "")))
			assert.Equal(t, c.want, SoftDepsRaw(w.s.Work.Placed("b")), "the soft dependencies the card holds after the edit")
		})
	}
}

// 2026-10-18: a brief edit validates its LAND-AFTER list against the card's old
// needs, not the needs the edit writes (steps_edit.go briefLandAfterSet). Turning
// a need into a land-after — DEPENDS-ON: none, LAND-AFTER: a-1 on a card that
// needed a-1 — was refused as "one relation, not two" against the stale need.
func TestABriefEditTurningANeedIntoLandAfterIsTaken(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "one", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"b"}, Needs: []string{"a-1"}, Brief: "tier: flash\nDEPENDS-ON: a-1\n\nthe task", Who: "coordinator"}))
	require.Equal(t, "a-1", w.s.Work.Placed("b").F("needs"))
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "b", Brief: "tier: flash\nLAND-AFTER: a-1\n\nthe task"}}, NeedsFromBrief: true, Who: "coordinator"})
	w.must(p)
	assert.Empty(t, w.s.Work.Placed("b").F("needs"), "the need is turned into a land-after, not kept")
	assert.Equal(t, []string{"a-1"}, SoftDepsRaw(w.s.Work.Placed("b")), "the land-after the edit writes")
}

// An edit naming the same id on the DEPENDS-ON and LAND-AFTER lines is judged
// against the needs it writes, so the one-relation rule holds: prior to the fix
// the check ran against the card's old needs and let the broken write through.
func TestABriefEditNamingOneIdOnBothLinesIsRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "one", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"b"}, Brief: "tier: flash\n\nthe task", Who: "coordinator"}))
	p := Brief(w.s, briefOf("b", "tier: flash\nDEPENDS-ON: a-1\nLAND-AFTER: a-1\n\nthe task", "a-1"))
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "one relation, not two")
	assert.Empty(t, w.s.Work.Placed("b").F("needs"), "nothing was written")
	assert.Empty(t, SoftDepsRaw(w.s.Work.Placed("b")), "nothing was written")
}
