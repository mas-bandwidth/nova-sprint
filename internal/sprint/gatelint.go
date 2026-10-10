package sprint

import (
	"go/ast"
	"go/parser"
	"go/token"
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
}

// BenchRunner runs a test at a given commit and returns whether it passed.
type BenchRunner func(commit, pkg, testname string) (passed bool, err error)

// GateLintFindings returns findings for the extended gate checks.
func GateLintFindings(input GateLintInput, run BenchRunner) []GateLintFinding {
	var out []GateLintFinding

	if input.TestPkg == "" || input.TestName == "" {
		return nil
	}

	if run == nil {
		return nil
	}

	// Check 1: Test should fail at merge-base (or not exist)
	// If it passes at merge-base, that's a finding (pin absent)
	passedAtMergeBase, err := run(input.MergeBase, input.TestPkg, input.TestName)
	if err == nil && passedAtMergeBase {
		out = append(out, GateLintFinding{What: GateLintPinAbsent + ": test passes at merge-base"})
		return out
	}
	if input.RevertedHead == "" {
		return append(out, GateLintFinding{What: GateLintPinBroken + ": no reverted non-test tree"})
	}

	// Check 2: Test should fail when non-test hunks are reverted (pin broken)
	// If the test passes on the reverted variant, that's a finding
	// We need to run the test at head with non-test hunks reverted
	passedAtReverted, err := run(input.RevertedHead, input.TestPkg, input.TestName)
	if err == nil && passedAtReverted {
		out = append(out, GateLintFinding{What: GateLintPinBroken + ": test passes with non-test hunks reverted"})
	}

	// Check 3: Reach check - every exported symbol in changed non-test files
	// should have a reference from a non-test file
	if len(input.ChangedFiles) > 0 && input.ChangedDir != "" {
		symbols := getChangedExportedSymbols(input.ChangedDir, input.ChangedFiles)
		if len(symbols) > 0 {
			reached, err := findNonTestReferences(input.ChangedDir, symbols)
			if err == nil {
				for sym, isReached := range reached {
					if !isReached {
						out = append(out, GateLintFinding{What: GateLintReach + ": " + sym + " has no reference from any non-test file"})
					}
				}
			}
		}
	}

	return out
}

// getChangedExportedSymbols returns exported declarations from changed files.
func getChangedExportedSymbols(dir string, changedFiles []string) map[string]bool {
	symbols := make(map[string]bool)
	fset := token.NewFileSet()

	for _, f := range changedFiles {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
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
				if d.Name.IsExported() {
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

	// Walk all non-test Go files in the package directory
	filepath.Walk(pkgDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return nil
		}
		declarations := declaredNames(file)
		ast.Inspect(file, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				if symbols[ident.Name] && !declarations[ident.Pos()] {
					reached[ident.Name] = true
				}
			}
			return true
		})
		return nil
	})

	return reached, nil
}

// ParseTestLine extracts the test line from a brief.
func ParseTestLine(brief string) (pkg, name string) {
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

// GetExportedDecls returns all exported declarations from a file.
func GetExportedDecls(src []byte, fset *token.FileSet) map[string]token.Pos {
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

// FindNonTestReferences finds references to symbols from non-test files.
func FindNonTestReferences(pkgDir string, changedFiles []string, symbols map[string]bool) (map[string]bool, error) {
	reached := make(map[string]bool)
	for sym := range symbols {
		reached[sym] = false
	}

	fset := token.NewFileSet()

	// Walk all non-test Go files in the changed files and package directory
	for _, f := range changedFiles {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(fset, f, src, parser.ParseComments)
		if err != nil {
			continue
		}
		declarations := declaredNames(file)
		ast.Inspect(file, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				if symbols[ident.Name] && !declarations[ident.Pos()] {
					reached[ident.Name] = true
				}
			}
			return true
		})
	}

	return reached, nil
}

// declaredNames returns the identifier positions that introduce declarations.
// A declaration is not a non-test use: counting it makes an exported API look
// reached even when no production code refers to it.
func declaredNames(file *ast.File) map[token.Pos]bool {
	out := make(map[token.Pos]bool)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out[d.Name.Pos()] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					out[s.Name.Pos()] = true
				case *ast.ValueSpec:
					for _, name := range s.Names {
						out[name.Pos()] = true
					}
				}
			}
		}
	}
	return out
}
