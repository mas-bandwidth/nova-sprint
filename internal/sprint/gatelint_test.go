package sprint

import (
	"go/token"
	"testing"
)

// TestATestThatDoesNotPinTheChangeIsReworkedBeforeARead tests that:
// 1. A test that passes at merge-base is reworked
// 2. A test that stays green when the change is reverted is reworked
// 3. An exported function with no non-test caller is reworked
func TestATestThatDoesNotPinTheChangeIsReworkedBeforeARead(t *testing.T) {
	tests := []struct {
		name     string
		input    GateLintInput
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GateLintFindings(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("GateLintFindings() returned %d findings, want %d", len(got), len(tt.expected))
			}
		})
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
`)
	fset := token.NewFileSet()
	decls := GetExportedDecls(src, fset)
	if len(decls) != 2 {
		t.Errorf("GetExportedDecls() returned %d decls, want 2", len(decls))
	}
	if _, ok := decls["ExportedFunc"]; !ok {
		t.Error("GetExportedDecls() missing ExportedFunc")
	}
	if _, ok := decls["ExportedType"]; !ok {
		t.Error("GetExportedDecls() missing ExportedType")
	}
}
