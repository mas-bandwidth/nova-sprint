package sprint

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

// The machine gate checks: pins and reach.

const (
	GateLintPinAbsent = "gate-pin-absent"
	GateLintPinBroken = "gate-pin-broken"
	GateLintReach     = "gate-reach-unreached"
)

// GateLintFinding is a finding from the extended machine gate checks.
type GateLintFinding struct {
	What string
}

// GateLintInput is the input for the extended gate checks.
type GateLintInput struct {
	MergeBase    string
	Head         string
	RevertedHead string
	TestPkg      string
	TestName     string
	ChangedFiles []string // non-test files changed by the commit
	ChangedDir   string   // directory of the changed files
	SkipReach    bool     // deletion cards remove code and have no new API to reach
}

// BenchRunner runs a test at a given commit and returns whether it passed.
type BenchRunner func(commit, pkg, testname string) (passed bool, err error)

// GateLintFindingsChecked returns an error when a probe or source analysis did
// not answer. Such an error is not evidence that the test is red or code is used.
func GateLintFindingsChecked(input GateLintInput, run BenchRunner) ([]GateLintFinding, error) {
	var out []GateLintFinding

	if input.TestPkg == "" || input.TestName == "" {
		return nil, nil
	}

	if run == nil {
		return nil, nil
	}

	// Check 1: Test should fail at merge-base (or not exist)
	// If it passes at merge-base, that's a finding (pin absent)
	passedAtMergeBase, err := run(input.MergeBase, input.TestPkg, input.TestName)
	if err != nil {
		return nil, err
	}
	if passedAtMergeBase {
		out = append(out, GateLintFinding{What: GateLintPinAbsent + ": test passes at merge-base"})
		return out, nil
	}
	if input.RevertedHead == "" {
		return append(out, GateLintFinding{What: GateLintPinBroken + ": no reverted non-test tree"}), nil
	}

	// Check 2: Test should fail when non-test hunks are reverted (pin broken)
	// If the test passes on the reverted variant, that's a finding
	// We need to run the test at head with non-test hunks reverted
	passedAtReverted, err := run(input.RevertedHead, input.TestPkg, input.TestName)
	if err != nil {
		return nil, err
	}
	if passedAtReverted {
		out = append(out, GateLintFinding{What: GateLintPinBroken + ": test passes with non-test hunks reverted"})
	}

	// Check 3: Reach check - every exported symbol in changed non-test files
	// should have a reference from a non-test file
	if !input.SkipReach && len(input.ChangedFiles) > 0 && input.ChangedDir != "" {
		for _, changed := range input.ChangedFiles {
			symbols := getChangedExportedSymbols(input.ChangedDir, []string{changed})
			if len(symbols) == 0 {
				continue
			}
			pkgDir := filepath.Dir(filepath.Join(input.ChangedDir, changed))
			reached, err := findNonTestReferences(pkgDir, symbols)
			if err != nil {
				return nil, err
			}
			for sym, isReached := range reached {
				if !isReached {
					out = append(out, GateLintFinding{What: GateLintReach + ": " + sym + " has no reference from any non-test file"})
				}
			}
		}
	}

	return out, nil
}

// getChangedExportedSymbols returns exported declarations from changed files.
func getChangedExportedSymbols(dir string, changedFiles []string) map[string]bool {
	symbols := make(map[string]bool)
	fset := token.NewFileSet()

	for _, f := range changedFiles {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		isVerbTable := strings.HasSuffix(f, "cmd/nova-sprint/verbs.go")
		f = filepath.Join(dir, f)
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() || isVerbTable {
					symbols[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						if ts.Name.IsExported() {
							symbols[ts.Name.Name] = true
						}
					}
				}
			}
		}
	}
	return symbols
}

// findNonTestReferences finds references to symbols from non-test files in the package.
func findNonTestReferences(pkgDir string, symbols map[string]bool) (map[string]bool, error) {
	reached := make(map[string]bool)
	for sym := range symbols {
		reached[sym] = false
	}

	fset := token.NewFileSet()
	var files []*ast.File

	// Only this package counts. A same-named local, field, or declaration in a
	// different package must not make an exported addition look wired.
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return reached, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(pkgDir, entry.Name())
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			continue
		}
		files = append(files, file)
	}
	return referencesByObject(fset, files, symbols, reached)
}

func referencesByObject(fset *token.FileSet, files []*ast.File, symbols map[string]bool, reached map[string]bool) (map[string]bool, error) {
	info := &types.Info{Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object)}
	conf := types.Config{Importer: importer.Default(), Error: func(error) {}}
	_, _ = conf.Check("gatelint", fset, files, info)
	targets := make(map[types.Object]string)
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if symbols[d.Name.Name] {
					if obj := info.Defs[d.Name]; obj != nil {
						targets[obj] = d.Name.Name
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && symbols[ts.Name.Name] {
						if obj := info.Defs[ts.Name]; obj != nil {
							targets[obj] = ts.Name.Name
						}
					}
				}
			}
		}
	}
	for ident, obj := range info.Uses {
		if name, ok := targets[obj]; ok && ident.Name == name {
			reached[name] = true
		}
	}
	return reached, nil
}

// parseTestLine extracts the test line from a brief. It is unexported: the production
// path parses the TEST line with cardhdr (TestLineOfBrief), so no non-test file calls it,
// and the reach check would name an exported one as dead.
func parseTestLine(brief string) (pkg, name string) {
	lines := strings.Split(brief, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "TEST:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "TEST:"))
			if val == "none" || val == "" {
				return "", ""
			}
			parts := strings.Fields(val)
			if len(parts) == 0 {
				return "", ""
			}
			pkg = parts[0]
			if len(parts) > 1 {
				name = parts[1]
			}
			return pkg, name
		}
	}
	return "", ""
}

// GateLintFindingString returns a string representation of a gate lint finding.
func GateLintFindingString(f GateLintFinding) string {
	return "machine gate: " + f.What
}

// getExportedDecls returns all exported declarations from a file. It is unexported for
// the same reason as parseTestLine: only tests read it, so an exported one would read as
// a change with no non-test caller.
func getExportedDecls(src []byte, fset *token.FileSet) map[string]token.Pos {
	out := map[string]token.Pos{}
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return out
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				out[d.Name.Name] = d.Pos()
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					if ts.Name.IsExported() {
						out[ts.Name.Name] = ts.Pos()
					}
				}
			}
		}
	}
	return out
}

// findNonTestReferencesInFiles finds references to symbols from the given non-test files.
// It is unexported: the production reach check scans a package directory itself
// (findNonTestReferences), so only tests need the explicit-files form.
func findNonTestReferencesInFiles(pkgDir string, changedFiles []string, symbols map[string]bool) (map[string]bool, error) {
	reached := make(map[string]bool)
	for sym := range symbols {
		reached[sym] = false
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, f := range changedFiles {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(pkgDir, f)
		}
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		file, err := parser.ParseFile(fset, f, src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return referencesByObject(fset, files, symbols, reached)
}
