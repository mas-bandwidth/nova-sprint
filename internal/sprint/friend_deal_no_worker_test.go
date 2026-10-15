package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The epoch-21 log has fourteen fleet moves in tick-deal-dm4zgrm3vivs-62591-
// dca9a85a06c92cd5~21-1 at 2026-10-15T00:25:28Z (the brief counted thirteen).
// Preserve all fourteen IDs. Tiers/streams are reduced to a single eligible
// stream; the regression pins the real TickDeal placement, not plan text.
func TestAFriendRowWithNoLiveWorkerIsDealtNothing(t *testing.T) {
	t.Parallel()
	ids := []string{
		"fixed-op-bench-harnessb",
		"netcode-cs-port-block",
		"delta-interpolation-lookback-never-underflows-and-frame-zero-is-a-frame",
		"delta-quantize-and-widen-have-no-undefined-behaviour",
		"serialize-cpp-fuzz-block-decoder-and-model-inputs",
		"fixed3d-issue-26",
		"replicant-interpolation-snaps-enums-and-flags",
		"rocketnet-csharp-binds-the-log-level-getter-and-its-name",
		"reliable-cs-port-block",
		"rocketnet-docs-name-iocp-the-vendored-sodium-and-the-pace-plants",
		"rocketnet-harness-setup-failures-stop-the-run",
		"rocketnet-every-compiled-out-test-is-named-and-counted",
		"rocketnet-address-and-base64-error-arms-match-their-contracts",
		"rocketnet-bench-refusal-paths-release-what-they-hold",
	}
	w := newWorld(t)
	w.s.Now = time.Date(2026, 10, 15, 0, 25, 28, 0, time.UTC)
	w.s.Epoch = 21
	w.s.Coordinator = "rowan"
	w.s.Friends = []FriendSeat{
		{Name: "rowan", Status: Up, Width: 16, Class: "flash,pro", Evidence: "nova-hook proven", WorkerLive: false},
		{Name: "worker", Status: Up, Width: 16, Class: "flash,pro", Evidence: "nova-hook proven", WorkerLive: true},
	}
	var cards []CardAdd
	for _, id := range ids {
		cards = append(cards, CardAdd{ID: id, Brief: friendBrief("friend")})
	}
	w.must(Add(w.s, AddReq{Stream: "replay", Cards: cards}))
	p, _ := TickDeal(w.s, TickReq{Friends: w.s.Friends})
	w.must(p)
	assert.Zero(t, w.s.Fleet.Count(FriendRow("rowan"), Ready)+w.s.Fleet.Count(FriendRow("rowan"), Working), "an up coordinator hook proves no live worker")
	for _, id := range ids {
		wc := w.s.Fleet.Placed(WorkCardID(id, 1))
		require.NotNil(t, wc, "the proven worker must actually receive %s", id)
		assert.Equal(t, FriendRow("worker"), wc.Row, id)
	}
	assert.Empty(t, Check(w.s, nil), "a legitimate worker placement passes check")

	// Inject the forbidden placement, leaving the primary/work relation intact.
	bad := w.s.Fleet.Card(WorkCardID(ids[0], 1))
	w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), FriendRow("rowan")))
	w.place(w.s.Fleet, bad.ID, FriendRow("rowan"), bad.Col)
	bad.Fields["member"] = bad.Row
	violations := Check(w.s, nil)
	assert.Len(t, violations, 1, "only the worker invariant is violated")
	found := false
	for _, v := range violations {
		if strings.Contains(v.Detail, "no live worker") && strings.Contains(v.Detail, bad.ID) && strings.Contains(v.Detail, bad.Row) {
			found = true
		}
	}
	assert.True(t, found, "check must name card %s and row %s: %v", bad.ID, bad.Row, violations)
}

func TestFriendDealRequiresWorkerProofOnEveryRow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		seat FriendSeat
	}{
		{"up without worker", FriendSeat{Name: "ghost", Status: Up, Width: 16, Class: "flash,pro", WorkerLive: false}},
		{"coordinator with worker proof", FriendSeat{Name: "rowan", Status: Up, Width: 16, Class: "flash,pro", WorkerLive: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.s.Coordinator = "rowan"
			w.s.Friends = []FriendSeat{tc.seat, {Name: "worker", Status: Up, Width: 1, Class: "flash,pro", WorkerLive: true}}
			w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: friendBrief("friend")}}}))
			p, _ := TickDeal(w.s, TickReq{Friends: w.s.Friends})
			w.must(p)
			wc := w.s.Fleet.Placed("s1-1.w1")
			require.NotNil(t, wc)
			assert.Equal(t, FriendRow("worker"), wc.Row)
		})
	}
}
