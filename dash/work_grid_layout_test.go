package dash

import (
	"strings"
	"testing"
)

// workGridWidths is the no-blocked Work grid's minimum width and its row's content
// width, in px, at viewport width vw, from page/index.html's own rules.
func workGridWidths(vw float64) (grid, content float64) {
	const rem = 16.0
	clamp := func(lo, v, hi float64) float64 { return max(lo, min(v, hi)) }
	e := clamp(18.5*rem, 0.45*vw, 42*rem)               // .panel { --E: clamp(18.5rem, 45vw, 42rem) }
	gap := clamp(1.5*rem, 0.10*vw-10.5625*rem, 2.5*rem) // .streams.no-blocked .row --gap
	pad, s := 2*rem, 9.75*rem                           // .streams .row --pad, --S
	name := min(27.75*rem, e-pad-gap-s)                 // --name-w, --name-cap 27.75rem
	status := e - pad - gap - name                      // --status-w
	// name status repeat(6, minmax(5.625rem,1fr)) 4rem minmax(5.625rem,1fr) minmax(5.625rem,1fr) minmax(7.5rem,1.25fr)
	grid = name + status + 6*5.625*rem + 4*rem + 5.625*rem + 7.5*rem + 10*gap
	content = vw - 2*2*rem - 2 - 2*pad // body padding 2rem each side, panel border 1px each side, row padding
	return grid, content
}

// TestWorkGridFitsAt2000And2800 checks that the ten-column Work grid, with no blocked card, fits its row at
// 2000 and 2800 px. The widths are computed from the rules in page/index.html, which the test also checks
// are unchanged.
func TestWorkGridFitsAt2000And2800(t *testing.T) {
	css, err := page.ReadFile("page/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		".panel { --E: clamp(18.5rem, 45vw, 42rem); }",
		".streams.no-blocked .row { --gap: clamp(1.5rem, calc(10vw - 10.5625rem), 2.5rem); grid-template-columns: var(--name-w) var(--status-w) repeat(6, minmax(5.625rem, 1fr)) 4rem minmax(5.625rem, 1fr) minmax(7.5rem, 1.25fr); }",
		".streams .row { --name-cap: 27.75rem; }",
		"padding: 2rem 2rem 4rem;",
	} {
		if !strings.Contains(string(css), rule) {
			t.Fatalf("page/index.html changed; recompute the grid widths: %s", rule)
		}
	}
	for _, vw := range []float64{2000, 2800} {
		grid, content := workGridWidths(vw)
		t.Logf("vw=%v grid=%.1f content=%.1f overflow=%.1f", vw, grid, content, grid-content)
		if grid > content {
			t.Errorf("at %v px the Work grid's minimum %.0f px exceeds its row's %.0f px by %.0f px (into the 32 px right padding)", vw, grid, content, grid-content)
		}
	}
}

// TestWorkGridFitsAt2000WithClassicScrollbar checks the Work grid at a 2000 px window with a classic
// 15 px vertical scrollbar: the no-blocked grid must not exceed the row's content box, or the table goes
// compact. The content box excludes the scrollbar; vw units include it.
func TestWorkGridFitsAt2000WithClassicScrollbar(t *testing.T) {
	const rem, vw, scrollbar = 16.0, 2000.0, 15.0
	clamp := func(lo, v, hi float64) float64 { return max(lo, min(v, hi)) }
	css, err := page.ReadFile("page/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".streams.no-blocked .row { --gap: clamp(1.5rem, calc(10vw - 10.5625rem), 2.5rem);") {
		t.Fatal("page/index.html changed; recompute the grid widths")
	}
	// vw units include the scrollbar; the content box does not.
	e := clamp(18.5*rem, 0.45*vw, 42*rem)
	gap := clamp(1.5*rem, 0.10*vw-10.5625*rem, 2.5*rem)
	name := min(27.75*rem, e-2*rem-gap-9.75*rem)
	status := e - 2*rem - gap - name
	grid := name + status + 6*5.625*rem + 4*rem + 5.625*rem + 5.625*rem + 7.5*rem + 11*gap
	content := vw - scrollbar - 4*rem - 2 - 4*rem
	t.Logf("2000 px + 15 px scrollbar: content %.0f, grid %.0f", content, grid)
	if grid > content {
		t.Errorf("at a 2000 px window with a 15 px scrollbar the Work grid needs %.0f px and its row content box is %.0f px", grid, content)
	}
}
