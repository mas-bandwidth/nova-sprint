package sprint

import (
	"errors"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestATestThatDoesNotPinTheChangeIsReworkedBeforeARead tests that:
// 1. A test that passes at merge-base is reworked (pin absent)
// 2. A test that stays green when the change is reverted is reworked (pin broken)
// 3. An exported function with no non-test caller is reworked (reach)
func TestATestThatDoesNotPinTheChangeIsReworkedBeforeARead(t *testing.T) {
	tests := []struct {
		name     string
		input    GateLintInput
		run      BenchRunner
		expected []GateLintFinding
	}{
		{
			name: "empty input returns nothing",
			input: GateLintInput{
				MergeBase: "",
				Head:      "",
				TestPkg:   "",
				TestName:  "",
			},
			expected: nil,
		},
		{
			name: "test pkg without test name returns nothing",
			input: GateLintInput{
				MergeBase: "abc123",
				Head:      "def456",
				TestPkg:   "internal/sprint",
				TestName:  "",
			},
			expected: nil,
		},
		{
			name: "test passes at merge-base (pin absent)",
			input: GateLintInput{
				MergeBase: "abc123",
				Head:      "def456",
				TestPkg:   "internal/sprint",
				TestName:  "TestSomething",
			},
			run: func(commit, pkg, testname string) (bool, error) {
				// Test passes at merge-base - this is a finding
				return true, nil
			},
			expected: []GateLintFinding{{What: GateLintPinAbsent + ": test passes at merge-base"}},
		},
		{
			name: "absent at base, green at head, red in reverted tree is accepted",
			input: GateLintInput{
				MergeBase:    "abc123",
				Head:         "def456",
				RevertedHead: "reverted789",
				TestPkg:      "internal/sprint",
				TestName:     "TestSomething",
			},
			run: func(commit, pkg, testname string) (bool, error) {
				// Absence at base is permitted; the probe must use the materialized
				// reverted tree rather than head before it accepts the pin.
				if commit == "abc123" {
					return false, errors.New("test absent at base")
				}
				if commit == "reverted789" {
					return false, nil
				}
				return true, nil // head is green, but must not be used for this probe
			},
			expected: nil,
		},
		{
			name:  "reverted tree green is a broken pin",
			input: GateLintInput{MergeBase: "abc123", Head: "def456", RevertedHead: "reverted789", TestPkg: "internal/sprint", TestName: "TestSomething"},
			run: func(commit, pkg, testname string) (bool, error) {
				if commit == "abc123" {
					return false, errors.New("test absent at base")
				}
				return commit == "reverted789", nil
			},
			expected: []GateLintFinding{{What: GateLintPinBroken + ": test passes with non-test hunks reverted"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GateLintFindings(tt.input, tt.run)
			if len(got) != len(tt.expected) {
				t.Errorf("GateLintFindings() returned %d findings, want %d", len(got), len(tt.expected))
				for i, f := range got {
					t.Logf("  [%d] %s", i, f.What)
				}
				return
			}
			for i, f := range got {
				if f.What != tt.expected[i].What {
					t.Errorf("GateLintFindings()[%d] = %q, want %q", i, f.What, tt.expected[i].What)
				}
			}
		})
	}
}

func TestFindNonTestReferencesDoesNotCountDeclaration(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/api.go"
	if err := os.WriteFile(path, []byte("package api\nfunc Exported() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reached, err := FindNonTestReferences(dir, []string{path}, map[string]bool{"Exported": true})
	if err != nil {
		t.Fatal(err)
	}
	if reached["Exported"] {
		t.Fatal("declaration must not count as a non-test reference")
	}
}

func TestChangedExportedSymbolsReadsRelativePathUnderChangedDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/sub", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/sub/api.go", []byte("package sub\nfunc Unwired() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := getChangedExportedSymbols(dir, []string{"sub/api.go"})
	if !got["Unwired"] {
		t.Fatal("relative changed file was not read under ChangedDir")
	}
}

func TestGateLintReachDoesNotUseAnotherPackage(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []struct{ name, src string }{
		{"one/api.go", "package one\nfunc Unwired() {}\n"},
		{"two/use.go", "package two\nfunc use() { Unwired() }\n"},
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file.name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file.name), []byte(file.src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := GateLintFindings(GateLintInput{MergeBase: "base", RevertedHead: "reverted", TestPkg: "./one", TestName: "TestPin", ChangedDir: dir, ChangedFiles: []string{"one/api.go"}}, func(commit, pkg, test string) (bool, error) { return false, nil })
	if len(got) != 1 || !strings.Contains(got[0].What, "Unwired") {
		t.Fatalf("got %#v, want unreached Unwired", got)
	}
}

// TestFindNonTestReferences tests that we can find references to symbols.
func TestFindNonTestReferences(t *testing.T) {
	symbols := map[string]bool{
		"GateLintFindings": true,
		"ParseTestLine":    true,
	}

	// Test with empty input - initially all symbols are unreached
	reached, err := FindNonTestReferences("", nil, symbols)
	if err != nil {
		t.Errorf("FindNonTestReferences() error = %v", err)
	}
	if reached["GateLintFindings"] || reached["ParseTestLine"] {
		t.Errorf("FindNonTestReferences() with empty input returned true, want false")
	}
}

// TestGetExportedDecls tests that we can extract exported declarations.
func TestGetExportedDecls(t *testing.T) {
	src := []byte(`
package test
func ExportedFunc() {}
func unexported() {}
type ExportedType struct{}
type unexported struct{}
// Method on struct
func (e ExportedType) Method() {}
func (u unexported) unexportedMethod() {}
`)
	fset := token.NewFileSet()
	decls := GetExportedDecls(src, fset)
	// Should find: ExportedFunc, ExportedType, Method
	if len(decls) != 3 {
		t.Errorf("GetExportedDecls() returned %d decls, want 3", len(decls))
	}
	if _, ok := decls["ExportedFunc"]; !ok {
		t.Error("GetExportedDecls() missing ExportedFunc")
	}
	if _, ok := decls["ExportedType"]; !ok {
		t.Error("GetExportedDecls() missing ExportedType")
	}
	if _, ok := decls["Method"]; !ok {
		t.Error("GetExportedDecls() missing Method")
	}
}

// TestGateLintFindingString tests the string representation.
func TestGateLintFindingString(t *testing.T) {
	finding := GateLintFinding{What: "test finding"}
	got := GateLintFindingString(finding)
	want := "machine gate: test finding"
	if got != want {
		t.Errorf("GateLintFindingString() = %q, want %q", got, want)
	}
}

// TestParseTestLine tests parsing of TEST lines from briefs.
func TestParseTestLine(t *testing.T) {
	tests := []struct {
		name     string
		brief    string
		wantPkg  string
		wantName string
	}{
		{
			name:     "simple test line",
			brief:    "TEST: internal/sprint TestSomething",
			wantPkg:  "internal/sprint",
			wantName: "TestSomething",
		},
		{
			name:     "test line with package only",
			brief:    "TEST: internal/sprint",
			wantPkg:  "internal/sprint",
			wantName: "",
		},
		{
			name:     "test line none",
			brief:    "TEST: none",
			wantPkg:  "",
			wantName: "",
		},
		{
			name:     "multiline brief with TEST",
			brief:    "CARD: test\nTEST: internal/sprint TestFoo\nPATHS: internal/sprint",
			wantPkg:  "internal/sprint",
			wantName: "TestFoo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, name := ParseTestLine(tt.brief)
			if pkg != tt.wantPkg || name != tt.wantName {
				t.Errorf("ParseTestLine() = (%q, %q), want (%q, %q)", pkg, name, tt.wantPkg, tt.wantName)
			}
		})
	}
}
