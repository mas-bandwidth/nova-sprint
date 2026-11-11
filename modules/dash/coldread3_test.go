package dash

// Third cold read of the v2 dashboard shim (2026-11-06), head be777a82.
// Adapted from Rowan’s cold reader.
// The other tests are checks that passed or measurements (t.Log only).

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// cr3WorkGrid is the Work grid's minimum width and its row's content
// width, in px, at viewport width vw, from page/index.html's own rules.
func cr3WorkGrid(vw float64) (grid, content float64) {
	const rem = 16.0
	clamp := func(lo, v, hi float64) float64 { return max(lo, min(v, hi)) }
	e := clamp(18.5*rem, 0.45*vw, 42*rem)               // .panel { --E: clamp(18.5rem, 45vw, 42rem) }
	gap := clamp(1.5*rem, 0.10*vw-10.5625*rem, 2.5*rem) // .streams .row --gap
	pad, s := 2*rem, 9.75*rem                           // .streams .row --pad, --S
	name := min(27.75*rem, e-pad-gap-s)                 // --name-w, --name-cap 27.75rem
	status := e - pad - gap - name                      // --status-w
	// name status repeat(6, minmax(5.625rem,1fr)) 4rem minmax(5.625rem,1fr) minmax(5.625rem,1fr) minmax(7.5rem,1.25fr)
	grid = name + status + 6*5.625*rem + 4*rem + 5.625*rem + 7.5*rem + 10*gap
	content = vw - 2*2*rem - 2 - 2*pad // body padding 2rem each side, panel border 1px each side, row padding
	return grid, content
}

// At 2000 px the Work grid, always ten columns (only the states a card has,
// the human 2026-11-07), needs 1733 px and its row has 1870 px: it fits. Before the fail
// column went it needed 1894 px, 24 px more than the row (v2-2000.png vs v1-live-2000.png).
// README: "the full Work table fits at both 2000 and 2800 px".
func TestColdread3DefectWorkGridOverflowsAt2000(t *testing.T) {
	t.Parallel()
	css, err := page.ReadFile("page/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		".panel { --E: clamp(18.5rem, 45vw, 42rem); }",
		".streams .row { --pad: 2rem; --gap: clamp(1.5rem, calc(10vw - 10.5625rem), 2.5rem); --S: 9.75rem;",
		"grid-template-columns: var(--name-w) var(--status-w) repeat(6, minmax(5.625rem, 1fr)) 4rem minmax(5.625rem, 1fr) minmax(7.5rem, 1.25fr); } /* Glenn 2026-11-07: always ten columns; a held card is waiting */",
		".streams .row { --name-cap: 27.75rem; }",
		"padding: 2rem 2rem 4rem;",
	} {
		if !strings.Contains(string(css), rule) {
			t.Fatalf("index.html changed; recompute: %s", rule)
		}
	}
	for _, vw := range []float64{2000, 2800} {
		grid, content := cr3WorkGrid(vw)
		t.Logf("vw=%v grid=%.1f content=%.1f overflow=%.1f", vw, grid, content, grid-content)
		if grid > content {
			t.Errorf("at %v px the Work grid's minimum %.0f px exceeds its row's %.0f px by %.0f px (into the 32 px right padding)", vw, grid, content, grid-content)
		}
	}
}

// A read with no holder (waiting, unassigned) makes a Readers row named "" with no cells:
// countRead creates readers[c.Holder] before its switch (shim.go:151). The ?all=1 Readers
// table then shows an unnamed row; v1's readers are named rows only.
func TestColdread3DefectReadersEmptyHolderRow(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p": {ID: "p", Kind: "producer", Stream: "s", State: "review", Tier: "pro"},
		"r": {ID: "r", Kind: "read", Of: "p", Stream: "s", State: "waiting", Tier: "pro"},
	}}
	readers := mapSnapshot(s, testRoster, time.Time{}, nil).Tables["readers"].(map[string]numberCells)
	if _, ok := readers[""]; ok {
		t.Errorf("readers table has an unnamed row: %v", readers)
	}
}

// A row whose ok and failed are both 0 shows "-" in its ok% cell until there is
// data, as the total already does.
func TestColdread3DefectOkPctZeroDenominator(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{"alex": {Name: "alex", State: "down"}}}
	b, err := json.Marshal(mapSnapshot(s, testRoster, time.Time{}, nil).Tables["friends"])
	if err != nil {
		t.Fatal(err)
	}
	var rows map[string]map[string]string
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	if got := rows["alex"]["okpct"]; got != "-" {
		t.Errorf("zero-denominator ok%%: %q, want \"-\"", got)
	}
}

// start and stop run pg_ctl (-m immediate on stop), never a signal.
