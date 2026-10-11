package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAStreamsDisplayNameIsAFieldOnItsControlCard pins the display name (stream set
// --display, docs/SPEC-SPRINT.md section 11): the field FieldDisplay on the stream's
// control card, written and cleared as the release is, and StreamDisplay, the name the
// stream is shown by: the display when set; else "point-fix-" plus its release exactly
// as stored; else its key (docs/SPEC-SPRINT-DASHBOARD.md, Work). The key is never
// parsed for a version.
func TestAStreamsDisplayNameIsAFieldOnItsControlCard(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Coordinator: "coordinator", Work: NewTable(Work), Merge: NewTable(Merge)}
	s.Work.SetRows([]string{"s1"})
	s.Merge.Put(&Card{ID: CtlID("s1"), Row: "s1", Col: Ctl, Rev: 1, Fields: map[string]string{}})

	apply := func(p Plan) {
		for _, u := range p.Units {
			for _, ch := range u.Changes {
				if ch.Table != Merge {
					continue
				}
				c := s.Merge.Card(ch.Entry.ID)
				for k, v := range ch.Entry.Set {
					c.Fields[k] = v
				}
				for _, k := range ch.Entry.Unset {
					delete(c.Fields, k)
				}
			}
		}
	}

	// stream set s1 --display "bus lock" records FieldDisplay
	p := Set(s, SetReq{Streams: []string{"s1"}, Display: "bus lock", Who: "coordinator"})
	require.Empty(t, p.Refused)
	require.Len(t, p.Units, 1)
	assert.Equal(t, "stream s1 display bus lock", p.Units[0].Moved)
	apply(p)
	assert.Equal(t, "bus lock", s.StreamCtl("s1").F(FieldDisplay))

	// the shown name: the display when set; else point-fix-<release>; else the key
	assert.Equal(t, "bus lock", StreamDisplay("s1", "bus lock", ""))
	assert.Equal(t, "bus lock", StreamDisplay("s1", "bus lock", "v1.2.12"))
	assert.Equal(t, "point-fix-v1.2.12", StreamDisplay("s1", "", "v1.2.12"))
	assert.Equal(t, "s1", StreamDisplay("s1", "", ""))

	// default clears it, the MOVED line reads display none, and the shown name falls back
	p = Set(s, SetReq{Streams: []string{"s1"}, Display: ReadTierDefault, Who: "coordinator"})
	require.Empty(t, p.Refused)
	require.Len(t, p.Units, 1)
	assert.Equal(t, "stream s1 display none", p.Units[0].Moved)
	apply(p)
	assert.Equal(t, "", s.StreamCtl("s1").F(FieldDisplay))
	assert.Equal(t, "s1", StreamDisplay("s1", s.StreamCtl("s1").F(FieldDisplay), s.StreamCtl("s1").F(FieldRelease)))

	// none clears it too
	p = Set(s, SetReq{Streams: []string{"s1"}, Display: "none", Who: "coordinator"})
	require.Empty(t, p.Refused)
	require.Len(t, p.Units, 1)
	assert.Equal(t, "stream s1 display none", p.Units[0].Moved)

	// a name with a newline, or over 60 bytes, is refused naming the bound, and nothing
	// is written
	for _, bad := range []string{"bus\nlock", strings.Repeat("x", DisplayMax+1)} {
		before := s.StreamCtl("s1").F(FieldDisplay)
		p = Set(s, SetReq{Streams: []string{"s1"}, Display: bad, Who: "coordinator"})
		require.Len(t, p.Refused, 1, "%q is refused", bad)
		assert.Contains(t, p.Refused[0].Why, "60 bytes", "the refusal names the bound")
		assert.Empty(t, p.Units, "%q writes nothing", bad)
		assert.Equal(t, before, s.StreamCtl("s1").F(FieldDisplay))
	}
}
