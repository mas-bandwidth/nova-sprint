package dash

// Fourth cold read of the v2 dashboard shim (2026-11-06), head 62f2dcff.
// Adapted from Rowan’s fourth cold reader.
// TestColdread4Measure* only logs.

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// At a 2000 px window with a classic (non-overlay) 15 px vertical scrollbar, as desktop
// Chrome draws on Linux and Windows and on macOS with "always show scrollbars", the
// ten-column Work grid needs 1864 px and its row content box is 1855 px: fitTables'
// new content-box check makes Work compact (stream | status | landed). v1 shows the full
// table there (its grid needs 1814 px). Measured in Chromium with a 15 px scrollbar on the
// unmodified pages: v1 full Work at windows >= 1930 px, v2 full only at >= 2010 px; at
// 2000 px v1 full (40 px gutter), v2 compact (overflow 9 px). The round-four evidence was
// captured headless with overlay scrollbars (no scrollbar width). Contract: README "the
// full eleven-column Work table fits at both 2000 and 2800 px"; Glenn "exactly matching".
func TestColdread4DefectWorkCompactAt2000WithScrollbar(t *testing.T) {
	t.Parallel()
	const rem, vw, scrollbar = 16.0, 2000.0, 15.0
	clamp := func(lo, v, hi float64) float64 { return max(lo, min(v, hi)) }
	css, err := page.ReadFile("page/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".streams .row { --pad: 2rem; --gap: clamp(1.5rem, calc(10vw - 10.5625rem), 2.5rem);") {
		t.Fatal("index.html changed; recompute")
	}
	// vw units include the scrollbar; the content box does not.
	e := clamp(18.5*rem, 0.45*vw, 42*rem)
	gap := clamp(1.5*rem, 0.10*vw-10.5625*rem, 2.5*rem)
	name := min(27.75*rem, e-2*rem-gap-9.75*rem)
	status := e - 2*rem - gap - name
	grid := name + status + 6*5.625*rem + 4*rem + 5.625*rem + 5.625*rem + 7.5*rem + 11*gap
	content := vw - scrollbar - 4*rem - 2 - 4*rem
	v1gap := 2.5 * rem
	v1name := min(27.75*rem, e-2*rem-v1gap-9.75*rem)
	v1grid := v1name + (e - 2*rem - v1gap - v1name) + 6*5.625*rem + 4*rem + 5.625*rem + 7.5*rem + 10*v1gap
	t.Logf("2000 px + 15 px scrollbar: content %.0f, v2 grid %.0f, v1 grid %.0f", content, grid, v1grid)
	if v1grid <= content && grid > content {
		t.Errorf("at a 2000 px window with a 15 px scrollbar v1 shows the full Work table (%.0f <= %.0f) and v2 goes compact (%.0f > %.0f)", v1grid, content, grid, content)
	}
}

// cmd/nova-sprint/open.go (the dash verb's store, and every verb's) must call store.Open (pool create + Ping) with a deadline, never the signal
// context, which has no deadline: an unreachable or blackholed PostgreSQL holds startup for
// the OS connect timeout (or forever behind a proxy that accepts and stalls). Every other
// store call has one (New 30 s, Advance 500 ms). Brief: "deadlines not covering every
// store call".
func TestColdread4DefectMainOpenWithoutDeadline(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "cmd", "nova-sprint", "open.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	deadlined := map[string]bool{}
	var openArg string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Rhs) == 1 {
				if c, ok := x.Rhs[0].(*ast.CallExpr); ok {
					if s, ok := c.Fun.(*ast.SelectorExpr); ok && (s.Sel.Name == "WithTimeout" || s.Sel.Name == "WithDeadline") {
						if id, ok := x.Lhs[0].(*ast.Ident); ok {
							deadlined[id.Name] = true
						}
					}
				}
			}
		case *ast.CallExpr:
			if s, ok := x.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Open" {
				if p, ok := s.X.(*ast.Ident); ok && p.Name == "store" && len(x.Args) > 0 {
					if id, ok := x.Args[0].(*ast.Ident); ok {
						openArg = id.Name
					}
				}
			}
		}
		return true
	})
	if openArg == "" {
		t.Fatal("store.Open call not found; recheck open.go")
	}
	if !deadlined[openArg] {
		t.Errorf("open.go: store.Open(%s, ...) has no deadline (%s is not from context.WithTimeout/WithDeadline)", openArg, openArg)
	}
}

// stream_costs.<s>.total_cost, work_cost and read_cost are dollar strings ("$0.54"); work
// and read partition the rounded total, so their displayed cents add to the total.
func TestColdread4StreamCostsAreRoundedStrings(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p": {ID: "p", Kind: "producer", Stream: "s", State: "working", Tier: "pro"},
		"w": {ID: "w", Kind: "work", Of: "p", Stream: "s", Tier: "pro", State: "done", Cost: 1234567, Priced: true},
		"r": {ID: "r", Kind: "read", Of: "p", Stream: "s", Tier: "pro", State: "done", Cost: 1000, Priced: true},
		"u": {ID: "u", Kind: "work", Of: "p", Stream: "s", Tier: "pro", State: "waiting"},
	}}
	cache := &Cache{view: mapSnapshot(s, testRoster, time.Time{}), at: t1000, now: at1000}
	b, err := json.Marshal(cache.pageSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var v2 map[string]any
	if err = json.Unmarshal(b, &v2); err != nil {
		t.Fatal(err)
	}
	sc := v2["data"].(map[string]any)["stream_costs"].(map[string]any)["s"].(map[string]any)
	if sc["total_cost"] != "$1.24" || sc["work_cost"] != "$1.24" || sc["read_cost"] != "$0.00" || sc["unpriced_runs"] != float64(1) {
		t.Fatalf("stream cost values and count: %v", sc)
	}
}

// A card with a problem shows only as waiting on the board (Glenn, 2026-11-07: "not just the display,
// that's an invented concept"): no column, legend entry, In flight part, wire count or
// layout variant. In flight reads "N working · M verify" and its tooltip
// "N working, R review, F fix, M merging", as v1's.
func TestNoBlockedOnTheBoard(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"page/app.js", "page/index.html", "page/stream.html", "page/charts.js"} {
		b, err := page.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(strings.ToLower(line), "blocked") {
				t.Errorf("%s still names blocked: %s", f, strings.TrimSpace(line))
			}
		}
	}
	js, _ := page.ReadFile("page/app.js")
	if !strings.Contains(string(js), `sum.merging + " merging");`) || !strings.Contains(string(js), `[[sum.working, w[0]], [sum.review + (sum.fix || 0) + sum.merging, w[1]]]`) {
		t.Error("app.js renderInflight: the tooltip and the line are working and verify alone")
	}
}

// The fleet/friends track counts a working work consumer with Attempt >= 2 as fix
// (shim.go countTrack). Round-four claim: "fix-track attempt >= 2 documented". README
// documents only the producer (Work) fix rule; DASHBOARD-LESSONS lists the track rule as
// "(rule to confirm)".
func TestColdread4DefectTrackFixRuleUndocumented(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range regexp.MustCompile(`[^.]*\.`).FindAllString(string(b), -1) {
		l := strings.ToLower(s)
		if strings.Contains(l, "track") && strings.Contains(l, "attempt") {
			return
		}
	}
	t.Error("README has no sentence stating the track's fix rule (working work consumer, attempt >= 2)")
}

// Round-four claim "dead code removed". Still present in non-test code: Cache.Data()
// (no non-test caller; it duplicates pageSnapshot's error override), workRow.Unpriced
// (written in countCost, never read outside tests; not marshalled), and wire.go's
// OKPct == nil branch (setPercent always sets OKPct, so "-" is unreachable).
func TestColdread4DefectDeadCodeRemains(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	s := src.String()
	var dead []string
	if strings.Contains(s, "func (c *Cache) Data() any") && !strings.Contains(s, ".Data()") {
		dead = append(dead, "Cache.Data (no non-test caller)")
	}
	reads := strings.Count(s, "Unpriced") - strings.Count(s, "Unpriced   bool") - strings.Count(s, "w.Unpriced = true")
	if strings.Contains(s, "Unpriced   bool") && reads == 0 {
		dead = append(dead, "workRow.Unpriced (never read)")
	}
	if strings.Contains(s, "f.OKPct = &p") && strings.Contains(s, "if f.OKPct != nil {") {
		dead = append(dead, `fleetRow.MarshalJSON "-" branch (OKPct never nil)`)
	}
	if len(dead) > 0 {
		t.Errorf("dead code remains: %s", strings.Join(dead, "; "))
	}
}

// Measurement: 5,000 producers in 50 streams, a work and a read consumer for every
// producer (10,000 consumers), 12 worker and 4 kin rows, on the in-memory store.
func TestFunctionalColdread4Measure5000(t *testing.T) {
	t.Parallel()
	requireDashLoad(t)
	ctx := context.Background()
	f := store.NewFake()
	var w []store.Write
	for i := 0; i < 12; i++ {
		w = append(w, store.PutRow{Name: fmt.Sprintf("m%02d", i)})
	}
	for i := 0; i < 4; i++ {
		w = append(w, store.PutRow{Name: fmt.Sprintf("k%d", i)})
	}
	for i := 0; i < 50; i++ {
		w = append(w, store.PutStream{Name: fmt.Sprintf("s%02d", i)})
	}
	for i := 0; i < 5000; i++ {
		w = append(w, store.PutCard{ID: fmt.Sprintf("p%05d", i), Kind: "producer", Tier: "pro", Stream: fmt.Sprintf("s%02d", i%50)})
	}
	mustApply(t, f, "seed", 1, w...)
	w = nil
	for i := 0; i < 5000; i++ {
		p, st := fmt.Sprintf("p%05d", i), fmt.Sprintf("s%02d", i%50)
		w = append(w, store.PutCard{ID: "w" + p, Kind: "work", Of: p, Stream: st, Tier: "pro", Holder: fmt.Sprintf("m%02d", i%12)},
			store.PutCard{ID: "r" + p, Kind: "read", Of: p, Stream: st, Tier: "pro", Holder: fmt.Sprintf("k%d", i%4)})
	}
	mustApply(t, f, "consumers", 1, w...)
	t0 := time.Now()
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	startup := time.Since(t0)
	if err = c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	h := Handler(c)
	var idle, one, js, jsz []time.Duration
	var size, sizez int
	for i := 0; i < 30; i++ {
		t0 := time.Now()
		if err = c.Advance(ctx, f); err != nil {
			t.Fatal(err)
		}
		idle = append(idle, time.Since(t0))
		mustApply(t, f, fmt.Sprintf("m%d", i), 1, store.SetCardReason{Card: "p00001", Reason: fmt.Sprint(i)})
		t1 := time.Now()
		if err = c.Advance(ctx, f); err != nil {
			t.Fatal(err)
		}
		one = append(one, time.Since(t1))
		t2 := time.Now()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
		js = append(js, time.Since(t2))
		size = rec.Body.Len()
		t3 := time.Now()
		req := httptest.NewRequest("GET", "/api/sprint", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		jsz = append(jsz, time.Since(t3))
		sizez = rec.Body.Len()
	}
	q := func(d []time.Duration) string {
		s := append([]time.Duration(nil), d...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		return fmt.Sprintf("median=%s p90=%s max=%s", s[len(s)/2], s[len(s)*9/10], s[len(s)-1])
	}
	t.Logf("5000 producers + 10000 consumers, 16 rows, 30 iterations: startup %s; idle tick %s; one-event tick %s; JSON %s (%d B); JSON gzip %s (%d B)", startup, q(idle), q(one), q(js), size, q(jsz), sizez)
}
