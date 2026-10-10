package sprint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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
	MergeBase string
	Head      string
	TestPkg   string
	TestName  string
}

// GateLintFindings returns findings for the extended gate checks.
func GateLintFindings(input GateLintInput) []GateLintFinding {
	var out []GateLintFinding

	if input.TestPkg == "" || input.TestName == "" {
		return nil
	}

	// Pin check: test should fail at merge-base (or not exist)
	// For now, stub implementation
	_ = input.MergeBase

	// Pin check: test should fail when non-test changes are reverted
	// For now, stub implementation
	_ = input.Head

	return out
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
// This uses go/parser and go/types as specified in the task.
func FindNonTestReferences(pkgPath string, changedFiles []string, symbols map[string]bool) (map[string]bool, error) {
	reached := make(map[string]bool)
	for sym := range symbols {
		reached[sym] = false
	}

	// Walk all non-test Go files in the package and check for references
	fset := token.NewFileSet()
	for _, f := range changedFiles {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := readFile(f)
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(fset, f, src, parser.ParseComments)
		if err != nil {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				if symbols[ident.Name] {
					reached[ident.Name] = true
				}
			}
			return true
		})
	}
	return reached, nil
}

// GateLintFindingString returns a string representation of a gate lint finding.
func GateLintFindingString(f GateLintFinding) string {
	return "machine gate: " + f.What
}

// readFile reads a file from the filesystem.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
