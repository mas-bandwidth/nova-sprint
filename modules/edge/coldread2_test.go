package edge

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Cold read 2, 2026-11-06, at ef4df30c (Rowan's cold reader). One failing
// probe per confirmed defect. These read the layer's own source and docs.

func cr2Sources_internal(t *testing.T, tests bool) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range []string{".", "../../cmd/nova-worker"} {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") != tests {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				t.Fatal(rerr)
			}
			out[p] = string(b)
			return nil
		})
	}
	return out
}

// Build rules: "no dead code"; round 2 ruling: RunRemoteLandStep "is removed
// if unused". A name below that appears exactly once in the layer's non-test
// Go is its declaration alone; one moved to a test file or deleted is gone.
func TestColdread2DefectDeadCode(t *testing.T) {
	t.Parallel()
	src := cr2Sources_internal(t, false)
	for _, name := range []string{"FormatTakeOpID", "RunArgv", "JobCIWait", "JobCleaned", "HasHead", "RestedUntil", "NewClient"} {
		re := regexp.MustCompile(`\b` + name + `\b`)
		n := 0
		for _, s := range src {
			n += len(re.FindAllStringIndex(s, -1))
		}
		if n == 1 {
			t.Errorf("%s is declared and never used in non-test code", name)
		}
	}
}

// Round 2 ruling (size): "Fakes move to _test.go files". FakeExec, its
// handlers and its outcome queue ship in modules/edge/exec.go.
func TestColdread2DefectFakeExecShipsInProduction(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("exec.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "type FakeExec struct") {
		t.Errorf("edge/exec.go (non-test) defines FakeExec")
	}
}

// Round 2 ruling: "the Built section names only functions that exist". Built
// names Lander, Execute, MergeLedgerLines and StoreSprintClient
// (docs/LAYER-4-EDGES.md:457, 474); none is defined.
func TestColdread2DefectBuiltNamesMissingFunctions(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile("../../docs/LAYER-4-EDGES.md")
	if err != nil {
		t.Fatal(err)
	}
	all := ""
	for _, s := range cr2Sources_internal(t, false) {
		all += s
	}
	for _, name := range []string{"Lander", "MergeLedgerLines", "StoreSprintClient"} {
		if strings.Contains(string(doc), "`"+name) && !regexp.MustCompile(`(func|type) (\([^)]*\) )?`+name+`\b`).MatchString(all) {
			t.Errorf("Built names %s; no such function or type exists", name)
		}
	}
}

// Build rules, added 2026-11-07: "the package's random walk emits traces ...
// projected from the code's state". The three walks drive their own copies
// of the steps: the landing walk
// drives git.Fake primitives (never LandPass), the runner walk writes
// job.json by hand (never RunConsumer, StepRow or RecoverRow). The no-forge
// path of LandPass and the pid the runner records are therefore outside every
// trace check. (The push walk went with the edge pusher, drift row 6.)
func TestColdread2DefectTracesDoNotDriveTheCode(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"git/trace_test.go":    {"LandPass("},
		"worker/trace_test.go": {"RunConsumer(", "StepRow(", "RecoverRow("},
	}
	for file, entry := range cases {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range entry {
			found = found || strings.Contains(string(b), e)
		}
		if !found {
			t.Errorf("%s never calls %v: the trace is of the test's own steps, not the code's", file, entry)
		}
	}
}

// Build rules: "Unit tests take no real time, open no socket ... Anything
// needing the real thing is a functional test." These Test functions are
// not named Functional (so `go test -skip Functional` runs them as unit
// tests) yet start real processes or sleep on the wall clock. Cold read 1 probes
// (coldread_test.go) and breaker probes (break_test.go) are left out.
func TestColdread2DefectUnitTierRunsRealProcessesAndTime(t *testing.T) {
	t.Parallel()
	real := regexp.MustCompile(`NewOSExec\(\)|time\.Sleep\(|exec\.Command\(|exec\.CommandContext\(|ListenLoopback|New\(Config\{\}\)\(`)
	fset := token.NewFileSet()
	for p, s := range cr2Sources_internal(t, true) {
		base := filepath.Base(p)
		if base == "functional_test.go" || strings.HasPrefix(base, "coldread") || base == "break_test.go" {
			continue
		}
		f, err := parser.ParseFile(fset, p, s, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "Test") || strings.Contains(fn.Name.Name, "Functional") {
				continue
			}
			body := s[fset.Position(fn.Pos()).Offset:fset.Position(fn.End()).Offset]
			if real.MatchString(body) {
				t.Errorf("%s: %s runs in the unit tier with a real process or the wall clock", p, fn.Name.Name)
			}
		}
	}
}
