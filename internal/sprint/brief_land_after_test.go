package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
