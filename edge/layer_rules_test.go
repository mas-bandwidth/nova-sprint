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

// Layer rules checked against the package's own source.

func layerSources_internal(t *testing.T, tests bool) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range []string{".", "../cmd/nova-worker"} {
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

// No dead code: each name below appears more than once in the layer's
// non-test Go (its declaration and at least one use).
func TestDeadCode(t *testing.T) {
	t.Parallel()
	src := layerSources_internal(t, false)
	for _, name := range []string{"FormatTakeOpID", "RunArgv", "JobCIWait", "JobCleaned", "HasHead", "RestedUntil", "NewClient"} {
		re := regexp.MustCompile(`\b` + name + `\b`)
		n := 0
		for _, s := range src {
			n += len(re.FindAllStringIndex(s, -1))
		}
		if n <= 1 {
			t.Errorf("%s is declared and never used in non-test code", name)
		}
	}
}

// Fakes live in _test.go files: FakeExec must not be defined in edge/exec.go.
func TestFakeExecShipsInProduction(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("exec.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "type FakeExec struct") {
		t.Errorf("edge/exec.go (non-test) defines FakeExec")
	}
}

// The package's random walks emit traces projected from the code's state, so
// each walk must drive the real entry points: the landing walk calls LandPass
// and the runner walk calls RunConsumer, StepRow and RecoverRow. A walk that
// reimplements the steps checks only the test's own copy.
func TestTracesDoNotDriveTheCode(t *testing.T) {
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

var exemptFromUnitTier = map[string]bool{
	"functional_test.go":      true,
	"exec_process_test.go":    true,
	"layer_rules_test.go":     true,
	"harness_sandbox_test.go": true,
}

// Unit tests take no real time, open no socket and start no process; anything
// needing the real thing is a functional test. No Test function that is not
// named Functional (so `go test -skip Functional` runs it) may start a real
// process or sleep on the wall clock. The files in exemptFromUnitTier are
// exempt: they drive real processes by design, or contain the patterns this
// test searches for.
func TestUnitTierRunsRealProcessesAndTime(t *testing.T) {
	t.Parallel()
	real := regexp.MustCompile(`NewOSExec\(\)|time\.Sleep\(|exec\.Command\(|exec\.CommandContext\(|ListenLoopback|New\(Config\{\}\)\(`)
	fset := token.NewFileSet()
	for p, s := range layerSources_internal(t, true) {
		base := filepath.Base(p)
		if exemptFromUnitTier[base] {
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
