//go:build functional

// The test binary started again as a child process, under a wall-clock bound: a
// functional test (RULES 6a: no real time in a unit test). The guard's unit tests
// and TestMain are reexec_test.go.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/testbin"
)

// runChild runs this test binary with words and an environment, bounded, and
// returns its exit code and its output.
func runChild(t *testing.T, env []string, args ...string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.WaitDelay = 10 * time.Second
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		require.NoError(t, err)
	}
	return cmd.ProcessState.ExitCode(), out.String()
}

// envWithout is the environment with every name in drop removed and the extra
// pairs added.
func envWithout(env []string, drop []string, extra ...string) []string {
	var out []string
	for _, kv := range env {
		keep := true
		for _, d := range drop {
			if strings.HasPrefix(kv, d+"=") {
				keep = false
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	return append(out, extra...)
}

// The incident's own words, run by a test the way the verb ran them: the child
// is the CLI, inits the twin and exits; no test runs in it, so nothing recurses.
// Without the mark the same words are refused loudly, and so is a chain too deep.
func TestTheIncidentWordsRunTheCLIAndNeverTheSuite(t *testing.T) {
	t.Parallel()
	twin := "mem:" + filepath.Join(t.TempDir(), "sprint.twin")
	words := []string{"init", "--readers", "reader-a", "--members", "m1:1", "--redis", twin, "--actor", "boss"}
	depthEnv := testbin.DepthEnv(reexecTool)
	marks := []string{reexecCLIEnv, depthEnv}
	base := envWithout(os.Environ(), marks)

	code, out := runChild(t, append(base, reexecCLIEnv+"=1", depthEnv+"=1"), words...)
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, "INIT OK")
	assert.NotContains(t, out, "=== RUN")
	assert.NotContains(t, out, "PASS")

	live := append(append([]string(nil), words[:6]...), "127.0.0.1:6380", "--actor", "stella")
	code, out = runChild(t, append(base, reexecCLIEnv+"=1", depthEnv+"=1"), live...)
	assert.NotEqual(t, 0, code, out)
	assert.Contains(t, out, "twin (--redis mem:<file>) only")

	code, out = runChild(t, append(base, depthEnv+"=1"), words...)
	assert.Equal(t, exitReexecRefuse, code, out)
	assert.Contains(t, out, "refusing to recurse")
	assert.NotContains(t, out, "PASS")

	code, out = runChild(t, append(base, reexecCLIEnv+"=1", depthEnv+"="+strconv.Itoa(testbin.MaxDepth)), words...)
	assert.Equal(t, exitReexecRefuse, code, out)
	assert.Contains(t, out, "refusing to recurse")
}
