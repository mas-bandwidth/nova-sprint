package ci

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTestsUseTestify holds a shrink-only per-package ledger of two kinds of
// non-testify test assertions:
//
//   - assert: an `if` statement whose body calls t.Fatal, t.Fatalf, t.Error,
//     t.Errorf, t.Fail or t.FailNow.
//   - env: any call to t.Setenv, t.Chdir or os.Chdir.
//
// The ledger lives in internal/ci/testdata/testify/<package-path>.txt, one shard
// per package that has a *_test.go file (except packages under testdata/ or tools/).
// Each shard opens with the comment `# ceiling: 0`. A row has the form
// `<package>:<kind> <sites> <reason>` where kind is `assert` or `env`.
//
// The count only falls. A package that measures more sites than its row, or has
// a site and no row, fails. A row whose count is above the measured count is
// reported as a NOTE, because this stream's package cards lower counts without
// editing the shards; run with NOVA_CI_UPDATE=1 to lower the rows.
//
// Remedies:
//   - assert: replace the bare `if` with a testify assertion. Use require.NoError(err)
//     for an `err != nil` guard, assert.Equal(got, want) for equality, assert.Contains
//     for substring checks, and assert.Empty when an empty result is expected.
//   - env: remove t.Setenv, t.Chdir and os.Chdir (use t.TempDir or pass values explicitly).
func TestTestsUseTestify(t *testing.T) {
	t.Parallel()
	problems := testifyCheck(t, "../..", os.Getenv("NOVA_CI_UPDATE") == "1")
	require.Empty(t, problems, "bare assertions or env calls outside the testify ledger:\n%s", strings.Join(problems, "\n"))
}

// TestTestsUseTestifyRefusesCeilingPlusOne pins that a package whose measured
// count is one above its ledger row fails, naming the package, kind and counts.
func TestTestsUseTestifyRefusesCeilingPlusOne(t *testing.T) {
	t.Parallel()
	root := testifyFixtureRoot(t)
	writeTestifyTestFile(t, root, "badpkg/badpkg_test.go", `package badpkg
import "testing"
func TestOne(t *testing.T) {
	if 1 != 2 {
		t.Fatalf("one")
	}
}
func TestTwo(t *testing.T) {
	if 2 != 3 {
		t.Fatalf("two")
	}
}
`)
	writeTestifyLedgerFile(t, root, "internal/ci/testdata/testify/badpkg.txt", "# ceiling: 0\nbadpkg:assert 1 use require.NoError, assert.Equal, assert.Contains or assert.Empty instead of bare if-guarded t.Fatal/Error/Fail\n")

	problems := testifyCheck(t, root, false)
	require.NotEmpty(t, problems)
	assert.Contains(t, strings.Join(problems, "\n"), "badpkg:assert: measured 2, row 1")
}

// TestTestsUseTestifyUpdateNeverRaises pins that the update mode is shrink-only:
// when a package measures more than its row, NOVA_CI_UPDATE leaves the row alone
// so the run stays red, and never writes the raised count.
func TestTestsUseTestifyUpdateNeverRaises(t *testing.T) {
	t.Parallel()
	root := testifyFixtureRoot(t)
	writeTestifyTestFile(t, root, "badpkg/badpkg_test.go", `package badpkg
import "testing"
func TestOne(t *testing.T) {
	if 1 != 2 {
		t.Fatalf("one")
	}
}
func TestTwo(t *testing.T) {
	if 2 != 3 {
		t.Fatalf("two")
	}
}
`)
	writeTestifyLedgerFile(t, root, "internal/ci/testdata/testify/badpkg.txt", "# ceiling: 0\nbadpkg:assert 1 use require.NoError, assert.Equal, assert.Contains or assert.Empty instead of bare if-guarded t.Fatal/Error/Fail\n")

	problems := testifyCheck(t, root, true)
	require.NotEmpty(t, problems, "a package above its row must stay red after the update")
	assert.Contains(t, strings.Join(problems, "\n"), "badpkg:assert: measured 2, row 1")

	b, err := os.ReadFile(filepath.Join(root, "internal/ci/testdata/testify/badpkg.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(b), "badpkg:assert 1 ")
	assert.NotContains(t, string(b), "badpkg:assert 2 ")
}

// TestTestsUseTestifyUpdateSeedsCommentOnlyShards pins that the update mode
// seeds a `# ceiling: 0` shard for a walked test package that measures no
// guarded site, so every package named on the ledger has a shard.
func TestTestsUseTestifyUpdateSeedsCommentOnlyShards(t *testing.T) {
	t.Parallel()
	root := testifyFixtureRoot(t)
	writeTestifyTestFile(t, root, "pkg/quiet/quiet_test.go", `package quiet
import (
	"testing"

	"github.com/stretchr/testify/require"
)
func TestQuiet(t *testing.T) {
	require.NoError(t, nil)
}
`)

	problems := testifyCheck(t, root, true)
	require.Empty(t, problems)

	b, err := os.ReadFile(filepath.Join(root, "internal/ci/testdata/testify/pkg/quiet.txt"))
	require.NoError(t, err)
	assert.Equal(t, "# ceiling: 0\n", string(b))
}

const (
	testifyLedgerRoot = "internal/ci/testdata/testify"

	assertReason = "use require.NoError, assert.Equal, assert.Contains or assert.Empty instead of bare if-guarded t.Fatal/Error/Fail"
	envReason    = "remove t.Setenv, t.Chdir and os.Chdir (use t.TempDir or pass values explicitly)"
)

// testifyKind is either assert or env.
type testifyKind string

const (
	testifyAssert testifyKind = "assert"
	testifyEnv    testifyKind = "env"
)

// testifySite is one counted site within a package's test files.
type testifySite struct {
	Pkg    string
	Kind   testifyKind
	File   string
	Line   int
	Reason string
}

// testifyRow is one ledger row read from a shard.
type testifyRow struct {
	Pkg    string
	Kind   testifyKind
	Count  int
	Reason string
}

// testifyImportMap maps an import name to its import path, and records dot-imported paths.
type testifyImportMap struct {
	byName map[string]string
	dots   []string
}

// loadTestifyImports builds an import map for a parsed file.
func loadTestifyImports(f *ast.File) testifyImportMap {
	out := testifyImportMap{byName: map[string]string{}}
	for _, is := range f.Imports {
		p, _ := strconv.Unquote(is.Path.Value)
		name := filepath.Base(p)
		if is.Name != nil {
			name = is.Name.Name
		}
		if name == "." {
			out.dots = append(out.dots, p)
			continue
		}
		out.byName[name] = p
	}
	return out
}

// testifyCheck walks root's Go test packages, compares each package's measured
// site counts against its ledger shard, and returns a problem per mismatch.
// When update is true, it rewrites the shards to match the measured counts.
func testifyCheck(t *testing.T, root string, update bool) []string {
	t.Helper()
	pkgs := testifyPackages(t, root)
	measured := map[string]map[testifyKind]int{} // pkg -> kind -> count
	for _, pkg := range pkgs {
		files := testifyTestFiles(t, root, pkg)
		if len(files) == 0 {
			continue
		}
		sites, err := scanTestifySites(pkg, files)
		require.NoError(t, err, "scanning %s", pkg)
		for _, s := range sites {
			if measured[s.Pkg] == nil {
				measured[s.Pkg] = map[testifyKind]int{}
			}
			measured[s.Pkg][s.Kind]++
		}
	}

	if update {
		updateTestifyLedgers(t, root, pkgs, measured)
	}

	rows := readTestifyLedgers(t, root)
	rowCount := map[string]map[testifyKind]int{}
	for _, r := range rows {
		if rowCount[r.Pkg] == nil {
			rowCount[r.Pkg] = map[testifyKind]int{}
		}
		rowCount[r.Pkg][r.Kind] = r.Count
	}

	var problems []string
	var notes []string
	var totalMeasured, totalRow int

	allPkgs := map[string]bool{}
	for pkg := range measured {
		allPkgs[pkg] = true
	}
	for pkg := range rowCount {
		allPkgs[pkg] = true
	}

	for pkg := range allPkgs {
		for _, kind := range []testifyKind{testifyAssert, testifyEnv} {
			m := measured[pkg][kind]
			r := rowCount[pkg][kind]
			totalMeasured += m
			totalRow += r
			switch {
			case m > 0 && r == 0:
				// No row or a row with count zero: the package has sites and nothing covers them.
				problems = append(problems, fmt.Sprintf("%s:%s: measured %d, no row: add a ledger shard at %s/%s.txt with the current count", pkg, kind, m, testifyLedgerRoot, pkg))
			case m > r:
				problems = append(problems, fmt.Sprintf("%s:%s: measured %d, row %d: lower the row or replace the bare assertions with testify", pkg, kind, m, r))
			case r > m:
				notes = append(notes, fmt.Sprintf("NOTE %s:%s: measured %d, row %d (lower with NOVA_CI_UPDATE=1)", pkg, kind, m, r))
			}
		}
	}

	if len(notes) > 0 {
		t.Logf("testify-ledger notes:\n%s", strings.Join(notes, "\n"))
	}
	t.Logf("testify-ledger total: measured %d, row %d", totalMeasured, totalRow)

	sort.Strings(problems)
	return problems
}

// testifyPackages returns every package directory under root that contains at
// least one *_test.go file, skipping testdata/, tools/ and hidden directories.
func testifyPackages(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		name := filepath.Base(rel)
		if strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "tmp" || rel == "tools" || strings.HasPrefix(rel, "tools/") {
			return filepath.SkipDir
		}
		if strings.Contains(rel, "/testdata/") || strings.HasSuffix(rel, "/testdata") {
			return filepath.SkipDir
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		hasTest := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
				hasTest = true
				break
			}
		}
		if hasTest && !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(out)
	return out
}

// testifyTestFiles returns the test files of a single package, keyed by their
// slash path relative to root.
func testifyTestFiles(t *testing.T, root, pkg string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	entries, err := os.ReadDir(filepath.Join(root, pkg))
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		rel := pkg + "/" + e.Name()
		b, err := os.ReadFile(filepath.Join(root, rel))
		require.NoError(t, err)
		files[rel] = b
	}
	return files
}

// scanTestifySites returns every counted site in a package's test files.
func scanTestifySites(pkg string, files map[string][]byte) ([]testifySite, error) {
	fset := token.NewFileSet()
	sites := map[string]bool{} // de-duplicate assert sites by package+file+if-line
	var out []testifySite
	for rel, src := range files {
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		imports := loadTestifyImports(f)

		// assert sites: if statements whose body contains a forbidden call.
		ast.Inspect(f, func(n ast.Node) bool {
			ifs, ok := n.(*ast.IfStmt)
			if !ok {
				return true
			}
			if hasForbiddenAssertCall(ifs.Body, imports) {
				key := fmt.Sprintf("%s:%d", rel, fset.Position(ifs.Pos()).Line)
				if !sites[key] {
					sites[key] = true
					out = append(out, testifySite{Pkg: pkg, Kind: testifyAssert, File: rel, Line: fset.Position(ifs.Pos()).Line, Reason: assertReason})
				}
			}
			return true
		})

		// env sites: any forbidden env call anywhere in the file.
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if kind, name := forbiddenEnvCall(call.Fun, imports); kind != "" {
				out = append(out, testifySite{Pkg: pkg, Kind: kind, File: rel, Line: fset.Position(call.Pos()).Line, Reason: name})
			}
			return true
		})
	}
	return out, nil
}

var forbiddenAssertMethods = map[string]bool{
	"Fatal": true, "Fatalf": true, "Error": true, "Errorf": true,
	"Fail": true, "FailNow": true,
}

// hasForbiddenAssertCall reports whether a block contains a call to one of the
// forbidden testing.TB methods.
func hasForbiddenAssertCall(body *ast.BlockStmt, imports testifyImportMap) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isForbiddenAssertCall(call, imports) {
			found = true
			return false
		}
		return true
	})
	return found
}

// isForbiddenAssertCall reports whether a call expression is one of the forbidden
// testing.TB methods. It recognises t.Method, tb.Method and imports of testing.
func isForbiddenAssertCall(call *ast.CallExpr, imports testifyImportMap) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if !forbiddenAssertMethods[sel.Sel.Name] {
		return false
	}
	if id.Name == "t" || id.Name == "tb" {
		return true
	}
	if p, ok := imports.byName[id.Name]; ok && p == "testing" {
		return true
	}
	return false
}

// forbiddenEnvCall reports whether a call expression is t.Setenv, t.Chdir or os.Chdir.
func forbiddenEnvCall(fun ast.Expr, imports testifyImportMap) (testifyKind, string) {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", ""
	}
	method := sel.Sel.Name
	if method == "Setenv" && (id.Name == "t" || id.Name == "tb") {
		return testifyEnv, envReason
	}
	if method == "Chdir" {
		if id.Name == "t" || id.Name == "tb" {
			return testifyEnv, envReason
		}
		if p, ok := imports.byName[id.Name]; ok && p == "os" {
			return testifyEnv, envReason
		}
	}
	return "", ""
}

// readTestifyLedgers walks every shard under internal/ci/testdata/testify.
func readTestifyLedgers(t *testing.T, root string) []testifyRow {
	t.Helper()
	dir := filepath.Join(root, testifyLedgerRoot)
	rows := []testifyRow{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == dir {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".txt") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(strings.TrimSuffix(rel, ".txt"))
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 3 {
				t.Fatalf("%s/%s:%d: ledger row must be `<package>:<kind> <count> <reason...>`", testifyLedgerRoot, rel, i+1)
			}
			nameKind := strings.SplitN(fields[0], ":", 2)
			if len(nameKind) != 2 {
				t.Fatalf("%s/%s:%d: ledger row %q must begin with package:kind", testifyLedgerRoot, rel, i+1, fields[0])
			}
			if filepath.Clean(nameKind[0]) != filepath.Clean(pkg) {
				t.Fatalf("%s/%s:%d: row package %q does not match shard path %q", testifyLedgerRoot, rel, i+1, nameKind[0], pkg)
			}
			kind := testifyKind(nameKind[1])
			if kind != testifyAssert && kind != testifyEnv {
				t.Fatalf("%s/%s:%d: kind %q is not assert or env", testifyLedgerRoot, rel, i+1, nameKind[1])
			}
			count, err := strconv.Atoi(fields[1])
			require.NoError(t, err, "%s/%s:%d: count", testifyLedgerRoot, rel, i+1)
			rows = append(rows, testifyRow{Pkg: pkg, Kind: kind, Count: count, Reason: strings.Join(fields[2:], " ")})
		}
		return nil
	})
	require.NoError(t, err)
	return rows
}

// updateTestifyLedgers rewrites the testify ledger shards. The update is
// shrink-only: a row is lowered to the measured count when the package measures
// fewer sites, and left untouched otherwise, so a package that measures more
// than its row stays red. A row is never added to a shard that already exists,
// so a package whose shard is comment-only stays red when it gains a site.
//
// A package that has no shard yet is seeded from the measured counts, and every
// walked test package gets a shard: a package that measures no guarded site gets
// a shard with the `# ceiling: 0` comment only.
func updateTestifyLedgers(t *testing.T, root string, pkgs []string, measured map[string]map[testifyKind]int) {
	t.Helper()
	dir := filepath.Join(root, testifyLedgerRoot)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	existing := map[string]map[testifyKind]int{}
	for _, r := range readTestifyLedgers(t, root) {
		if existing[r.Pkg] == nil {
			existing[r.Pkg] = map[testifyKind]int{}
		}
		existing[r.Pkg][r.Kind] = r.Count
	}

	write := map[string]bool{}
	for _, pkg := range pkgs {
		write[pkg] = true
	}
	for pkg := range existing {
		write[pkg] = true
	}

	for pkg := range write {
		_, err := os.Stat(filepath.Join(dir, pkg+".txt"))
		shardExists := err == nil

		var assertCount, envCount int
		for _, kind := range []testifyKind{testifyAssert, testifyEnv} {
			mc := measured[pkg][kind]
			ec, hasRow := existing[pkg][kind]
			count := 0
			switch {
			case hasRow:
				// Lower only: never write a count above the existing row.
				count = ec
				if mc < ec {
					count = mc
				}
			case shardExists:
				// A comment-only shard is never raised into a row.
				count = 0
			default:
				// Seed a fresh shard from the measured count.
				count = mc
			}
			if kind == testifyAssert {
				assertCount = count
			} else {
				envCount = count
			}
		}

		var lines []string
		lines = append(lines, "# ceiling: 0")
		if assertCount > 0 {
			lines = append(lines, fmt.Sprintf("%s:%s %d %s", pkg, testifyAssert, assertCount, assertReason))
		}
		if envCount > 0 {
			lines = append(lines, fmt.Sprintf("%s:%s %d %s", pkg, testifyEnv, envCount, envReason))
		}
		path := filepath.Join(dir, pkg+".txt")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	}
}

// testifyFixtureRoot creates a temporary root for the fixture tests.
func testifyFixtureRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// writeTestifyTestFile writes a test file under the fixture root.
func writeTestifyTestFile(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

// writeTestifyLedgerFile writes a testify shard under the fixture root.
func writeTestifyLedgerFile(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}
