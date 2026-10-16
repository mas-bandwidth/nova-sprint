package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/testbin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real lander's base gate must see the first red class, not only build
// and vet (docs/SPEC-SPRINT.md, the base's class gate). The bench is a fake.
func TestARedClassOnTheBaseStopsLandings(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the module", goModule)
	r.files("the class package", map[string]string{"internal/ci/ci.go": "package ci\n"})
	r.files("the base's unused helper", map[string]string{"unused.go": "package main\n\nfunc helper() {}\n"})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	seedKnownLanderBase(r, "main")
	r.ok("add --stream s1 --count 1 --one")
	r.queued(map[string]string{"s1-1": r.card("s1-1", map[string]string{"other.go": "package main\n"})}, "s1-1")
	var runs []string
	b := r.a.landState()
	b.mu.Lock()
	b.gateBench = func(_ context.Context, _ string, dir string, commands [][]string, _ bool) (string, int, error) {
		for _, command := range commands {
			run := strings.Join(command, " ")
			runs = append(runs, run)
			if strings.Contains(run, "TestStaticcheckFindings") {
				body, err := os.ReadFile(filepath.Join(dir, "unused.go"))
				require.NoError(t, err)
				if strings.Contains(string(body), "func helper") {
					return gateMark + run + "\n--- FAIL: TestStaticcheckFindings (0.00s)\nunused.go:3:6: func helper is unused (U1000)\nFAIL github.com/mas-bandwidth/nova-tools/internal/ci\n", 1, nil
				}
			}
		}
		return "", 0, nil
	}
	b.mu.Unlock()
	for n := range sprint.BaseGateStops {
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code, out+errs)
		assert.Contains(t, out+errs, "staticcheck")
		if n < len(sprint.BaseGateRetries) {
			r.after(sprint.BaseGateRetries[n])
		}
	}
	assert.Equal(t, "stopped base", r.streamState("s1"))
	assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"))
	inbox := r.ok("inbox")
	assert.Equal(t, 1, strings.Count(inbox, sprint.NBaseRed))
	for _, want := range []string{"staticcheck", "U1000", "fix-red-staticcheck-main", "TestStaticcheckFindings"} {
		assert.Contains(t, inbox, want)
	}
	assert.Contains(t, strings.Join(runs, "\n"), "gofmt -l .")
	assert.Contains(t, strings.Join(runs, "\n"), "functional")
	r.after(sprint.BaseGateRetries[1])
	r.do("land --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, strings.Count(r.ok("inbox"), sprint.NBaseRed), "the same red base has one judgment")
}

// gofmt's exit zero is not green when it prints. Both execution routes
// enforce that contract; these fake programs perform no Go work.
func TestTheClassGateReadsGofmtOutputLocallyAndOnTheBench(t *testing.T) {
	t.Parallel()
	for _, needsFormatting := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "formatting required"}[needsFormatting], func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			require.NoError(t, os.Mkdir(bin, 0700))
			body := "#!/bin/sh\nexit 0\n"
			if needsFormatting {
				body = "#!/bin/sh\nprintf 'bad.go\\n'\n"
			}
			require.NoError(t, testbin.WriteExecutable(filepath.Join(bin, "gofmt"), []byte(body), 0700))
			env := append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			l := &lander{}
			output, runErr := l.goRun(t.Context(), dir, []string{filepath.Join(bin, "gofmt"), "-l", "."})
			runErr = gateRunError([]string{"gofmt", "-l", "."}, output, runErr)
			if needsFormatting {
				assert.ErrorContains(t, runErr, "gofmt listed files")
				assert.Contains(t, output, "bad.go")
			} else {
				assert.NoError(t, runErr)
			}
			cmd := exec.CommandContext(t.Context(), "sh", "-c", gateScript([][]string{{"gofmt", "-l", "."}, {"sh", "-c", "touch reached"}}))
			cmd.Dir, cmd.Env = dir, env
			out, err := cmd.CombinedOutput()
			if needsFormatting {
				assert.Error(t, err, string(out))
				assert.Contains(t, string(out), "bad.go")
				_, err = os.Stat(filepath.Join(dir, "reached"))
				assert.True(t, os.IsNotExist(err), "first red ends the suite")
			} else {
				assert.NoError(t, err, string(out))
				assert.FileExists(t, filepath.Join(dir, "reached"))
			}
		})
	}
}

// The whole-tree analyzer classes are one run of one test binary, not three: the suite
// is staged (docs/SPEC-SPRINT.md, the base's class suite; the report order is the class
// order). gateRuns is the suite's stages, and its analyzer run names all three tests.
func TestGateRunsBatchesTheAnalyzerClasses(t *testing.T) {
	t.Parallel()
	stages := gateRuns(true, []string{"internal/ci"}, nil)
	var analyzer []gateRun
	for _, st := range stages {
		for _, r := range st.Runs {
			cmd := strings.Join(r.Run, " ")
			if strings.Contains(cmd, "TestStaticcheckFindings") || strings.Contains(cmd, "TestUncheckedErrors") || strings.Contains(cmd, "TestDeadCode") {
				analyzer = append(analyzer, r)
			}
		}
	}
	require.Len(t, analyzer, 1, "one analyzer run, not three")
	cmd := strings.Join(analyzer[0].Run, " ")
	for _, name := range []string{"TestStaticcheckFindings", "TestUncheckedErrors", "TestDeadCode"} {
		assert.Contains(t, cmd, name)
	}
	assert.Contains(t, cmd, "-run")
	assert.Contains(t, cmd, "./internal/ci/")
	require.Len(t, analyzer[0].Classes, 3, "the run reports as all three classes")
}

// Stage 1 is read-only over one tree, so its classes go side by side. Vet and the
// functional-tier vet each block until the other starts: a serial gate times out and
// is red, so this is red before the change.
func TestTheLocalGateRunsAStageSideBySide(t *testing.T) {
	t.Parallel()
	stages := gateRuns(true, []string{"internal/ci", "internal/docs"}, nil)
	l := &lander{}
	started := make(chan string, 4)
	release := make(chan struct{})
	fake := func(_ context.Context, _ string, run []string) (string, error) {
		cmd := strings.Join(run, " ")
		switch {
		case cmd == "go vet ./...":
			started <- "vet"
			select {
			case <-release:
			case <-time.After(2 * time.Second):
				return "", errors.New("the vet waited for a serial gate")
			}
		case strings.Contains(cmd, "go vet -tags functional"):
			started <- "vet-functional"
			select {
			case <-release:
			case <-time.After(2 * time.Second):
				return "", errors.New("the functional vet waited for a serial gate")
			}
		}
		return "", nil
	}
	go func() { <-started; <-started; close(release) }()
	why := l.runGateStages(context.Background(), t.TempDir(), stages, fake)
	assert.Empty(t, why, "both vets started, so the stage went side by side")
}

// Whatever finishes first, the finding is the first red in class order: gofmt runs before
// the functional-tier vet in the suite (docs/SPEC-SPRINT.md, the report order is the
// class order).
func TestTheLocalGateReportsTheFirstRedInClassOrder(t *testing.T) {
	t.Parallel()
	stages := gateRuns(true, []string{"internal/ci", "internal/docs"}, nil)
	l := &lander{}
	gofmtDone := make(chan struct{})
	fake := func(_ context.Context, _ string, run []string) (string, error) {
		cmd := strings.Join(run, " ")
		switch {
		case cmd == "gofmt -l .":
			defer close(gofmtDone)
			return "bad.go", errors.New("exit status 1")
		case strings.Contains(cmd, "go vet -tags functional"):
			<-gofmtDone
			return "later red", errors.New("exit status 1")
		}
		return "", nil
	}
	why := l.runGateStages(context.Background(), t.TempDir(), stages, fake)
	assert.Contains(t, why, "gofmt -l .", "gofmt is the earlier class")
	assert.NotContains(t, why, "vet -tags functional", "the later red is not reported")
}

// The bench gate runs each stage's runs as background jobs and waits for them (one shell
// line, gateScript); a red analyzer run is the class its FAIL line names (errcheck for
// TestUncheckedErrors), read as today (redRun).
func TestTheBenchGateRunsStagesSideBySide(t *testing.T) {
	t.Parallel()
	runs := gateCmds(gateRuns(true, []string{"internal/ci", "internal/docs"}, nil))
	script := gateScript(runs)
	assert.Contains(t, script, "2>&1 &", "the stage's runs are background jobs")
	assert.Contains(t, script, `wait "$gate_p"`, "the stage is waited for")
	assert.Contains(t, script, "TestStaticcheckFindings", "the merged analyzer run is the script's")
	assert.NotContains(t, script, "TestStaticcheckFindings$", "not one process per analyzer class")

	merged := runs[4]
	require.Contains(t, strings.Join(merged, " "), "TestUncheckedErrors")
	out := gateMark + strings.Join(merged, " ") + "\n--- FAIL: TestUncheckedErrors (0.01s)\nFAIL github.com/mas-bandwidth/nova-sprint/internal/ci\n"
	red := gateRedRun(runs, out)
	assert.Equal(t, []string{"go", "test", "-tags", "functional", "-count=1", "-timeout", "600s", "-run", "^TestUncheckedErrors$", "./internal/ci/"}, red)
}
