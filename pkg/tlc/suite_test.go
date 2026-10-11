package tlc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// passOutput is a TLC output a pass case accepts, with state counts.
const passOutput = "Model checking completed. No error has been found.\n1 states generated, 1 distinct states found\n"

// passExec is a fake Executor: it writes a passing log and returns ExitPass.
func passExec(log string) int {
	if err := os.WriteFile(log, []byte(passOutput), 0o644); err != nil {
		return ExitNoStart
	}
	return ExitPass
}

// suiteTree writes a checkout with n independent pass cases and names them
// MCTiny00 .. MCTiny(n-1); it returns the root and the parsed plan.
func suiteTree(t *testing.T, n int) (string, []Case) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "tla")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("config\tmodule\texpected\tproperty\tdeadlock\tgroup\tgate\tdebt\n")
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("MCTiny%02d", i)
		fmt.Fprintf(&b, "%s.cfg\t%s.tla\tpass\t-\tcheck\ttiny\tbench\t-\n", name, name)
	}
	if err := os.WriteFile(filepath.Join(dir, CasesFile), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("MCTiny%02d", i)
		writeTestFile(t, filepath.Join(dir, name+".cfg"), "SPECIFICATION Spec\n")
		writeTestFile(t, filepath.Join(dir, name+".tla"),
			"---- MODULE "+name+" ----\nVARIABLE x\nInit == x = 0\nNext == x' = x\n====\n")
	}
	cases, err := LoadCases(root)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	return root, cases
}

// suiteOptions is one suite over root running exec with the given parallelism
// and budget; it returns the options and the output directory.
func suiteOptions(t *testing.T, root string, cases []Case, parallel int, budget time.Duration, exec Executor) (Options, string) {
	t.Helper()
	out := t.TempDir()
	platform, err := Platform("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	return Options{
		Root: root, Cases: cases,
		Jar:  Jar{Path: "/nonexistent/tla2tools.jar", SHA256: strings.Repeat("a", 64)},
		Java: "java", JavaVer: "21.0.1",
		Out: out, Budget: budget, Workers: 2, Parallel: parallel,
		Platform: platform, CPUs: 16,
		Exec: exec,
	}, out
}

// TestSuiteRunsCasesConcurrentlyAtParallel pins the card's rule that a suite's
// independent cases run at the same time: with Parallel 4 and 8 cases whose
// fake executor holds until four are running, the suite finishes and the peak
// concurrency reaches four. It is red on the serial loop, which never has two
// cases running.
func TestSuiteRunsCasesConcurrentlyAtParallel(t *testing.T) {
	root, cases := suiteTree(t, 8)
	var running, peak atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	exec := func(ctx context.Context, r Run, log string) int {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		if n >= 4 {
			once.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		running.Add(-1)
		return passExec(log)
	}
	o, _ := suiteOptions(t, root, cases, 4, 110*time.Second, exec)
	res, err := RunSuite(o)
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if res.Failed {
		t.Fatalf("a suite of passing cases failed: %+v", res.Records)
	}
	if got := peak.Load(); got < 4 {
		t.Fatalf("at most %d cases ran at once, want 4", got)
	}
}

// TestSuiteRecordsStayInChosenOrder pins the card's rule that the records keep
// the plan's order whatever the finish order: the fake finishes the cases in
// reverse (later cases sleep less) and the records still come back MCTiny00 ..
// MCTiny05, the chosen order.
func TestSuiteRecordsStayInChosenOrder(t *testing.T) {
	root, cases := suiteTree(t, 6)
	exec := func(ctx context.Context, r Run, log string) int {
		var i int
		if _, err := fmt.Sscanf(filepath.Base(r.Config), "MCTiny%02d.cfg", &i); err != nil {
			return ExitNoStart
		}
		time.Sleep(time.Duration(len(cases)-i) * 20 * time.Millisecond)
		return passExec(log)
	}
	o, _ := suiteOptions(t, root, cases, 6, 110*time.Second, exec)
	res, err := RunSuite(o)
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if len(res.Records) != len(cases) {
		t.Fatalf("got %d records, want %d", len(res.Records), len(cases))
	}
	for i, c := range cases {
		if res.Records[i].Config != c.Config {
			t.Fatalf("record %d is %s, want %s (records not in chosen order)", i, res.Records[i].Config, c.Config)
		}
	}
}

// TestSuiteBudgetExhaustionMarksStartedAfter pins the card's budget rule: with
// a 1 s budget, 8 fake cases of 400 ms and Parallel 2, the cases whose start
// comes at or after the deadline write the budget-exhausted log and are
// recorded, and the suite fails.
func TestSuiteBudgetExhaustionMarksStartedAfter(t *testing.T) {
	root, cases := suiteTree(t, 8)
	exec := func(ctx context.Context, r Run, log string) int {
		time.Sleep(400 * time.Millisecond)
		return passExec(log)
	}
	o, out := suiteOptions(t, root, cases, 2, time.Second, exec)
	res, err := RunSuite(o)
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if !res.Failed {
		t.Fatalf("a suite whose cases started after the deadline did not fail")
	}
	if len(res.Records) != len(cases) {
		t.Fatalf("got %d records, want %d", len(res.Records), len(cases))
	}
	exhausted := 0
	for _, rec := range res.Records {
		raw, err := os.ReadFile(filepath.Join(out, rec.Config+".log"))
		if err != nil {
			t.Fatalf("read %s log: %v", rec.Config, err)
		}
		if strings.Contains(string(raw), "TLC suite budget exhausted before starting this case") {
			exhausted++
		}
	}
	if exhausted == 0 {
		t.Fatalf("no case started after the deadline got the budget-exhausted log")
	}
	if exhausted == len(cases) {
		t.Fatalf("every case was exhausted; the run never used its budget")
	}
}

// TestSuiteParallelOneIsSerialOrder pins the card's rule that Parallel 1
// reproduces today's order of exec calls exactly.
func TestSuiteParallelOneIsSerialOrder(t *testing.T) {
	root, cases := suiteTree(t, 6)
	var mu sync.Mutex
	var order []string
	exec := func(ctx context.Context, r Run, log string) int {
		mu.Lock()
		order = append(order, r.Config)
		mu.Unlock()
		return passExec(log)
	}
	o, _ := suiteOptions(t, root, cases, 1, 110*time.Second, exec)
	if _, err := RunSuite(o); err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if len(order) != len(cases) {
		t.Fatalf("ran %d cases, want %d", len(order), len(cases))
	}
	for i, c := range cases {
		if order[i] != c.Config {
			t.Fatalf("exec call %d is %s, want %s", i, order[i], c.Config)
		}
	}
}

func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
