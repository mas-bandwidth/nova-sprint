package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// add admits a card whose BASE is not the sprint's base (dev, main, a temporary
// integration branch, a personal or a dead one) only into the promotion stream, the
// stream the coordinator's `stream set --land-protected` marked (docs/SPEC-SPRINT.md
// section 7, the sprint branch): in any other stream, a new one included, the card is
// refused naming its BASE and the sprint's base, every card of the add alike; a card on
// the sprint branch, or naming no BASE, is admitted anywhere; `--land-protected default`
// makes the stream ordinary again.
func TestAddAdmitsACardOffTheSprintBaseOnlyIntoThePromotionStream(t *testing.T) {
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
		{"main in a stream", "s1", "main", nil, true},
		{"a temporary integration branch", "s1", "rowan/integration-2026-10-04", nil, true},
		{"a personal branch", "s1", "rowan/friend-health", nil, true},
		{"a bare sprint/ prefix", "s1", "sprint/", nil, true},
		{"a side branch in the promotion stream", "s1", "stella/friend-activity-followup", []string{LandProtectedAny}, false},
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
				assert.Contains(t, p.Refused[0].Why, "card x1 is cut on "+c.base+", and stream "+c.stream+" is not the promotion stream")
				assert.Contains(t, p.Refused[0].Why, c.base+" is not the sprint's base, a sprint branch sprint/<name>")
				assert.Contains(t, p.Refused[0].Why, "BASE: <the sprint base>")
				assert.Contains(t, p.Refused[0].Why, "run: nova-sprint stream set "+c.stream+" --land-protected <owner/name,...|any>")
			}
		})
	}
}
