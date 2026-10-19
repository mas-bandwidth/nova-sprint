package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoWorkflowNamesAGitHubHostedRunnerOnTheFleet scans every workflow under
// .github/workflows and fails if any job names a GitHub-hosted runner label
// (ubuntu-*, macos-*, windows-*, including inside a matrix or expression line)
// or runs a global install onto the shared fleet runner (sudo apt-get install,
// brew install, npm -g/--global, go install, cargo install, dotnet tool install
// -g, luarocks install without --tree, pip install outside a job-local venv).
func TestNoWorkflowNamesAGitHubHostedRunnerOnTheFleet(t *testing.T) {
	t.Parallel()
	problems := scanWorkflows(t, "../../.github/workflows")
	require.Empty(t, problems, "hosted labels or install lines found:\n%s", strings.Join(problems, "\n"))
}

// TestNoWorkflowNamesAGitHubHostedRunnerOnTheFleetPlant plants both offenses in
// a temp copy and asserts the scanner names both; it fails if the scanner is
// stubbed (returns nothing). It also plants a .yaml file (GitHub accepts both
// extensions) and each named install form, so none can slip past the lint.
func TestNoWorkflowNamesAGitHubHostedRunnerOnTheFleetPlant(t *testing.T) {
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

	// GitHub accepts .yaml too: a hosted label there must be named as well.
	newYAML := `jobs:
  new:
    runs-on: windows-2022
`
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "new.yaml"), []byte(newYAML), 0o644))
	problems = scanWorkflows(t, workflows)
	all = strings.Join(problems, "\n")
	require.Contains(t, all, "windows-2022", "scanner did not read a .yaml workflow")
	require.Contains(t, all, "new.yaml", "scanner did not name the .yaml file")
	require.NoError(t, os.Remove(filepath.Join(workflows, "new.yaml")))

	// Each globally-installing form the rule names must be refused; the pip
	// line mentions "venv" in a comment, which does not establish a venv.
	installs := `jobs:
  installs:
    runs-on: [self-hosted, Linux, X64, nova-sprint]
    steps:
      - run: dotnet tool install -g dotnet-format
      - run: dotnet tool install --global dotnet-format
      - run: luarocks install luacheck
      - run: pip install requests  # venv is only a comment here
      - run: npm install -g typescript
      - run: npm install --global typescript
      - run: go install example.com/tool@latest
      - run: cargo install ripgrep
      - run: brew install jq
`
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "installs.yml"), []byte(installs), 0o644))
	problems = scanWorkflows(t, workflows)
	all = strings.Join(problems, "\n")
	for _, want := range []string{
		"dotnet tool install -g", "dotnet tool install --global",
		"luarocks install", "pip install", "npm install -g",
		"npm install --global", "go install", "cargo install", "brew install",
	} {
		require.Contains(t, all, want, "scanner did not name install form %q", want)
	}
	require.NoError(t, os.Remove(filepath.Join(workflows, "installs.yml")))
	require.NoError(t, os.Remove(filepath.Join(workflows, "ci.yml")))

	// A job-local venv or --tree is allowed, not a global install.
	local := `jobs:
  installs:
    runs-on: [self-hosted, Linux, X64, nova-sprint]
    steps:
      - run: .venv/bin/pip install requests
      - run: pip install --target .deps requests
      - run: luarocks install --tree ./lua luacheck
      - run: npm install typescript
`
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "local.yml"), []byte(local), 0o644))
	problems = scanWorkflows(t, workflows)
	require.Empty(t, problems, "job-local install flagged:\n%s", strings.Join(problems, "\n"))
	require.NoError(t, os.Remove(filepath.Join(workflows, "local.yml")))

	problems = scanWorkflows(t, workflows)
	require.Empty(t, problems, "clean workflow flagged:\n%s", strings.Join(problems, "\n"))
}

// scanWorkflows returns a line per problem found in every .yml and .yaml under
// dir. The line carries the file, the line number, the job it belongs to and
// the offending text. The scan is line-based: no YAML library is used.
func scanWorkflows(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var problems []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".yml", ".yaml":
		default:
			continue
		}
		path := filepath.Join(dir, entry.Name())
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		job := ""
		for i, raw := range strings.Split(string(b), "\n") {
			if name, ok := jobHeader(raw); ok {
				job = name
			}
			trimmed := strings.TrimSpace(raw)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if hostedLabel(raw) {
				problems = append(problems, fmt.Sprintf("%s:%d job %s: %s", path, i+1, job, trimmed))
			}
			if installLine(raw) {
				problems = append(problems, fmt.Sprintf("%s:%d job %s: %s", path, i+1, job, trimmed))
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
// runner. A pip install is only allowed into a job-local venv or --target; a
// "venv" in a comment does not count. A luarocks install is only allowed with
// a job-local --tree.
func installLine(line string) bool {
	s := withoutComment(line)
	if strings.TrimSpace(s) == "" {
		return false
	}
	switch {
	case strings.Contains(s, "sudo apt-get install"),
		strings.Contains(s, "brew install"),
		strings.Contains(s, "go install"),
		strings.Contains(s, "cargo install"):
		return true
	case strings.Contains(s, "npm install") && (wordIn(s, "-g") || strings.Contains(s, "--global")):
		return true
	case strings.Contains(s, "dotnet tool install") && (wordIn(s, "-g") || strings.Contains(s, "--global")):
		return true
	case strings.Contains(s, "luarocks install") && !strings.Contains(s, "--tree"):
		return true
	case strings.Contains(s, "pip install") && !pipIntoLocalVenv(s):
		return true
	}
	return false
}

// pipIntoLocalVenv reports whether the pip install writes into a job-local
// venv or target rather than the shared runner's Python.
func pipIntoLocalVenv(s string) bool {
	if strings.Contains(s, "--target") {
		return true
	}
	for _, field := range strings.Fields(s) {
		field = strings.Trim(field, `"'`)
		if strings.HasPrefix(field, "venv/bin/") || strings.HasPrefix(field, ".venv/bin/") ||
			strings.Contains(field, "/venv/bin/") || strings.Contains(field, "/.venv/bin/") ||
			strings.Contains(field, "$VIRTUAL_ENV/bin/") {
			return true
		}
	}
	return false
}

// wordIn reports whether word is a whitespace-delimited field of s.
func wordIn(s, word string) bool {
	for _, field := range strings.Fields(s) {
		if field == word {
			return true
		}
	}
	return false
}

// withoutComment drops a trailing shell comment so a form merely mentioned in a
// comment is not mistaken for the command.
func withoutComment(s string) string {
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	return s
}
