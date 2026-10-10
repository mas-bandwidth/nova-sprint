package sprint

import (
	"context"
	"errors"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
)

func findingsForTest(t *testing.T, input GateLintInput, run BenchRunner) []GateLintFinding {
	t.Helper()
	if input.BaseDir == "" {
		input.BaseDir = t.TempDir()
	}
	got, err := GateLintFindingsChecked(input, run)
	if err != nil {
		t.Fatalf("GateLintFindingsChecked: %v", err)
	}
	return got
}

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
					return false, nil // absent tests are mapped to false by benchTestRunner
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
					return false, nil
				}
				return commit == "reverted789", nil
			},
			expected: []GateLintFinding{{What: GateLintPinBroken + ": test passes with non-test hunks reverted"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findingsForTest(t, tt.input, tt.run)
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

func TestGateLintProbeErrorsAreNotTreatedAsRedTests(t *testing.T) {
	t.Parallel()
	input := GateLintInput{MergeBase: "base", RevertedHead: "reverted", TestPkg: "./pkg", TestName: "TestPin"}
	for _, failingProbe := range []string{"base", "reverted"} {
		t.Run(failingProbe, func(t *testing.T) {
			calls := 0
			findings, err := GateLintFindingsChecked(input, func(commit, _, _ string) (bool, error) {
				calls++
				if commit == failingProbe {
					return false, errors.New("bench unavailable")
				}
				return false, nil
			})
			if err == nil || !strings.Contains(err.Error(), "bench unavailable") {
				t.Fatalf("probe error was not propagated: findings=%v err=%v", findings, err)
			}
			if failingProbe == "base" && calls != 1 {
				t.Fatalf("base error should stop probes; calls=%d", calls)
			}
			if failingProbe == "reverted" && calls != 2 {
				t.Fatalf("reverted probe should run after base; calls=%d", calls)
			}
		})
	}
}

func TestGateLintReachIgnoresShadowedLocal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api.go"), []byte("package api\nfunc Exported() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local.go"), []byte("package api\nfunc unrelated() { Exported := 1; _ = Exported }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reached, err := findNonTestReferences(dir, map[string]bool{"Exported": true})
	if err != nil {
		t.Fatal(err)
	}
	if reached["Exported"] {
		t.Fatal("shadowed local identifier counted as a caller")
	}
}

func TestGateLintRunScansPinnedHeadNotLanderCheckout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "init.defaultBranch=main"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "base.go"), []byte("package api\nfunc Existing() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "base.go")
	git("commit", "-q", "-m", "base")
	base := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "api.go"), []byte("package api\nfunc Unwired() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "api.go")
	git("commit", "-q", "-m", "candidate")
	head := git("rev-parse", "HEAD")
	git("checkout", "-q", base) // the lander clone deliberately remains at the base
	diff := "diff --git a/api.go b/api.go\nnew file mode 100644\n--- /dev/null\n+++ b/api.go\n@@ -0,0 +1 @@\n+func Unwired() {}\n"
	got, err := GateLintRun(WorkView{Head: head, Diff: diff}, cardhdr.TestLine{Package: "./api", Name: "TestPin"}, base, dir,
		[]string{"linux"}, func(context.Context, []string, string, string, []string) (BenchResult, error) {
			return BenchResult{Code: 1}, nil
		}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].What, "Unwired") {
		t.Fatalf("pinned head addition was not checked: %#v", got)
	}
}

func TestGateLintRunTypeChecksStdlibCrossingReferences(t *testing.T) {
	t.Setenv("GOROOT", "") // the wall binary may have no GOROOT embedded
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "init.defaultBranch=main"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	write := func(name, src string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/gatelint\n\ngo 1.26\n")
	write("one/base.go", "package one\n")
	git("add", "go.mod", "one/base.go")
	git("commit", "-q", "-m", "base")
	base := git("rev-parse", "HEAD")
	write("one/api.go", "package one\nfunc Exported() {}\n")
	write("two/use.go", `package two
import (
    "context"
    "time"
    "example.test/gatelint/one"
)
func ExportedUse() {}
func use() {
    _, _ = context.WithTimeout(context.Background(), time.Second)
    one.Exported()
    ExportedUse()
}
`)
	git("add", "one/api.go", "two/use.go")
	git("commit", "-q", "-m", "candidate")
	head := git("rev-parse", "HEAD")
	diff := git("diff", base, head)
	legacy, err := findNonTestReferences(filepath.Join(dir, "two"), map[string]bool{"ExportedUse": true})
	if err != nil || !legacy["ExportedUse"] {
		t.Fatalf("legacy package scan did not preserve stdlib imports: reached=%v err=%v", legacy, err)
	}
	git("checkout", "-q", base)
	got, err := GateLintRun(WorkView{Head: head, Diff: diff}, cardhdr.TestLine{}, base, dir, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("GateLintRun could not type-check the pinned tree's standard-library crossing: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("stdlib-crossing caller did not satisfy reach: %#v", got)
	}
}

func TestGateLintTypeCheckFailureIsAnExplicitFinding(t *testing.T) {
	t.Parallel()
	base, head := t.TempDir(), t.TempDir()
	for _, root := range []string{base, head} {
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/blocker\n\ngo 1.26\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "api.go"), []byte("package api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(head, "api.go"), []byte("package api\nfunc Unwired() { missing() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := GateLintFindingsChecked(GateLintInput{BaseDir: base, ChangedDir: head, ChangedFiles: []string{"api.go"}}, nil)
	if err != nil {
		t.Fatalf("source analysis failure must not become a waiting error: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0].What, "reference analysis could not complete") {
		t.Fatalf("source analysis failure was not named as a gate finding: %#v", got)
	}
}

func TestFindNonTestReferencesDoesNotCountDeclaration(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/api.go"
	if err := os.WriteFile(path, []byte("package api\nfunc Exported() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reached, err := findNonTestReferencesInFiles(dir, []string{path}, map[string]bool{"Exported": true})
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

func TestGateLintReachAcceptsReferenceFromAnotherPackage(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []struct{ name, src string }{
		{"go.mod", "module example.test/tree\n\ngo 1.23\n"},
		{"one/api.go", "package one\nfunc Exported() {}\n"},
		{"two/use.go", "package two\nimport \"example.test/tree/one\"\nfunc use() { one.Exported() }\n"},
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file.name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file.name), []byte(file.src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := findingsForTest(t, GateLintInput{MergeBase: "base", RevertedHead: "reverted", TestPkg: "./one", TestName: "TestPin", ChangedDir: dir, ChangedFiles: []string{"one/api.go"}}, func(commit, pkg, test string) (bool, error) { return false, nil })
	if len(got) != 0 {
		t.Fatalf("cross-package semantic reference was reported: %#v", got)
	}
}

func TestGateLintReachDoesNotBorrowSameNamedReferenceFromOtherPackage(t *testing.T) {
	base, head := t.TempDir(), t.TempDir()
	for _, root := range []string{base, head} {
		if err := os.MkdirAll(filepath.Join(root, "one"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "two"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/same\n\ngo 1.23\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "two/api.go"), []byte("package two\nfunc Exported() {}\nfunc use() { Exported() }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(head, "one/api.go"), []byte("package one\nfunc Exported() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := findingsForTest(t, GateLintInput{BaseDir: base, ChangedDir: head, ChangedFiles: []string{"one/api.go"}}, nil)
	if len(got) != 1 || !strings.Contains(got[0].What, "Exported") {
		t.Fatalf("same-named reference from other package was borrowed: %#v", got)
	}
}

func TestGateLintReachChecksOnlyDeclarationsAddedByChange(t *testing.T) {
	base, head := t.TempDir(), t.TempDir()
	for _, root := range []string{base, head} {
		if err := os.WriteFile(filepath.Join(root, "api.go"), []byte("package api\nfunc Existing() {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(head, "api.go"), []byte("package api\nfunc Existing() {}\nfunc Added() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(head, "use.go"), []byte("package api\nfunc use() { Existing() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := findingsForTest(t, GateLintInput{MergeBase: "base", RevertedHead: "rev", TestPkg: "./api", TestName: "TestPin", BaseDir: base, ChangedDir: head, ChangedFiles: []string{"api.go"}}, func(string, string, string) (bool, error) { return false, nil })
	if len(got) != 1 || !strings.Contains(got[0].What, "Added") {
		t.Fatalf("got %#v, want only newly-added Added", got)
	}
}

func TestGateLintReachStillRunsWithoutTestLine(t *testing.T) {
	base, head := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(head, "api.go"), []byte("package api\nfunc Unwired() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := findingsForTest(t, GateLintInput{BaseDir: base, ChangedDir: head, ChangedFiles: []string{"api.go"}}, nil)
	if len(got) != 1 || !strings.Contains(got[0].What, "Unwired") {
		t.Fatalf("reach analysis without a TEST line = %#v", got)
	}
}

func TestGateLintVerbTableReachesNewRunMethod(t *testing.T) {
	base, head := t.TempDir(), t.TempDir()
	for _, root := range []string{base, head} {
		if err := os.MkdirAll(filepath.Join(root, "cmd/nova-sprint"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	old := "package main\ntype verb struct{name, syntax, example string; run any}\nvar verbs = []verb{{\"old\", \"\", \"\", (*app).cmdOld}}\ntype app struct{}\nfunc (*app) cmdOld() {}\n"
	newsrc := "package main\ntype verb struct{name, syntax, example string; run any}\nvar verbs = []verb{{\"old\", \"\", \"\", (*app).cmdOld},{\"fresh\", \"\", \"\", (*app).cmdFresh}}\ntype app struct{}\nfunc (*app) cmdOld() {}\nfunc (*app) cmdFresh() {}\n"
	for root, src := range map[string]string{base: old, head: newsrc} {
		if err := os.WriteFile(filepath.Join(root, "cmd/nova-sprint/verbs.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := addedVerbRows(base, head, []string{"cmd/nova-sprint/verbs.go"})
	if err != nil {
		t.Fatal(err)
	}
	if rows["fresh"] != "cmdFresh" || len(rows) != 1 {
		t.Fatalf("new verb rows = %#v", rows)
	}
	if !hasMethod(head, "cmdFresh") {
		t.Fatal("new verb run method was not found")
	}
}

// TestGateLintReachAcceptsAWiredSymbol is the sound attempt: an exported function the
// change adds with a caller in a non-test file of its own package is not a finding, so
// the reach check refuses dead code without refusing wired code.
func TestGateLintReachAcceptsAWiredSymbol(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []struct{ name, src string }{
		{"api.go", "package api\nfunc Wired() {}\n"},
		{"use.go", "package api\nfunc use() { Wired() }\n"},
	} {
		if err := os.WriteFile(filepath.Join(dir, file.name), []byte(file.src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := findingsForTest(t, GateLintInput{MergeBase: "base", RevertedHead: "reverted", TestPkg: "./api", TestName: "TestPin", ChangedDir: dir, ChangedFiles: []string{"api.go"}}, func(commit, pkg, test string) (bool, error) { return false, nil })
	if len(got) != 0 {
		t.Fatalf("a wired symbol was flagged: %#v", got)
	}
}

// TestFindNonTestReferences tests that we can find references to symbols.
func TestFindNonTestReferences(t *testing.T) {
	symbols := map[string]bool{
		"GateLintFindings": true,
		"ParseTestLine":    true,
	}

	// Test with empty input - initially all symbols are unreached
	reached, err := findNonTestReferencesInFiles("", nil, symbols)
	if err != nil {
		t.Errorf("findNonTestReferencesInFiles() error = %v", err)
	}
	if reached["GateLintFindings"] || reached["ParseTestLine"] {
		t.Errorf("findNonTestReferencesInFiles() with empty input returned true, want false")
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
	decls := getExportedDecls(src, fset)
	// Should find: ExportedFunc, ExportedType, Method
	if len(decls) != 3 {
		t.Errorf("getExportedDecls() returned %d decls, want 3", len(decls))
	}
	if _, ok := decls["ExportedFunc"]; !ok {
		t.Error("getExportedDecls() missing ExportedFunc")
	}
	if _, ok := decls["ExportedType"]; !ok {
		t.Error("getExportedDecls() missing ExportedType")
	}
	if _, ok := decls["Method"]; !ok {
		t.Error("getExportedDecls() missing Method")
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
			pkg, name := parseTestLine(tt.brief)
			if pkg != tt.wantPkg || name != tt.wantName {
				t.Errorf("parseTestLine() = (%q, %q), want (%q, %q)", pkg, name, tt.wantPkg, tt.wantName)
			}
		})
	}
}
