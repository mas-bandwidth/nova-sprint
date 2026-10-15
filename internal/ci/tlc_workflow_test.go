package ci

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTLCWorkflowRunsOnHostedRunners pins .github/workflows/tlc.yml: the models
// run on GitHub-hosted ubuntu-latest runners (nova-sprint is public, so the
// fleet's self-hosted runners are not allowed), every job is bounded at five
// minutes, the downloaded TLC jar is checked against tla/tla2tools.sha256
// before make tlc runs, and make tlc is called with the 110-second budget.
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
		minutes := jobTimeout(t, name, body)
		require.LessOrEqual(t, minutes, 5, "job %q allows %d minutes, over the five-minute cap", name, minutes)
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

// ruleRecipe is the recipe lines of the Makefile rule named target: the tab-indented
// lines that follow its header.
func ruleRecipe(t *testing.T, text, target string) string {
	t.Helper()
	var recipe []string
	in := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, target) {
			in = true
			continue
		}
		if !in {
			continue
		}
		if line == "" || !strings.HasPrefix(line, "\t") {
			break
		}
		recipe = append(recipe, line)
	}
	return strings.Join(recipe, "\n")
}

// TestTheTLCWorkflowRunsNovaToolsTlacheck pins that nova-sprint runs nova-tools'
// tlacheck: go.mod's tool directive supplies it, so the Makefile and tlc.yml call
// `go tool tlacheck`, never the deleted local copy ./tools/tlacheck, and tlc.yml's
// pull-request path trigger watches go.mod instead of the deleted tools/tlacheck.
func TestTheTLCWorkflowRunsNovaToolsTlacheck(t *testing.T) {
	t.Parallel()
	mk, err := os.ReadFile("../../Makefile")
	require.NoError(t, err)
	mkText := string(mk)
	require.NotContains(t, mkText, "./tools/tlacheck", "the Makefile still names the deleted local copy tools/tlacheck")
	for _, target := range []string{"tlc:", "tlc-full:", "tlc-groups:", "tlc-test:"} {
		recipe := ruleRecipe(t, mkText, target)
		require.NotEmpty(t, recipe, "Makefile has no %s rule", target)
		require.Contains(t, recipe, "tool tlacheck", "Makefile %s does not run nova-tools' tlacheck through `$(GO) tool tlacheck`", target)
		require.NotContains(t, recipe, "./tools/tlacheck", "Makefile %s still runs the deleted local copy", target)
	}

	b, err := os.ReadFile("../../.github/workflows/tlc.yml")
	require.NoError(t, err)
	wf := string(b)
	require.NotContains(t, wf, "go run ./tools/tlacheck", "tlc.yml still runs the deleted local copy")
	require.Contains(t, wf, "go tool tlacheck", "tlc.yml does not run nova-tools' tlacheck through `go tool tlacheck`")
	require.Contains(t, wf, `- "go.mod"`, "tlc.yml's pull-request path trigger does not watch go.mod")
	require.NotContains(t, wf, "tools/tlacheck/**", "tlc.yml's pull-request path trigger still watches the deleted tools/tlacheck")
}

// oneSourceRetiredFile lists the nova-tools copies nova-sprint has retired and must not
// bring back, one repository-relative path a line (empty lines and '#' comments skipped),
// read by TestNoRetiredOneSourceCopyReturns.
const oneSourceRetiredFile = "testdata/one-source-retired.txt"

// TestNoRetiredOneSourceCopyReturns pins that a copy nova-sprint has retired into
// nova-tools (one-source-retired.txt) has not come back.
func TestNoRetiredOneSourceCopyReturns(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(oneSourceRetiredFile)
	require.NoError(t, err, "%s is missing: the retired copy is unprotected", oneSourceRetiredFile)
	var retired []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		retired = append(retired, line)
	}
	require.NotEmpty(t, retired, "%s lists nothing", oneSourceRetiredFile)
	for _, rel := range retired {
		_, statErr := os.Stat(filepath.Join("../..", rel))
		require.Error(t, statErr, "%s came back: delete it and import nova-tools", rel)
	}
}
