package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// A friend with no free lane is not dealt work while a machine that serves the card's
// tier has one (docs/SPEC-SPRINT.md section 1, friend-deal-idle-lanes-first.w1). On
// 2026-10-12 five heavy cards were dealt to a friend whose lanes were all taken and sat
// dealt-but-untaken while the fleet ran at 15 of 140 slots: the friend deal filled her
// room even when every one of her lanes was busy and a capable machine was idle. The
// deal now leaves such a card to the machines, which start it at once.
func TestFriendWithNoFreeLaneIsNotDealt(t *testing.T) {
	t.Parallel()
	// amy at width 2 fills both lanes: two work cards working on her row, no free lane,
	// and no card behind them
	w := friendWorld(t, friendBrief("friend amy"), friendBrief("friend amy"))
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	dealStarted(w, amy)
	require.Equal(t, 2, w.s.Fleet.Count(FriendRow("amy"), Working), "both lanes work")
	require.Zero(t, workOn(w, FriendRow("amy"))-w.s.Fleet.Count(FriendRow("amy"), Working), "nothing waits behind them")

	// a machine up that draws flash: m1 and m2 are up at friendWorld, and a flash route
	// is enabled, so a machine can start the card at once
	w.s.Routes = []Route{{Name: "small", Tier: cardhdr.RouteFlash, Provider: "p", Model: "small", Enabled: true}}
	w.admit(AddReq{Stream: "s2", Cards: []CardAdd{{ID: "s2-1", Brief: friendBrief("friend")}}})

	dealWith(w, amy)
	wc := w.s.Fleet.Card(WorkCardID("s2-1", 1))
	require.NotNil(t, wc, "the card is dealt")
	assert.NotEqual(t, FriendRow("amy"), wc.Row,
		"a friend with no free lane is not dealt work while a machine that serves its tier has one")
	assert.Contains(t, w.s.Members(), wc.Row, "the machine takes it")
	assert.Empty(t, Check(w.s, nil))
}
