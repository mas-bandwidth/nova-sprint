package dash

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestEtaFollowsV1(t *testing.T) {
	cases := []struct {
		left     int
		rate     float64
		landings int
		want     string
	}{
		{10, 30, 5, "20m"},    // 10 cards at 30/h
		{90, 60, 5, "1h30m"},  // 90 min
		{1, 7, 3, "9m"},       // rounded up
		{1000, 20, 9, "2d2h"}, // 3000 min = 50h, rounded up: 2d2h
		{10, 30, 2, "-"},      // under the landing floor
		{10, 0, 9, "-"},       // no rate
		{0, 30, 9, "-"},       // nothing left
	}
	for _, c := range cases {
		if got := etaString(etaMinutes(c.left, c.rate, c.landings)); got != c.want {
			t.Errorf("left=%d rate=%v landings=%d: eta %q want %q", c.left, c.rate, c.landings, got, c.want)
		}
	}
}

func TestEtaFromCacheSamples(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base
	c := &Cache{now: func() time.Time { return now }}
	c.view = envelope{Landed: 0, All: 40, Left: 40, At: base}
	c.addSample(base, 0)
	now = base.Add(30 * time.Minute)
	c.view.Landed, c.view.Left = 10, 30
	c.addSample(now, 10) // 20 per hour over 30 minutes
	d := c.pageSnapshot().Data.(envelope)
	if d.Eta != "1h30m" || d.EtaLandings != 10 || d.EtaWindow != 30 || !strings.Contains(d.Summary, "10/40 25.0% -> ETA 1h30m") {
		t.Fatalf("eta=%q landings=%d window=%v summary=%q", d.Eta, d.EtaLandings, d.EtaWindow, d.Summary)
	}
}

func TestCoordinatorName(t *testing.T) {
	rows := map[string]store.Row{"w": {Name: "w", Role: "worker"}, "rowan": {Name: "rowan", Role: "coordinator"}, "zed": {Name: "zed", Role: "coordinator"}}
	if got := coordinatorOf(rows); got != "rowan" {
		t.Fatalf("coordinator %q", got)
	}
	if got := coordinatorOf(map[string]store.Row{"w": {Role: "worker"}}); got != "" {
		t.Fatalf("no coordinator is blank, got %q", got)
	}
	d := mapSnapshot(store.Snapshot{Rows: rows}, testRoster, time.Time{})
	if d.Coordinator != "rowan" {
		t.Fatalf("envelope coordinator %q", d.Coordinator)
	}
}

func TestVersionRidesTheEnvelope(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "v2-stitch+abc123def456"
	if d := mapSnapshot(store.Snapshot{}, testRoster, time.Time{}); d.ServerVersion != "v2-stitch+abc123def456" {
		t.Fatalf("version %q", d.ServerVersion)
	}
}

func TestCostSummaryPricedAndUnpriced(t *testing.T) {
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p1":   {ID: "p1", Kind: "producer", Stream: "s", State: "landed", Tier: "flash"},
		"p2":   {ID: "p2", Kind: "producer", Stream: "s", State: "landed", Tier: "pro"},
		"old":  {ID: "old", Kind: "producer", Stream: "s", State: "landed", Reason: "its producer landed in v1"},
		"p1.w": {ID: "p1.w", Kind: "work", Of: "p1", Stream: "s", State: "done", Tier: "flash", Cost: 300000, Priced: true},
		"p1.r": {ID: "p1.r", Kind: "read", Of: "p1", Stream: "s", State: "done", Tier: "flash", Cost: 100000, Priced: true},
		"p2.w": {ID: "p2.w", Kind: "work", Of: "p2", Stream: "s", State: "done", Tier: "pro", Cost: 600000, Priced: true},
		"p2.r": {ID: "p2.r", Kind: "read", Of: "p2", Stream: "s", State: "done", Tier: "pro"},    // finished, no cost: unpriced
		"p3.w": {ID: "p3.w", Kind: "work", Of: "p3", Stream: "s", State: "working", Tier: "pro"}, // not finished: not counted unpriced
	}}
	c := summarizeCost(s)
	if c.Total != "$1.00" || c.PerCard != "$0.50" || c.Landed != 2 || c.Priced != 3 || c.Unpriced != 1 || c.ByTier["flash"] != "$0.40" || c.ByTier["pro"] != "$0.60" {
		t.Fatalf("%+v", c)
	}
	d := mapSnapshot(s, testRoster, time.Time{})
	if got := d.StreamCosts["s"].amounts["read_cost"].Int64(); got != 100000 {
		t.Fatalf("read consumers' cost reaches the stream's read_cost: %d", got)
	}
}

func TestCostSummaryNothingPricedIsDashNotZero(t *testing.T) {
	s := store.Snapshot{Cards: map[string]store.Card{
		"p":   {ID: "p", Kind: "producer", State: "landed"},
		"p.w": {ID: "p.w", Kind: "work", Of: "p", State: "done", Tier: "pro"},
	}}
	c := summarizeCost(s)
	if c.Total != "-" || c.PerCard != "-" || c.Unpriced != 1 || len(c.ByTier) != 0 {
		t.Fatalf("%+v", c)
	}
}
