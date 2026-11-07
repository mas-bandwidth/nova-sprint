package dash

// Fifth cold read of the v2 dashboard shim (2026-11-06), head 20cf5255.
// Rowan’s fifth cold-read probes.
// TestColdread5Check* are checks that passed on this head (they fail only on a regression).

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// v1's ETA tile, with no ETA in the summary, not stopped and not done, shows "-" with the sub
// "not in the summary" (v1.6.29 internal/sprintdash/page/app.js renderHero). The shim's page
// blanks the sub. The v2 store has no summary, so this is the branch the ETA tile always takes.
// Contract: docs/DASHBOARD-LESSONS.md ETA tile (d) "else `-` with sub `not in the summary`";
// dash/README.md round-two ruling 4 "v1's cost, ok%, ETA and throughput fallbacks remain".
func TestColdread5DefectETAFallbackSubDropped(t *testing.T) {
	t.Parallel()
	// Ruled 2026-11-06 (Glenn: "I don't think I ever asked for 'not in the summary' to be
	// displayed"): v2 has no summary, so the ETA subline is blank. The probe now holds that.
	js, err := page.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	const v1 = `else { setText($("eta"), "-"); setText($("eta-at"), "not in the summary"); }`
	const v2 = `else { setText($("eta"), "-"); setText($("eta-at"), ""); }`
	if strings.Contains(string(js), v1) || !strings.Contains(string(js), v2) {
		t.Errorf("ETA fallback sub: ruled blank in v2, page still carries v1's %q", "not in the summary")
	}
}

// No production function in dash/ is over 60 lines (the brief's limit); test
// functions over the limit are logged only.
func TestColdread5CheckFunctionsUnder60Lines(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"*.go"} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				n := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
				if n <= 60 {
					continue
				}
				if strings.HasSuffix(path, "_test.go") {
					t.Logf("test function %s:%s is %d lines", path, fn.Name.Name, n)
				} else {
					t.Errorf("%s:%s is %d lines", path, fn.Name.Name, n)
				}
			}
		}
	}
}

// main.go now opens the store under a 30 s context and cancels it as soon as Open returns.
// pgxpool keeps no use of that context, so the pool must stay usable afterwards: Snapshot,
// Events and the cursor write all succeed after the open context is cancelled.
func TestColdread5CheckFunctionalPoolAfterOpenCancel(t *testing.T) {
	t.Parallel()
	dsn := startPG(t)
	var s *store.PG
	var err error
	for i := 0; i < 100; i++ {
		openCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		s, err = store.Open(openCtx, dsn, "")
		cancel()
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply(ctx, store.Op{ID: "cr5-seed", Epoch: 1, Actor: "cr5", Kind: "seed", At: time.Now(), Writes: []store.Write{store.PutStream{Name: "s"}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "s"}}})
	if err != nil || !r.Accepted {
		t.Fatalf("seed: %v %s", err, r.Reason)
	}
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	d := c.pageSnapshot().Data
	if !d.OK || d.All != 1 || c.state.Cursors[CursorName] == 0 {
		t.Fatalf("after open-cancel: ok=%v all=%d cursor=%d", d.OK, d.All, c.state.Cursors[CursorName])
	}
}
