package dash

// Fifth cold read of the v2 dashboard shim (2026-11-06), head 20cf5255.
// Rowan’s fifth cold-read probes.
// TestColdread5Check* are checks that passed on this head (they fail only on a regression).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
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
