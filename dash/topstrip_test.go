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

func at(base time.Time, m ...int) []time.Time {
	var out []time.Time
	for _, x := range m {
		out = append(out, base.Add(time.Duration(x)*time.Minute))
	}
	return out
}

// The ETA reads the store's landings, not the process's samples: a cache with
// no samples at all (a restart) still has its rate.
func TestEtaSurvivesARestart(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := base
	c := &Cache{now: func() time.Time { return now }, landingsOK: true, landings: at(base, -50, -30, -20, -5)} // 4 in the hour
	c.view = envelope{Landed: 4, All: 44, Left: 40, At: base}
	d := c.pageSnapshot().Data
	if d.Eta != "10h" && d.Eta != "10h0m" || d.EtaLandings != 4 || d.EtaWindow != 60 || d.EtaBasis != "last hour" || !strings.Contains(d.Summary, "-> ETA 10h0m") {
		t.Fatalf("eta=%q landings=%d window=%v basis=%q summary=%q", d.Eta, d.EtaLandings, d.EtaWindow, d.EtaBasis, d.Summary)
	}
}

func TestEtaFallsBackToTheSprintAverage(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// 6 landings over the 6 hours since the first, 2 of them in the last hour: under the floor
	ls := at(base, -360, -300, -240, -180, -50, -10)
	rate, window, n, avg := landingRate(ls, base)
	if !avg || rate != 1 || window != 360 || n != 6 {
		t.Fatalf("rate=%v window=%v n=%d avg=%v", rate, window, n, avg)
	}
	c := &Cache{now: func() time.Time { return base }, landingsOK: true, landings: ls}
	c.view = envelope{Left: 12, All: 18, Landed: 6, At: base}
	d := c.pageSnapshot().Data
	if d.Eta != "12h0m" || d.EtaBasis != "sprint average" {
		t.Fatalf("eta=%q basis=%q", d.Eta, d.EtaBasis)
	}
	// before the third landing of the sprint: no ETA
	c.landings = at(base, -30, -10)
	c.view = envelope{Left: 12, All: 14, At: base}
	if d := c.pageSnapshot().Data; d.Eta != "-" {
		t.Fatalf("two landings give %q", d.Eta)
	}
	// a store that cannot give landings: no ETA, never a guess
	c = &Cache{now: func() time.Time { return base }}
	c.view = envelope{Left: 12, All: 14, At: base}
	if d := c.pageSnapshot().Data; d.Eta != "-" {
		t.Fatalf("unread landings give %q", d.Eta)
	}
}

func TestLandedInCountsProducersBecomingLanded(t *testing.T) {
	pre := store.Card{ID: "a", Kind: "producer", State: "merging"}
	post := store.Card{ID: "a", Kind: "producer", State: "landed"}
	imp := store.Card{ID: "i", Kind: "producer", State: "landed", Reason: "its producer landed in v1"}
	wk := store.Card{ID: "a.w", Kind: "work", State: "landed"}
	e := store.Event{Accepted: true, Before: &store.State{Cards: map[string]store.Card{"a": pre}}, After: &store.State{Cards: map[string]store.Card{"a": post, "i": imp, "a.w": wk}}}
	if got := store.LandedIn(e); got != 1 {
		t.Fatalf("landed %d", got)
	}
	e.Before.Cards["a"] = post // already landed before: not a landing
	if got := store.LandedIn(e); got != 0 {
		t.Fatalf("relanded %d", got)
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
	if c.Total != "$1.00" || c.PerCard != "$0.50" || c.Landed != 2 || c.PricedCards != 2 || c.Priced != 3 || c.Unpriced != 1 || c.ByTier["flash"] != "$0.40" || c.ByTier["pro"] != "$0.60" {
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

// Landed cards no run of which carried a cost are not in the per-card divisor.
func TestPerCardDividesByPricedLandedCards(t *testing.T) {
	s := store.Snapshot{Cards: map[string]store.Card{
		"p1":   {ID: "p1", Kind: "producer", State: "landed"},
		"p2":   {ID: "p2", Kind: "producer", State: "landed"}, // landed, its runs unpriced
		"p3":   {ID: "p3", Kind: "producer", State: "landed"},
		"p4":   {ID: "p4", Kind: "producer", State: "landed", Reason: "its producer landed in v1"},
		"p1.w": {ID: "p1.w", Kind: "work", Of: "p1", State: "done", Tier: "pro", Cost: 3000000, Priced: true},
		"p3.w": {ID: "p3.w", Kind: "work", Of: "p3", State: "done", Tier: "pro", Cost: 1000000, Priced: true},
		"p3.r": {ID: "p3.r", Kind: "read", Of: "p3", State: "done", Tier: "pro", Cost: 2000000, Priced: true},
		"p2.w": {ID: "p2.w", Kind: "work", Of: "p2", State: "done", Tier: "pro"},
		"p2.r": {ID: "p2.r", Kind: "read", Of: "p2", State: "done", Tier: "pro"},
	}}
	c := summarizeCost(s)
	if c.Total != "$6.00" || c.Landed != 3 || c.PricedCards != 2 || c.PerCard != "$3.00" || c.Unpriced != 2 {
		t.Fatalf("%+v", c)
	}
}
