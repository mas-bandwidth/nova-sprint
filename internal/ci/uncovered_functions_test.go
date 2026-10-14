package ci

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	uncoveredFunctionsLedgerRel  = "internal/ci/testdata/uncovered_functions_allowlist.txt"
	uncoveredFunctionsLedgerFile = "testdata/uncovered_functions_allowlist.txt"
	coverageActiveEnv            = "NOVA_CI_COVERAGE_ACTIVE"
	novaSprintModulePrefix       = "github.com/mas-bandwidth/nova-sprint/"
)

var coverageWatchedDirs = []string{"cmd", "internal", "pkg"}

type coverageBlock struct {
	startLine, endLine int
	stmts, count       int
}

func sprintRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	return root
}

func sprintGitOut(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func sprintCoverageBase(t *testing.T, root string) string {
	t.Helper()
	base := os.Getenv("UNIT_RULE_BASE")
	if base == "" {
		for _, ref := range []string{"refs/remotes/origin/v1.3/comms", "refs/remotes/origin/sprint/comfort", "refs/remotes/origin/dev", "refs/remotes/origin/main"} {
			if _, err := sprintGitOut(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
				if mb, err := sprintGitOut(root, "merge-base", "HEAD", ref); err == nil {
					base = strings.TrimSpace(mb)
				} else {
					base = ref
				}
				break
			}
		}
	}
	if base == "" {
		t.Skip("UNIT_RULE_BASE names no base revision and git has no origin/v1.3/comms, origin/sprint/comfort, origin/dev or origin/main here; CI sets it to the pull request's base")
	}
	if out, err := sprintGitOut(root, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
		t.Fatalf("UNIT_RULE_BASE %s is no commit git can read here (%v: %s): the uncovered-functions ledger is unchecked", base, err, strings.TrimSpace(out))
	}
	return base
}

func coverageFuncName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch x := t.(type) {
	case *ast.IndexExpr:
		t = x.X
	case *ast.IndexListExpr:
		t = x.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

func parseUncoveredFunctionsLedger(text string) ([]string, map[string]bool, []string) {
	var rows []string
	seen := map[string]bool{}
	var problems []string
	prev := ""
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			problems = append(problems, fmt.Sprintf("%s:%d: %q is not `<file> <func>`", uncoveredFunctionsLedgerRel, i+1, line))
			continue
		}
		key := fields[0] + " " + fields[1]
		if seen[key] {
			problems = append(problems, fmt.Sprintf("%s:%d: duplicate row %q", uncoveredFunctionsLedgerRel, i+1, key))
			continue
		}
		if prev != "" && key < prev {
			problems = append(problems, fmt.Sprintf("%s:%d: row %q is out of alphabetical order after %q", uncoveredFunctionsLedgerRel, i+1, key, prev))
		}
		prev = key
		seen[key] = true
		rows = append(rows, key)
	}
	return rows, seen, problems
}

func uncoveredLedgerStaleASTProblems(root string, rows []string) []string {
	byFile := map[string][]string{}
	var files []string
	for _, row := range rows {
		rel, fn, ok := strings.Cut(row, " ")
		if !ok {
			continue
		}
		if _, exists := byFile[rel]; !exists {
			files = append(files, rel)
		}
		byFile[rel] = append(byFile[rel], fn)
	}
	sort.Strings(files)
	var problems []string
	for _, rel := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		src, err := os.ReadFile(full)
		if err != nil {
			for _, fn := range byFile[rel] {
				problems = append(problems, fmt.Sprintf("%s lists %s %s, but %s does not exist; remove the stale row (the ledger only shrinks)", uncoveredFunctionsLedgerRel, rel, fn, rel))
			}
			continue
		}
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, rel, src, 0)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: parse %s: %v", uncoveredFunctionsLedgerRel, rel, err))
			continue
		}
		decls := map[string]bool{}
		for _, d := range af.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			decls[coverageFuncName(fn)] = true
		}
		for _, fn := range byFile[rel] {
			if !decls[fn] {
				problems = append(problems, fmt.Sprintf("%s lists %s %s, but no such function exists in %s; remove the stale row (the ledger only shrinks)", uncoveredFunctionsLedgerRel, rel, fn, rel))
			}
		}
	}
	return problems
}

func uncoveredLedgerGrowth(baseText, headText string) []string {
	_, baseSet, _ := parseUncoveredFunctionsLedger(baseText)
	headRows, _, _ := parseUncoveredFunctionsLedger(headText)
	var added []string
	for _, row := range headRows {
		if !baseSet[row] {
			added = append(added, row)
		}
	}
	sort.Strings(added)
	return added
}

func parseCoverProfileBlocks(profileText, modulePrefix string) map[string][]coverageBlock {
	byFile := map[string][]coverageBlock{}
	for _, raw := range strings.Split(profileText, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		stmts, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		colon := strings.LastIndex(fields[0], ":")
		if err1 != nil || err2 != nil || colon < 0 {
			continue
		}
		rel := strings.TrimPrefix(fields[0][:colon], modulePrefix)
		span := strings.Split(fields[0][colon+1:], ",")
		if len(span) != 2 {
			continue
		}
		sLine, err1 := strconv.Atoi(strings.Split(span[0], ".")[0])
		eLine, err2 := strconv.Atoi(strings.Split(span[1], ".")[0])
		if err1 != nil || err2 != nil {
			continue
		}
		byFile[rel] = append(byFile[rel], coverageBlock{
			startLine: sLine,
			endLine:   eLine,
			stmts:     stmts,
			count:     count,
		})
	}
	return byFile
}

func evaluateCoverageAgainstLedger(sources map[string][]byte, byFile map[string][]coverageBlock, allowed, touched map[string]bool) []string {
	var files []string
	for rel := range byFile {
		if strings.HasPrefix(rel, "internal/ci/") {
			continue
		}
		files = append(files, rel)
	}
	sort.Strings(files)
	var problems []string
	for _, rel := range files {
		src, ok := sources[rel]
		if !ok {
			continue
		}
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, rel, src, 0)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: parse error: %v", rel, err))
			continue
		}
		blocks := byFile[rel]
		seen := map[string]bool{}
		for _, d := range af.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := coverageFuncName(fn)
			key := rel + " " + name
			if seen[key] {
				continue
			}
			seen[key] = true
			sLine := fset.Position(fn.Pos()).Line
			eLine := fset.Position(fn.End()).Line
			totalStmts, coveredBlocks := 0, 0
			for _, b := range blocks {
				if b.startLine >= sLine && b.endLine <= eLine {
					totalStmts += b.stmts
					if b.count > 0 {
						coveredBlocks++
					}
				}
			}
			if totalStmts == 0 {
				continue
			}
			if coveredBlocks == 0 {
				if !allowed[key] {
					problems = append(problems, fmt.Sprintf("%s: live non-test function has 0%% test coverage and is not in %s; add a test that executes it", key, uncoveredFunctionsLedgerRel))
				} else if touched[key] {
					problems = append(problems, fmt.Sprintf("%s: live non-test function was modified by this change and still has 0%% test coverage; add a test that executes it and remove its row from %s", key, uncoveredFunctionsLedgerRel))
				}
			} else if allowed[key] {
				problems = append(problems, fmt.Sprintf("%s: function is executed by tests now; remove its stale row from %s (the ledger only shrinks)", key, uncoveredFunctionsLedgerRel))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

func parseDiffChangedLines(diffText string) map[string]map[int]bool {
	changed := map[string]map[int]bool{}
	curFile := ""
	for _, raw := range strings.Split(diffText, "\n") {
		if strings.HasPrefix(raw, "+++ b/") {
			curFile = strings.TrimSpace(strings.TrimPrefix(raw, "+++ b/"))
			continue
		}
		if curFile == "" || !strings.HasPrefix(raw, "@@ ") {
			continue
		}
		plus := strings.Index(raw, "+")
		end := strings.Index(raw[3:], " @@")
		if plus < 0 || end < 0 {
			continue
		}
		hunk := raw[plus+1 : 3+end]
		if sp := strings.Index(hunk, " "); sp >= 0 {
			hunk = hunk[:sp]
		}
		startStr, countStr, hasComma := strings.Cut(hunk, ",")
		start, err := strconv.Atoi(startStr)
		if err != nil {
			continue
		}
		count := 1
		if hasComma {
			count, err = strconv.Atoi(countStr)
			if err != nil {
				continue
			}
		}
		if count <= 0 {
			continue
		}
		if changed[curFile] == nil {
			changed[curFile] = map[int]bool{}
		}
		for line := start; line < start+count; line++ {
			changed[curFile][line] = true
		}
	}
	return changed
}

func touchedFunctionsInSource(rel string, src []byte, changedLines map[int]bool) map[string]bool {
	touched := map[string]bool{}
	if len(changedLines) == 0 {
		return touched
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return touched
	}
	lines := strings.Split(string(src), "\n")
	for _, d := range af.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		sLine := fset.Position(fn.Pos()).Line
		eLine := fset.Position(fn.End()).Line
		for l := sLine; l <= eLine; l++ {
			if !changedLines[l] || l < 1 || l > len(lines) {
				continue
			}
			trimmed := strings.TrimSpace(lines[l-1])
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			touched[rel+" "+coverageFuncName(fn)] = true
			break
		}
	}
	return touched
}

func isWatchedCoverageFile(rel string) bool {
	if !strings.HasSuffix(rel, ".go") || strings.HasPrefix(rel, "internal/ci/") {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "testdata" || part == "vendor" {
			return false
		}
	}
	for _, d := range coverageWatchedDirs {
		if strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

func dirHasLiveGoFiles(root, pkgDir string) bool {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(pkgDir)))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			return true
		}
	}
	return false
}

func TestUncoveredFunctionsLedgerNamesRealFunctions(t *testing.T) {
	t.Parallel()

	root := sprintRepoRoot(t)
	raw, err := os.ReadFile(uncoveredFunctionsLedgerFile)
	require.NoError(t, err)
	rows, _, parseProblems := parseUncoveredFunctionsLedger(string(raw))
	for _, p := range parseProblems {
		assert.Fail(t, p)
	}
	require.NotEmpty(t, rows, "%s is empty", uncoveredFunctionsLedgerRel)
	for _, p := range uncoveredLedgerStaleASTProblems(root, rows) {
		assert.Fail(t, p)
	}
}

func TestUncoveredFunctionsLedgerOnlyShrinks(t *testing.T) {
	t.Parallel()

	root := sprintRepoRoot(t)
	base := sprintCoverageBase(t, root)
	if _, err := sprintGitOut(root, "cat-file", "-e", base+":"+uncoveredFunctionsLedgerRel); err != nil {
		t.Logf("%s holds no %s: this change introduces the ledger", base, uncoveredFunctionsLedgerRel)
		return
	}
	baseText, err := sprintGitOut(root, "show", base+":"+uncoveredFunctionsLedgerRel)
	require.NoError(t, err)
	headBytes, err := os.ReadFile(uncoveredFunctionsLedgerFile)
	require.NoError(t, err)
	added := uncoveredLedgerGrowth(baseText, string(headBytes))
	if len(added) > 0 {
		t.Errorf("%s adds %d functions that %s does not list: the ledger only shrinks -- add a test that executes the function instead:\n%s",
			uncoveredFunctionsLedgerRel, len(added), base, strings.Join(added, "\n"))
	}
}

func TestChangedLiveCodeIsExecutedByTests(t *testing.T) {
	t.Parallel()

	if os.Getenv(coverageActiveEnv) == "1" {
		return
	}
	if runtime.GOOS != "linux" {
		t.Logf("live package coverage subprocess runs on linux CI legs; static ledger checks ran on %s", runtime.GOOS)
		return
	}
	root := sprintRepoRoot(t)
	base := sprintCoverageBase(t, root)
	diffNames, err := sprintGitOut(root, "diff", "--name-only", base, "HEAD")
	require.NoError(t, err)
	workNames, err := sprintGitOut(root, "diff", "--name-only", "HEAD")
	require.NoError(t, err)

	pkgSet := map[string]bool{}
	var changedLiveFiles []string
	seenFile := map[string]bool{}
	for _, raw := range strings.Split(diffNames+"\n"+workNames, "\n") {
		rel := strings.TrimSpace(raw)
		if !isWatchedCoverageFile(rel) || seenFile[rel] {
			continue
		}
		seenFile[rel] = true
		dir := path.Dir(rel)
		if dirHasLiveGoFiles(root, dir) {
			pkgSet["./"+dir] = true
		}
		if !strings.HasSuffix(rel, "_test.go") {
			changedLiveFiles = append(changedLiveFiles, rel)
		}
	}
	if len(pkgSet) == 0 {
		t.Logf("no watched packages changed relative to %s", base)
		return
	}
	var pkgs []string
	for pkg := range pkgSet {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)

	touched := map[string]bool{}
	if len(changedLiveFiles) > 0 {
		args := append([]string{"diff", "-U0", base, "--"}, changedLiveFiles...)
		diffOut, err := sprintGitOut(root, args...)
		require.NoError(t, err)
		changedLinesByFile := parseDiffChangedLines(diffOut)
		for _, rel := range changedLiveFiles {
			src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				continue
			}
			for k := range touchedFunctionsInSource(rel, src, changedLinesByFile[rel]) {
				touched[k] = true
			}
		}
	}

	profPath := filepath.Join(t.TempDir(), "coverage.out")
	args := append([]string{"test", "-count=1", "-timeout=120s", "-skip", "^TestTickDrainAndDealUnderTheGateAtLoad$", "-coverprofile=" + profPath}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), coverageActiveEnv+"=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go %s:\n%s", strings.Join(args, " "), string(out))

	profBytes, err := os.ReadFile(profPath)
	require.NoError(t, err)
	byFile := parseCoverProfileBlocks(string(profBytes), novaSprintModulePrefix)
	sources := map[string][]byte{}
	for rel := range byFile {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err == nil {
			sources[rel] = src
		}
	}
	headBytes, err := os.ReadFile(uncoveredFunctionsLedgerFile)
	require.NoError(t, err)
	_, allowed, _ := parseUncoveredFunctionsLedger(string(headBytes))
	for _, p := range evaluateCoverageAgainstLedger(sources, byFile, allowed, touched) {
		assert.Fail(t, p)
	}
}

func TestCoverageRefuserRefusesUncoveredCodeAndLedgerGrowth(t *testing.T) {
	t.Parallel()

	const sampleFile = "sample/widget.go"
	const sampleSrc = `package sample

type Widget struct{}

func Covered() int {
	return 1
}

func (w *Widget) GrandfatheredUntouched() int {
	return 2
}

func (w *Widget) GrandfatheredModified() int {
	return 3
}

func NewlyAddedUncovered() int {
	return 4
}
`
	sources := map[string][]byte{sampleFile: []byte(sampleSrc)}
	allowed := map[string]bool{
		sampleFile + " Widget.GrandfatheredUntouched": true,
		sampleFile + " Widget.GrandfatheredModified":  true,
	}

	diffText := `diff --git a/sample/widget.go b/sample/widget.go
--- a/sample/widget.go
+++ b/sample/widget.go
@@ -14,1 +14,1 @@
-	return 0
+	return 3
@@ -17,0 +17,3 @@
+func NewlyAddedUncovered() int {
+	return 4
+}
`
	changedLines := parseDiffChangedLines(diffText)
	touched := touchedFunctionsInSource(sampleFile, []byte(sampleSrc), changedLines[sampleFile])
	assert.True(t, touched[sampleFile+" Widget.GrandfatheredModified"])
	assert.True(t, touched[sampleFile+" NewlyAddedUncovered"])
	assert.False(t, touched[sampleFile+" Widget.GrandfatheredUntouched"])
	assert.False(t, touched[sampleFile+" Covered"])

	profile := `mode: set
github.com/mas-bandwidth/nova-sprint/sample/widget.go:5.20,7.2 1 1
github.com/mas-bandwidth/nova-sprint/sample/widget.go:9.47,11.2 1 0
github.com/mas-bandwidth/nova-sprint/sample/widget.go:13.46,15.2 1 0
github.com/mas-bandwidth/nova-sprint/sample/widget.go:17.32,19.2 1 0
`
	byFile := parseCoverProfileBlocks(profile, novaSprintModulePrefix)
	problems := evaluateCoverageAgainstLedger(sources, byFile, allowed, touched)
	assert.Equal(t, []string{
		sampleFile + " NewlyAddedUncovered: live non-test function has 0% test coverage and is not in " + uncoveredFunctionsLedgerRel + "; add a test that executes it",
		sampleFile + " Widget.GrandfatheredModified: live non-test function was modified by this change and still has 0% test coverage; add a test that executes it and remove its row from " + uncoveredFunctionsLedgerRel,
	}, problems)

	profileNowCovered := `mode: set
github.com/mas-bandwidth/nova-sprint/sample/widget.go:5.20,7.2 1 1
github.com/mas-bandwidth/nova-sprint/sample/widget.go:9.47,11.2 1 1
github.com/mas-bandwidth/nova-sprint/sample/widget.go:13.46,15.2 1 1
github.com/mas-bandwidth/nova-sprint/sample/widget.go:17.32,19.2 1 1
`
	staleProblems := evaluateCoverageAgainstLedger(sources, parseCoverProfileBlocks(profileNowCovered, novaSprintModulePrefix), allowed, touched)
	assert.Equal(t, []string{
		sampleFile + " Widget.GrandfatheredModified: function is executed by tests now; remove its stale row from " + uncoveredFunctionsLedgerRel + " (the ledger only shrinks)",
		sampleFile + " Widget.GrandfatheredUntouched: function is executed by tests now; remove its stale row from " + uncoveredFunctionsLedgerRel + " (the ledger only shrinks)",
	}, staleProblems)

	baseLedger := sampleFile + " Widget.GrandfatheredModified\n" + sampleFile + " Widget.GrandfatheredUntouched\n"
	shrunkLedger := sampleFile + " Widget.GrandfatheredUntouched\n"
	grownLedger := baseLedger + sampleFile + " NewlyAddedUncovered\n"
	assert.Empty(t, uncoveredLedgerGrowth(baseLedger, shrunkLedger))
	assert.Equal(t, []string{sampleFile + " NewlyAddedUncovered"}, uncoveredLedgerGrowth(baseLedger, grownLedger))
}
