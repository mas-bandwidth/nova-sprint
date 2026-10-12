package tlc

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// TestRunArgsPinsTheFixedOrder: Args lays out the JVM flags, the classpath and
// TLC main class, TLC's flags and last the config and module.
func TestRunArgsPinsTheFixedOrder(t *testing.T) {
	t.Parallel()
	r := Run{
		Java: "java", Jar: "/x/tlc.jar", Dir: ".", JVM: []string{"-Xmx2g"},
		TmpDir: "/tmp/j", Workers: 4, LnCheckFinal: true, NoDeadlock: true,
		MetaDir: "/meta", Config: "A.cfg", Module: "A.tla",
	}
	want := []string{
		"-Xmx2g", "-Djava.io.tmpdir=/tmp/j", "-cp", "/x/tlc.jar", "tlc2.TLC",
		"-lncheck", "final", "-workers", "4", "-deadlock", "-metadir", "/meta",
		"-config", "A.cfg", "A.tla",
	}
	if got := r.Args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Args()=%v want %v", got, want)
	}
}

func javaScript(version, body string) string {
	return "#!/bin/sh\n" +
		"if [ \"$1\" = \"-version\" ]; then\n" +
		"  echo 'openjdk version \"" + version + "\" 2025-04-15' >&2\n" +
		"  exit 0\nfi\n" + body
}

func writeJava(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "java")
	if err := os.WriteFile(path, []byte(javaScript("21.0.1", body)), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// javaExecMu serializes writing an executable java script and running it. A
// forked child inherits the parent's open descriptors, so when one test forks
// while another still has the script open for writing, the exec fails with
// ETXTBSY ("text file busy"). The lock spans the write and the exec, so a
// script is never open for writing while a sibling test forks. It is held only
// by the tests that run a written script; no other test in the package forks.
var javaExecMu sync.Mutex

// runJavaScript writes body as this package's java script in a fresh temp
// directory and calls fn with its path while the exec lock is held.
func runJavaScript(t *testing.T, body string, fn func(java string)) {
	t.Helper()
	javaExecMu.Lock()
	defer javaExecMu.Unlock()
	fn(writeJava(t, body))
}

// TestExecuteRunsTheJavaScript: Execute runs the java program and leaves its
// combined output in the log, returning the exit status.
func TestExecuteRunsTheJavaScript(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runJavaScript(t, "echo 'Model checking completed. No error has been found.'\nexit 0\n", func(java string) {
		r := Run{Java: java, Jar: filepath.Join(dir, "tlc.jar"), Dir: dir}
		log := filepath.Join(dir, "out.log")
		code := Execute(context.Background(), r, log)
		if code != ExitPass {
			t.Fatalf("Execute=%d want %d", code, ExitPass)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if !contains(string(raw), "Model checking completed.") {
			t.Fatalf("log does not hold TLC output: %q", raw)
		}
	})
}

// TestExecuteReportsTheProgramsExit: a java program that exits 13 returns 13.
func TestExecuteReportsTheProgramsExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runJavaScript(t, "exit 13\n", func(java string) {
		r := Run{Java: java, Jar: filepath.Join(dir, "tlc.jar"), Dir: dir}
		code := Execute(context.Background(), r, filepath.Join(dir, "out.log"))
		if code != 13 {
			t.Fatalf("Execute=%d want 13", code)
		}
	})
}

// TestCheckParallelRefusesWidthsOutsideTheRange: CheckParallel refuses widths
// the bench cannot run.
func TestCheckParallelRefusesWidthsOutsideTheRange(t *testing.T) {
	t.Parallel()
	if err := CheckParallel(1, 8); err != nil {
		t.Fatalf("CheckParallel(1,8): %v", err)
	}
	if err := CheckParallel(0, 8); err == nil {
		t.Fatal("CheckParallel accepted 0")
	}
	if err := CheckParallel(5, 8); err == nil {
		t.Fatal("CheckParallel accepted over half the CPUs")
	}
}

// TestCheckLimitsRefusesABudgetOverTheRoom: CheckLimits refuses a timeout above
// the room's budget and a worker count over the machines.
func TestCheckLimitsRefusesABudgetOverTheRoom(t *testing.T) {
	t.Parallel()
	if err := CheckLimits(time.Minute, 2, false); err != nil {
		t.Fatalf("CheckLimits: %v", err)
	}
	if err := CheckLimits(10*time.Hour, 2, false); err == nil {
		t.Fatal("CheckLimits accepted an unbounded budget")
	}
	if err := CheckLimits(time.Minute, 64, false); err == nil {
		t.Fatal("CheckLimits accepted too many workers")
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (hay == needle || len(needle) == 0 || indexOf(hay, needle) >= 0)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
