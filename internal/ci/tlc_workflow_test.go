package ci

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fleetRunsOn is the label set that selects the fleet's Linux benches
// registered to nova-sprint (amd-2 and amd-3, 16 cores each).
const fleetRunsOn = "runs-on: [self-hosted, Linux, X64, fleet, nova-sprint]"

// forkGuard keeps a pull request from a fork off the fleet's self-hosted
// runners: nova-sprint is public.
const forkGuard = "github.event.pull_request.head.repo.full_name == github.repository"

// TestTLCWorkflowRunsOnFleetRunners pins .github/workflows/tlc.yml: the models
// run on the fleet's self-hosted Linux benches, never for a pull request from
// a fork (nova-sprint is public), and every job that names a self-hosted
// runner carries that guard; the groups job, which runs no TLC, stays on
// ubuntu-latest; every job is bounded at five minutes; the downloaded TLC jar
// is checked against tla/tla2tools.sha256 before make tlc runs; and make tlc
// is called with the 110-second budget and nproc/4 cases at once (two TLC
// workers each: half the bench's cores). The workflow is read as text: no
// YAML dependency is added.
func TestTLCWorkflowRunsOnFleetRunners(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/tlc.yml")
	require.NoError(t, err)
	jobs := workflowJobs(t, string(b))
	require.NotEmpty(t, jobs, "tlc.yml names no jobs")

	models := ""
	groups := ""
	for name, body := range jobs {
		minutes := jobTimeout(t, name, body)
		require.LessOrEqual(t, minutes, 5, "job %q allows %d minutes, over the five-minute cap", name, minutes)
		if strings.Contains(body, "self-hosted") {
			require.Contains(t, body, fleetRunsOn, "job %q names a self-hosted runner other than the fleet's label set", name)
			require.Contains(t, body, forkGuard, "job %q runs on a self-hosted runner without the fork guard", name)
		} else {
			require.Contains(t, body, "runs-on: ubuntu-latest", "job %q runs on neither the fleet nor ubuntu-latest", name)
		}
		if strings.Contains(body, "make tlc ") {
			models = body
		}
		if strings.Contains(body, "make tlc-test") {
			groups = body
		}
	}
	require.NotEmpty(t, groups, "no job runs make tlc-test")
	require.NotContains(t, groups, "make tlc ", "the groups job runs TLC")
	// the schedule/dispatch branch must select every required group: make
	// tlc-groups is the --stale set, which is empty on a current tree, so the
	// nightly would run none of the cases.
	require.Contains(t, groups, "tlacheck groups --root .", "the groups job never asks tlacheck for the required groups")
	require.NotContains(t, groups, "make tlc-groups", "the groups job selects the stale set with make tlc-groups")
	require.NotEmpty(t, models, "no job runs make tlc")
	require.Contains(t, models, fleetRunsOn, "the models job does not run on the fleet's benches")
	require.Contains(t, models, forkGuard, "the models job runs a fork's pull request on the fleet")
	check := strings.Index(models, "tla/tla2tools.sha256")
	run := strings.Index(models, "make tlc ")
	require.GreaterOrEqual(t, check, 0, "the models job never checks the jar against tla/tla2tools.sha256")
	require.GreaterOrEqual(t, run, 0, "the models job never runs make tlc")
	require.Less(t, check, run, "the models job runs make tlc before checking the jar against tla/tla2tools.sha256")
	require.Contains(t, models, "TLC_BUDGET=110", "the models job does not run make tlc with TLC_BUDGET=110")
	require.Contains(t, models, "$(( $(nproc) / 4 ))", "the models job does not size the case pool at nproc/4")
	require.Contains(t, models, `TLC_PARALLEL="${parallel}"`, "the models job does not pass the case pool to make tlc")
}

// workflowJobs splits the "jobs:" block of a workflow into each job's body by
// its two-space-indented header.
func workflowJobs(t *testing.T, text string) map[string]string {
	t.Helper()
	jobs := map[string]string{}
	var body strings.Builder
	name := ""
	flush := func() {
		if name != "" {
			jobs[name] = body.String()
		}
		body.Reset()
		name = ""
	}
	inJobs := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == "jobs:":
			inJobs = true
		case !inJobs:
		case line != "" && !strings.HasPrefix(line, " "):
			flush()
			inJobs = false
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   "):
			trimmed := strings.TrimPrefix(line, "  ")
			if strings.HasSuffix(trimmed, ":") && !strings.ContainsAny(trimmed, " ") && !strings.Contains(trimmed, ": ") {
				flush()
				name = strings.TrimSuffix(trimmed, ":")
			} else if name != "" {
				body.WriteString(line)
				body.WriteByte('\n')
			}
		default:
			if name != "" {
				body.WriteString(line)
				body.WriteByte('\n')
			}
		}
	}
	flush()
	return jobs
}

func jobTimeout(t *testing.T, name, body string) int {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "timeout-minutes:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "timeout-minutes:"))
		n, err := strconv.Atoi(value)
		require.NoError(t, err, "job %q has a non-numeric timeout-minutes %q", name, value)
		return n
	}
	require.Failf(t, "no timeout", "job %q has no timeout-minutes", name)
	return 0
}
