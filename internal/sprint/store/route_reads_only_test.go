package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAReadsOnlyRouteTakesReadsAndNeverWork: set --read-routes reserves pro-um and
// heavy-um for reads (v19-route-reads-only, on the v1.6 line). The work deal never draws
// a reads-only route, even one named in the tier's shared array, and the two reads of a
// pro card in review are both drawn on pro-um. A name the store holds no route of is
// refused, and default takes the reservation off. tla/RouteIndex.tla
// WorkNeverOnReadsOnly, ReadTakesReadsOnly.
func TestAReadsOnlyRouteTakesReadsAndNeverWork(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-a", "pro"), route("pro-b", "pro"), route("pro-um", "pro"), route("heavy-um", "heavy"))
	// pro's shared array names pro-um too: the reservation, not the array, keeps work off it
	h.m.SetTiers(map[string][]string{"pro": {"pro-a", "pro-um", "pro-b"}})
	require.NoError(t, h.st.BeatReaders(h.ctx))

	res := h.run(SetStep(sprint.SetReq{ReadRoutes: "pro-um,nope", Who: h.st.Actor}))
	require.NotEmpty(t, res.Refused, "a name with no route is refused")
	h.must(SetStep(sprint.SetReq{ReadRoutes: "pro-um,heavy-um", Who: h.st.Actor}))

	h.addReady("s1", 4, briefOf("pro", ""))
	for i := 1; i <= 4; i++ {
		h.setPrimary("s1-"+string(rune('0'+i)), map[string]string{sprint.FieldTierNow: "pro"})
	}
	h.addReady("s2", 1, briefOf("heavy", ""))
	h.setPrimary("s2-1", map[string]string{sprint.FieldTierNow: "heavy"})
	h.startMachine()
	h.machine()
	work := h.workCards()
	for i := 1; i <= 4; i++ {
		id := "s1-" + string(rune('0'+i)) + ".w1"
		require.Contains(t, work, id)
		got := work[id].F(sprint.FieldRoute)
		assert.Contains(t, []string{"pro-a", "pro-b"}, got, "%s is dealt on a shared route, never the reads-only pro-um", id)
	}
	_, heavyDealt := work["s2-1.w1"]
	assert.False(t, heavyDealt, "a heavy card whose tier has only a reads-only route is not dealt")

	h.work("m1")
	h.work("m2")
	h.machine()
	s := h.snap()
	reads := s.Readers.Of("s1-1")
	require.Len(t, reads, 2, "a pro card is read twice")
	for _, rc := range reads {
		assert.Equal(t, "pro-um", rc.F(sprint.FieldRoute), "%s, a read of a pro card, takes the reads-only pro route", rc.ID)
	}

	h.must(SetStep(sprint.SetReq{ReadRoutes: sprint.ReadTierDefault, Who: h.st.Actor}))
	s = h.snap()
	v, _ := s.Work.Prop(sprint.PropReadRoutes)
	assert.Equal(t, sprint.ReadTierDefault, v, "default takes the reservation off")
}
