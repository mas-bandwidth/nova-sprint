package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoWorkflowNamesAGitHubHostedRunner scans every workflow under .github/workflows
// and fails if any job names a GitHub-hosted runner label (ubuntu-*, macos-*,
// windows-*, including inside a matrix or expression line) or runs a global install
// onto the shared fleet runner (sudo apt-get install, brew install, npm -g/--global,
// go install, cargo install, pip install outside a job-local venv).
func TestNoWorkflowNamesAGitHubHostedRunner(t *testing.T) {
	t.Parallel()
	problems := scanWorkflows(t, "../../.github/workflows")
	require.Empty(t, problems, "hosted labels or install lines found:\n%s", strings.Join(problems, "\n"))
}

// TestNoWorkflowNamesAGitHubHostedRunnerPlant plants both offenses in a temp copy
// and asserts the scanner names both; it fails if the scanner is stubbed (returns
// nothing).
func TestNoWorkflowNamesAGitHubHostedRunnerPlant(t *testing.T) {
	t.Parallel()
	workflows := filepath.Join(t.TempDir(), ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflows, 0o755))

	bad := `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: sudo apt-get install -y redis-server
`
	good := `jobs:
  test:
    runs-on: [self-hosted, Linux, X64, nova-sprint]
    steps:
      - name: go on PATH
        run: go version
`
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "ci.yml"), []byte(bad), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "tlc.yml"), []byte(good), 0o644))

	problems := scanWorkflows(t, workflows)
	all := strings.Join(problems, "\n")
	require.Contains(t, all, "ubuntu-latest", "scanner did not name the hosted label")
	require.Contains(t, all, "apt-get install", "scanner did not name the install line")

	require.NoError(t, os.Remove(filepath.Join(workflows, "ci.yml")))
	problems = scanWorkflows(t, workflows)
	require.Empty(t, problems, "clean workflow flagged:\n%s", strings.Join(problems, "\n"))
}

// scanWorkflows returns a line per problem found in every *.yml under dir. The
// line carries the file, the line number, the job it belongs to and the offending
// text. The scan is line-based: no YAML library is used.
func scanWorkflows(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	require.NoError(t, err)
	var problems []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		job := ""
		for i, raw := range strings.Split(string(b), "\n") {
			if name, ok := jobHeader(raw); ok {
				job = name
			}
			if hostedLabel(raw) {
				problems = append(problems, fmt.Sprintf("%s:%d job %s: %s", path, i+1, job, strings.TrimSpace(raw)))
			}
			if installLine(raw) {
				problems = append(problems, fmt.Sprintf("%s:%d job %s: %s", path, i+1, job, strings.TrimSpace(raw)))
			}
		}
	}
	return problems
}

// jobHeader reports whether line is a two-space-indented job header and its name.
func jobHeader(line string) (string, bool) {
	if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
		return "", false
	}
	trimmed := strings.TrimPrefix(line, "  ")
	if strings.HasSuffix(trimmed, ":") && !strings.ContainsAny(trimmed, " ") && !strings.Contains(trimmed, ": ") {
		return strings.TrimSuffix(trimmed, ":"), true
	}
	return "", false
}

// hostedLabel reports whether line names a GitHub-hosted runner label, including
// inside a matrix or expression line.
func hostedLabel(line string) bool {
	for _, label := range []string{"ubuntu-", "macos-", "windows-"} {
		if strings.Contains(line, label) {
			return true
		}
	}
	return false
}

// installLine reports whether line runs a global install onto the shared fleet
// runner. A pip install is only refused outside a job-local venv/target.
func installLine(line string) bool {
	s := line
	switch {
	case strings.Contains(s, "sudo apt-get install"), strings.Contains(s, "brew install"),
		strings.Contains(s, "go install"), strings.Contains(s, "cargo install"):
		return true
	case strings.Contains(s, "npm install") && (strings.Contains(s, "-g") || strings.Contains(s, "--global")):
		return true
	case strings.Contains(s, "pip install") && !strings.Contains(s, "venv") && !strings.Contains(s, "--target"):
		return true
	}
	return false
}
