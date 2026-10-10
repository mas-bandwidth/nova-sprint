package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The bounce-back through the store's tick (sprint bounce.go, tla/DealFill.tla): a member
// that beats and never takes holds the cards dealt to it no longer than the take bound
// (default 5m). The tick returns them, deals them to the live member in the same tick, and
// sets the silent one aside with one judgment; no coordinator verb moves anything.
func TestCardsAMemberNeverTakesBounceToALiveMemberInTheTick(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Width: 2}))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Member: "m2", Width: 2}))
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s1", Count: 4}))
	h.startMachine()
	h.machine()
	silent := h.snap().Fleet.Cell("m1", sprint.Ready)
	require.NotEmpty(t, silent, "the deal gave m1 cards")
	var ids []string
	for _, c := range silent {
		ids = append(ids, c.F("primary"))
	}
	h.tick(time.Minute)
	h.work("m2") // the live member takes and finishes its own
	h.tick(5 * time.Minute)
	h.machine()
	s := h.snap()
	assert.Empty(t, s.Fleet.Cell("m1", sprint.Ready), "past the take bound, never taken: returned")
	for _, id := range ids {
		wc := s.Fleet.Card(s.Primary(id).F("work"))
		require.NotNil(t, wc, "%s dealt again", id)
		assert.Equal(t, "m2", wc.Row, "%s dealt to the live member in the same tick", id)
	}
	h.work("m2")
	for _, id := range ids {
		assert.NotEqual(t, sprint.Working, h.state(id), "%s worked by the live member", id)
	}
	var aside int
	for _, o := range h.snap().Open {
		if o.Note.Type == "member aside" && o.Subject() == sprint.StreamSubject(sprint.MemberSubject("m1")) {
			aside++
		}
	}
	assert.Equal(t, 1, aside, "m1 set aside: one judgment, pushed")
	h.clean("bounced")
}
