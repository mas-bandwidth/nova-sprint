package ci

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTLCWorkflow pins .github/workflows/tlc.yml: every job runs on a GitHub-hosted
// ubuntu-latest runner (never self-hosted), every job is bounded to at most five minutes,
// the models job checks the downloaded jar against tla/tla2tools.sha256 before it runs
// make tlc, and make tlc is called with TLC_BUDGET=110.
func TestTLCWorkflow(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/tlc.yml")
	require.NoError(t, err)
	text := string(b)

	require.NotContains(t, text, "self-hosted", "tlc.yml must never run on a self-hosted runner")

	runsOn := regexp.MustCompile(`runs-on:\s*(\S+)`)
	runs := runsOn.FindAllStringSubmatch(text, -1)
	require.NotEmpty(t, runs, "tlc.yml names no runs-on")
	for _, m := range runs {
		require.Equal(t, "ubuntu-latest", m[1], "a job runs on %s, not ubuntu-latest", m[1])
	}

	timeouts := regexp.MustCompile(`timeout-minutes:\s*(\d+)`)
	tos := timeouts.FindAllStringSubmatch(text, -1)
	require.NotEmpty(t, tos, "tlc.yml names no timeout-minutes")
	for _, m := range tos {
		n, err := strconv.Atoi(m[1])
		require.NoError(t, err)
		require.LessOrEqual(t, n, 5, "a job's timeout-minutes is %d, over 5", n)
	}

	digest := strings.Index(text, "tla/tla2tools.sha256")
	makeTLC := strings.Index(text, "make tlc TLC_JAR=")
	require.GreaterOrEqual(t, digest, 0, "the models step must name tla/tla2tools.sha256")
	require.GreaterOrEqual(t, makeTLC, 0, "the models step must run make tlc TLC_JAR=")
	require.Less(t, digest, makeTLC, "the jar digest check must come before make tlc")

	require.Contains(t, text, "TLC_BUDGET=110", "make tlc must be called with TLC_BUDGET=110")
}
