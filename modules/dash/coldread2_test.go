package dash

// Second cold read of the v2 dashboard shim (2026-11-06), head 348442a9.
// Probes by Rowan’s second cold reader.

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// v1 partitions a stream's rounded-up total into tier cents (internal/sprint/cost_view.go
// tierCostCents), so the tier cells and the legend add up to the stream's cost. v2 sends raw
// per-tier dollars and the page rounds each tier up on its own: three tiers of 0.1 cent each
// show 1 cent each beside a 1-cent total.
func TestColdread2DefectTierCentsNotPartitioned(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p": {ID: "p", Kind: "producer", Stream: "s", State: "working", Tier: "pro"},
		"a": {ID: "a", Kind: "work", Of: "p", Stream: "s", Tier: "flash", Cost: 1000, Priced: true},
		"b": {ID: "b", Kind: "work", Of: "p", Stream: "s", Tier: "pro", Cost: 1000, Priced: true},
		"c": {ID: "c", Kind: "work", Of: "p", Stream: "s", Tier: "heavy", Cost: 1000, Priced: true},
	}}
	w := mapSnapshot(s, testRoster, time.Time{}, nil).Tables["work"].(map[string]*workRow)["s"]
	total, tiers := w.Cost.Int64(), int64(0)
	for _, d := range w.ByTier {
		tiers += d.Int64()
	}
	if tiers != total {
		t.Errorf("tier cells sum to %d cents, stream cost %d cents; v1 partitions the total (cost_view.go tierCostCents)", tiers, total)
	}
}

// The bar's health is judged on the viewer's clock against the server's time (app.js accept:
// Date.now() - Date.parse(j.fetchedAt) < 2000; poll: Date.now() - shownAt >= 2000, shownAt is
// the server's d.at). A viewer two seconds ahead of the server never sees the bar pulse; one
// behind sees it pulse on stale data. The server already sends dataAgeSeconds, which is skew-free.
func TestColdread2DefectPulseUsesViewerClock(t *testing.T) {
	t.Parallel()
	js, err := page.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)
	for _, bad := range []string{"Date.now() - Date.parse(j.fetchedAt)", "Date.now() - shownAt"} {
		if strings.Contains(src, bad) {
			t.Errorf("app.js compares the viewer clock with server time: %q", bad)
		}
	}
	if !strings.Contains(src, "dataAgeSeconds") {
		t.Error("app.js never reads the server's dataAgeSeconds")
	}
}
