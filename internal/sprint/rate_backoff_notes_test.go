package sprint

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// withdrawalWorld is a world with one pro route and three ready work cards dealt on it:
// restWithdrawals withdraws all three the tick a rest holds the route (route_rest.go).
func withdrawalWorld(t *testing.T) *world {
	t.Helper()
	w := setup(t, 3)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 3}}))
	require.Len(t, w.s.Fleet.Column(Ready), 3, "three ready work cards dealt on pro-a")
	for _, c := range w.s.Fleet.Column(Ready) {
		require.Equal(t, "pro-a", c.F(FieldRoute), "the deal drew pro-a")
	}
	return w
}

// planNotes is every note a plan carries: its own and each unit's, the stream's.
func planNotes(p Plan) []Note {
	out := append([]Note(nil), p.Notes...)
	for _, u := range p.Units {
		out = append(out, u.Notes...)
	}
	return out
}

// withdrawnIDs is the ids of the units whose Moved words say the card was taken back
// from its resting route.
func withdrawnIDs(p Plan) []string {
	var out []string
	for _, u := range p.Units {
		if strings.Contains(u.Moved, "taken back: its route") {
			out = append(out, u.Key)
		}
	}
	return out
}

// A rate-limit backoff already notes itself once per new end, to the coordinator
// (restWrites; tla/RateBackoff.tla), and it is never a judgment. Withdrawing the ready
// cards it holds must not write a note per card: one backoff on a route with N ready
// cards would write its one note plus N NRestWithdrawn notes, drowning it. The withdrawal
// itself and its Moved words are unchanged, and no redeal is spent (docs/SPEC-SPRINT.md
// section 5, the deal: a card dealt before its route rests is taken back, not failed).
func TestABackoffWithdrawalWritesNoNotePerCard(t *testing.T) {
	t.Parallel()
	w := withdrawalWorld(t)
	w.s.Fleet.Put(&Card{ID: "r9", Row: "m1", Col: Withdrawn, Fields: map[string]string{
		FieldRoute: "pro-a",
		FieldRateTake + "1": ProviderTake{Route: "pro-a", Finished: stamp(w.s.Now),
			Error: "provider: class=rate-limited status=429 msg=Too many requests"}.String(),
	}})

	p, _ := TickDeal(w.s, TickReq{})
	got := withdrawnIDs(p)
	require.Len(t, got, 3, "the tick withdraws every ready card on the backing-off route: %v", got)
	for _, u := range p.Units {
		if strings.Contains(u.Moved, "taken back: its route") {
			assert.Contains(t, u.Moved, "rests (rate-limit backoff", "%s", u.Moved)
		}
	}
	w.do(p)
	for _, id := range []string{"s1-1", "s1-2", "s1-3"} {
		assert.Equal(t, Ready, w.state(id), "%s: its primary is back to ready", id)
	}

	var backs, withdrawn int
	for _, n := range planNotes(p) {
		switch {
		case n.Type == NRestWithdrawn:
			withdrawn++
		case n.Type == NRouteRested && strings.Contains(n.What, "rate-limit backoff"):
			backs++
		}
	}
	assert.Equal(t, 0, withdrawn, "a backoff's withdrawal writes no note per card")
	assert.Equal(t, 1, backs, "the plan carries exactly the backoff's one note (restWrites)")
}

// A route resting for its 5xxs is a judgment of the owner's, not the backoff's: each
// withdrawn card still notes why, one NRestWithdrawn note per card, as before this card.
func TestAFiveHundredsRestWithdrawalStillNotesEachCard(t *testing.T) {
	t.Parallel()
	w := withdrawalWorld(t)
	for i := range 3 {
		w.s.Fleet.Put(&Card{ID: fmt.Sprintf("x%d", i), Row: "m1", Col: Withdrawn, Fields: map[string]string{
			FieldRoute: "pro-a",
			FieldProviderTake + "1": ProviderTake{Route: "pro-a", Finished: stamp(w.s.Now.Add(time.Duration(i-3) * time.Second)),
				Error: "provider: class=provider-5xx status=503 msg=upstream unavailable"}.String(),
		}})
	}

	p, _ := TickDeal(w.s, TickReq{})
	require.Len(t, withdrawnIDs(p), 3, "the tick withdraws every ready card on the resting route")
	var withdrawn, rested int
	for _, n := range planNotes(p) {
		switch {
		case n.Type == NRestWithdrawn:
			withdrawn++
		case n.Type == NRouteRested && !strings.Contains(n.What, "rate-limit backoff"):
			rested++
		}
	}
	assert.Equal(t, 3, withdrawn, "one NRestWithdrawn note per withdrawn card")
	assert.Equal(t, 1, rested, "the 5xx rest's own note")
}
