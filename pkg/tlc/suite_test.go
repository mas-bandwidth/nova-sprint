package tlc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func suiteFixture(t *testing.T, cases int) (string, []Case, Jar) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "tla")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := "config\tmodule\texpected\tproperty\tdeadlock\tgroup\tgate\tdebt\n"
	selected := make([]Case, cases)
	for i := range selected {
		name := fmt.Sprintf("MCTest%d", i)
		plan += fmt.Sprintf("%s.cfg\t%s.tla\tpass\t-\tcheck\ttiny\trequired\t-\n", name, name)
		selected[i] = Case{Config: name + ".cfg", Module: name + ".tla", Expected: "pass", Property: "-", Deadlock: "check", Group: "tiny", Gate: "required", Debt: "-"}
		if err := os.WriteFile(filepath.Join(dir, name+".cfg"), []byte("SPECIFICATION Spec\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".tla"), []byte("---- MODULE "+name+" ----\nVARIABLE x\nInit == x = 0\nNext == x' = x\n====\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, CasesFile), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, selected, Jar{Path: "unused", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
}

func suiteOptions(root string, cases []Case, jar Jar, out string) Options {
	return Options{Root: root, Cases: cases, Jar: jar, Java: "unused", JavaVer: "21", Out: out, Budget: time.Second * 5, Workers: 2, Platform: "linux-amd64", CPUs: 8, Selection: Selection{Group: "tiny", Shards: 1}}
}

func successfulFake(ctx context.Context, _ Run, log string) int {
	_ = os.WriteFile(log, []byte("Model checking completed. No error has been found.\n1 states generated, 1 distinct states found\n"), 0o644)
	return ExitPass
}

func TestRunSuiteRunsUpToParallelCasesAtOnce(t *testing.T) {
	root, cases, jar := suiteFixture(t, 8)
	var mu sync.Mutex
	active, peak := 0, 0
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	o := suiteOptions(root, cases, jar, t.TempDir())
	o.Parallel = 4
	o.Exec = func(ctx context.Context, _ Run, log string) int {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		_ = os.WriteFile(log, []byte("Model checking completed. No error has been found.\n1 states generated, 1 distinct states found\n"), 0o644)
		mu.Lock()
		active--
		mu.Unlock()
		return ExitPass
	}
	done := make(chan struct{})
	go func() { _, _ = RunSuite(o); close(done) }()
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			close(release)
			<-done
			t.Fatalf("only %d cases started concurrently", i)
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("suite did not finish")
	}
	if peak < 4 {
		t.Fatalf("peak active cases=%d, want 4", peak)
	}
}

func TestRunSuiteRecordsPlanOrderWhenCasesFinishOutOfOrder(t *testing.T) {
	root, cases, jar := suiteFixture(t, 4)
	o := suiteOptions(root, cases, jar, t.TempDir())
	o.Parallel = 4
	var mu sync.Mutex
	finished := []string{}
	callbacks := []string{}
	var callbackActive atomic.Int32
	var callbackOverlap atomic.Bool
	o.OnCase = func(r Record) {
		if callbackActive.Add(1) > 1 {
			callbackOverlap.Store(true)
		}
		time.Sleep(time.Millisecond)
		mu.Lock()
		callbacks = append(callbacks, r.Config)
		mu.Unlock()
		callbackActive.Add(-1)
	}
	o.Exec = func(ctx context.Context, r Run, log string) int {
		i := 0
		fmt.Sscanf(r.Config, "MCTest%d.cfg", &i)
		time.Sleep(time.Duration(4-i) * 20 * time.Millisecond)
		_ = os.WriteFile(log, []byte("Model checking completed. No error has been found.\n1 states generated, 1 distinct states found\n"), 0o644)
		mu.Lock()
		finished = append(finished, r.Config)
		mu.Unlock()
		return ExitPass
	}
	res, err := RunSuite(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != len(cases) {
		t.Fatalf("got %d records", len(res.Records))
	}
	for i, r := range res.Records {
		if r.Config != cases[i].Config {
			t.Fatalf("record[%d]=%s want %s", i, r.Config, cases[i].Config)
		}
	}
	if len(finished) < 2 || finished[0] == cases[0].Config {
		t.Fatalf("fake cases did not finish out of order: %v", finished)
	}
	if callbackOverlap.Load() || len(callbacks) != len(cases) || callbacks[0] != cases[len(cases)-1].Config {
		t.Fatalf("OnCase was not serialized in finish order: overlap=%t callbacks=%v", callbackOverlap.Load(), callbacks)
	}
}

func TestRunSuiteBudgetExhaustionRecordsUnstartedCases(t *testing.T) {
	root, cases, jar := suiteFixture(t, 4)
	o := suiteOptions(root, cases, jar, t.TempDir())
	o.Parallel = 2
	o.Budget = 250 * time.Millisecond
	o.Exec = func(ctx context.Context, _ Run, log string) int {
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(log, []byte("Model checking completed. No error has been found.\n1 states generated, 1 distinct states found\n"), 0o644)
		return ExitPass
	}
	res, err := RunSuite(o)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("budget exhaustion must fail suite")
	}
	found := false
	for _, c := range cases {
		b, e := os.ReadFile(filepath.Join(o.Out, c.Config+".log"))
		if e == nil && string(b) == "TLC suite budget exhausted before starting this case\n" {
			found = true
		}
	}
	if !found {
		t.Fatal("no unstarted case recorded the suite budget exhaustion")
	}
}

func TestRunSuiteParallelOnePreservesExecutionOrder(t *testing.T) {
	root, cases, jar := suiteFixture(t, 4)
	o := suiteOptions(root, cases, jar, t.TempDir())
	o.Parallel = 1
	o.Exec = successfulFake
	var calls []string
	o.Exec = func(ctx context.Context, r Run, log string) int {
		calls = append(calls, r.Config)
		return successfulFake(ctx, r, log)
	}
	res, err := RunSuite(o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("suite failed")
	}
	for i, c := range cases {
		if calls[i] != c.Config {
			t.Fatalf("call[%d]=%s want %s", i, calls[i], c.Config)
		}
	}
}
