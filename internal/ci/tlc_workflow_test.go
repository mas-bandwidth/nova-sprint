package ci

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTLCWorkflowRunsOnHostedRunners pins .github/workflows/tlc.yml: the models
// run on GitHub-hosted ubuntu-latest runners (nova-sprint is public, so the
// fleet's self-hosted runners are not allowed), every pull request job is
// bounded at five minutes, the downloaded TLC jar is checked against
// tla/tla2tools.sha256 before make tlc runs, and a pull request's make tlc is
// called with the 110-second budget. The nightly (schedule and
// workflow_dispatch) is exempt from the two-minute rule (Glenn, 2026-10-21:
// "nightly runs are ALL exempt from the 2 minute rule!"): it runs every
// required group with make tlc-full and a real budget, inside a job timeout
// that covers that budget. On a four-core runner the 110-second budget ran out
// inside sprinttables, statsreset, dirtytick-three and nine more groups every
// night from 2026-10-17 (runs 39544741003, 39723025612): the cases after the
// budget recorded FAIL in 0.000 s, never a model's own verdict.
// The workflow is read as text: no YAML dependency is added.
func TestTLCWorkflowRunsOnHostedRunners(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/tlc.yml")
	require.NoError(t, err)
	jobs := workflowJobs(t, string(b))
	require.NotEmpty(t, jobs, "tlc.yml names no jobs")

	models := ""
	groups := ""
	for name, body := range jobs {
		require.Contains(t, body, "runs-on: ubuntu-latest", "job %q does not run on ubuntu-latest", name)
		require.NotContains(t, body, "self-hosted", "job %q names a self-hosted runner", name)
		minutes, nightly := jobTimeouts(t, name, body)
		require.LessOrEqual(t, minutes, 5, "job %q allows a pull request %d minutes, over the five-minute cap", name, minutes)
		if strings.Contains(body, "make tlc ") {
			require.Greater(t, nightly, 5, "the models job gives the nightly no more than a pull request's five minutes")
			require.GreaterOrEqual(t, nightly*60, nightlyBudget(t, body)+120, "the models job's nightly timeout does not cover the nightly TLC budget and its setup")
		}
		if strings.Contains(body, "make tlc ") {
			models = body
		}
		if strings.Contains(body, "make tlc-test") {
			groups = body
		}
	}
	require.NotEmpty(t, groups, "no job runs make tlc-test")
	// the schedule/dispatch branch must select every required group: make
	// tlc-groups is the --stale set, which is empty on a current tree, so the
	// nightly would run none of the cases.
	require.Contains(t, groups, "tlacheck groups --root .", "the groups job never asks tlacheck for the required groups")
	require.NotContains(t, groups, "make tlc-groups", "the groups job selects the stale set with make tlc-groups")
	require.NotEmpty(t, models, "no job runs make tlc")
	check := strings.Index(models, "tla/tla2tools.sha256")
	run := strings.Index(models, "make tlc ")
	require.GreaterOrEqual(t, check, 0, "the models job never checks the jar against tla/tla2tools.sha256")
	require.GreaterOrEqual(t, run, 0, "the models job never runs make tlc")
	require.Less(t, check, run, "the models job runs make tlc before checking the jar against tla/tla2tools.sha256")
	require.Contains(t, models, "TLC_BUDGET=110", "the models job does not run make tlc with TLC_BUDGET=110")
	pr := strings.Index(models, `if [ "${GITHUB_EVENT_NAME}" = "pull_request" ]; then`)
	full := strings.Index(models, "make tlc-full ")
	require.GreaterOrEqual(t, pr, 0, "the models job does not choose the budget by event")
	require.Less(t, pr, run, "make tlc (the 110-second budget) is not the pull request branch")
	require.Less(t, run, full, "the nightly branch does not run make tlc-full after the pull request branch")
	require.Less(t, check, full, "the models job runs make tlc-full before checking the jar against tla/tla2tools.sha256")
	require.Greater(t, nightlyBudget(t, models), 110, "the nightly runs with no more than the pull request's 110-second budget")
}

// nightlyBudget returns the TLC_BUDGET the models job's make tlc-full line names.
func nightlyBudget(t *testing.T, body string) int {
	t.Helper()
	full := strings.Index(body, "make tlc-full ")
	require.GreaterOrEqual(t, full, 0, "the models job never runs make tlc-full for the nightly")
	rest := body[full:]
	at := strings.Index(rest, "TLC_BUDGET=")
	require.GreaterOrEqual(t, at, 0, "make tlc-full names no TLC_BUDGET")
	digits := strings.TrimLeft(rest[at+len("TLC_BUDGET="):], `"`)
	end := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(digits)
	}
	n, err := strconv.Atoi(digits[:end])
	require.NoError(t, err, "make tlc-full's TLC_BUDGET is not a number of seconds")
	return n
}

// jobTimeouts returns a job's timeout-minutes for a pull request and for the
// nightly: a plain number is both, and the expression
// ${{ github.event_name == 'pull_request' && P || N }} is P and N.
func jobTimeouts(t *testing.T, name, body string) (int, int) {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "timeout-minutes:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "timeout-minutes:"))
		if n, err := strconv.Atoi(value); err == nil {
			return n, n
		}
		const prefix = "${{ github.event_name == 'pull_request' && "
		inner, ok := strings.CutPrefix(value, prefix)
		require.True(t, ok, "job %q has a timeout-minutes %q that is neither a number nor the pull request choice", name, value)
		inner, ok = strings.CutSuffix(inner, " }}")
		require.True(t, ok, "job %q has an unterminated timeout-minutes %q", name, value)
		p, n, ok := strings.Cut(inner, " || ")
		require.True(t, ok, "job %q has a timeout-minutes %q with no nightly value", name, value)
		pr, err := strconv.Atoi(p)
		require.NoError(t, err, "job %q has a non-numeric pull request timeout %q", name, p)
		nightly, err := strconv.Atoi(n)
		require.NoError(t, err, "job %q has a non-numeric nightly timeout %q", name, n)
		return pr, nightly
	}
	require.Failf(t, "no timeout", "job %q has no timeout-minutes", name)
	return 0, 0
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
