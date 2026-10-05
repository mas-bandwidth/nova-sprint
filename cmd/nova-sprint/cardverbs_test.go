package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/goenv"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
	"github.com/mas-bandwidth/nova-sprint/internal/testbin"
)

// TestCardVerbsAreNovaSprintVerbs pins the move: nova-card generate, template
// and lint are verbs of nova-sprint, the read verb card still reads one card,
// and the old top-level names refuse as moved.
func TestCardVerbsAreNovaSprintVerbs(t *testing.T) {
	t.Parallel()
	help, errb := sprintOut("help")
	require.Empty(t, errb)
	for _, line := range []string{
		"\n  nova-sprint card generate ",
		"\n  nova-sprint card template\n",
		"\n  nova-sprint card lint ",
		"[--max <n>]",
		"[--dry-run]",
		"[--tier flash|pro]",
	} {
		assert.Contains(t, help, line, "nova-sprint help")
	}

	genHelp, _ := sprintOut("card", "generate", "-h")
	assert.Contains(t, genHelp, "usage: nova-sprint card generate --from ledger|findings|help ")
	assert.Contains(t, genHelp, "effect: local write:")
	assert.Contains(t, genHelp, "card generate --from ledger --ledger serial-tests")
	assert.Contains(t, genHelp, "card generate --from help --tool nova-bus")
	lintHelp, _ := sprintOut("card", "lint", "-h")
	assert.Contains(t, lintHelp, "usage: nova-sprint card lint --card <file>")
	assert.Contains(t, lintHelp, "effect: inspection:")
	tplHelp, _ := sprintOut("card", "template", "-h")
	assert.Equal(t, "usage: nova-sprint card template", headLine(tplHelp))
	assert.Contains(t, tplHelp, "effect: inspection: prints the card template")

	// help card stays the read. card generate is not that read.
	readHelp, _ := sprintOut("help", "card")
	assert.Contains(t, headLine(readHelp), "usage: nova-sprint card <id>")
	bare, bareErr := sprintOut("card")
	assert.Contains(t, bareErr, "wants one primary id, found none")
	assert.Empty(t, bare)
	_, twoErr := sprintOut("card", "a", "b")
	assert.Contains(t, twoErr, "wants one primary id, found a b")

	// the old nova-card verb names, typed on nova-sprint, refuse as moved
	for _, name := range []string{"generate", "template", "lint"} {
		out, err := sprintOut(name)
		assert.Empty(t, out, name)
		assert.Contains(t, err, name+" moved to nova-sprint card "+name+"; run: nova-sprint card "+name+" -h")
	}

	text, err := swarm.Template("card")
	require.NoError(t, err)
	got, tplErr := sprintOut("card", "template")
	assert.Empty(t, tplErr)
	assert.Equal(t, text, got)
	_, extra := sprintOut("card", "template", "nope")
	assert.Contains(t, extra, "takes no flags and no arguments")

	// a server named does not take these verbs: they read this machine's files
	code, served, servedErr := runSprint(func(k string) string {
		if k == "NOVA_SPRINT_SERVER" {
			return "127.0.0.1:1"
		}
		return ""
	}, "card", "template")
	assert.Equal(t, 0, code, servedErr)
	assert.Equal(t, text, served)

	// generate from the findings fixture, then lint what it wrote
	findings := filepath.Join("..", "nova-card", "testdata", "findings.tsv")
	outDir := filepath.Join(t.TempDir(), "cards")
	sha := strings.Repeat("ab", 20)
	code, stdout, stderr := runSprint(nil, "card", "generate", "--from", "findings", "--file", findings, "--repo", "example/repo", "--base", "dev", "--sha", sha, "--out", outDir)
	require.Equal(t, 0, code, "stdout %s\nstderr %s", stdout, stderr)
	assert.Contains(t, stdout, "CARDS OK dir="+outDir+" cards=2 waves=1 tier=pro")
	brief := filepath.Join(outDir, "finding-internal-bus-send.md")
	code, stdout, stderr = runSprint(nil, "card", "lint", "--card", brief)
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "LINT OK file="+brief)

	// dry-run writes nothing and is not the read verb treating "generate" as an id
	dry := filepath.Join(t.TempDir(), "dry")
	code, stdout, stderr = runSprint(nil, "card", "generate", "--from", "findings", "--file", findings, "--repo", "example/repo", "--base", "dev", "--sha", sha, "--out", dry, "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "cards=2 waves=1 tier=pro dry-run=yes")
	assert.Contains(t, stdout, "id\tfile\ttest\twave\tdeps\n")
	assert.NotContains(t, stdout+stderr, "primary")
	assert.NoDirExists(t, dry)

	// a red brief is named and generate writes nothing
	bad := filepath.Join(t.TempDir(), "bad.md")
	require.NoError(t, os.WriteFile(bad, []byte("RESULT: bad sha=0123456789ab tier: cheap\nKIND: fix-red\n\nTHE TASK. <fill>\n"), 0o644))
	code, stdout, _ = runSprint(nil, "card", "lint", "--card", bad)
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "LINT DRIFT card=bad check=model-lines line=1")
	assert.Contains(t, stdout, "check=placeholder")
}

func headLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func sprintOut(args ...string) (string, string) {
	_, out, err := runSprint(nil, args...)
	return out, err
}

func runSprint(getenv func(string) string, args ...string) (int, string, string) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	var out, errb strings.Builder
	code := newApp(getenv).run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// A directory cut by --max names no cut card, and a detached HEAD is no base.
func TestCardGenerateMaxAndDetachedHEAD(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	ledger := cardgen.Ledgers["serial-tests"]
	for rel, text := range map[string]string{
		"cmd/a/a_test.go": "package main\n", "cmd/b/b_test.go": "package main\n", "cmd/c/c_test.go": "package main\n",
		ledger.File: "cmd/a/a_test.go:TestA serial: t.Setenv\ncmd/b/b_test.go:TestB serial: t.Chdir\ncmd/c/c_test.go:TestC serial: os.Setenv\n",
	} {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	code, stdout, stderr := runSprint(nil, "card", "generate", "--from", "ledger", "--ledger", "serial-tests", "--repo-dir", repo, "--repo", "o/r", "--base", "dev", "--sha", strings.Repeat("ab", 20), "--out", filepath.Join(t.TempDir(), "cards"), "--max", "2", "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "cards=2 waves=2 tier=flash dry-run=yes")
	assert.NotContains(t, stdout, "serial-tests-cmd-c-c")

	dir := gitFixture(t, map[string]string{"internal/bus/send.go": "package bus\n", "internal/bus/send_test.go": "package bus\n", "cmd/nova-bus/main.go": "package main\n", "cmd/nova-bus/main_test.go": "package main\n"})
	code, stdout, stderr = runSprint(nil, "card", "generate", "--from", "findings", "--file", filepath.Join("..", "nova-card", "testdata", "findings.tsv"), "--repo-dir", dir, "--out", filepath.Join(t.TempDir(), "cards"), "--dry-run")
	require.Equal(t, 0, code, "an attached HEAD names its branch: %s", stderr)
	assert.Contains(t, stdout, "cards=2")
	gitCmd(t, dir, "checkout", "-q", "--detach")
	code, _, stderr = runSprint(nil, "card", "generate", "--from", "findings", "--file", filepath.Join("..", "nova-card", "testdata", "findings.tsv"), "--repo-dir", dir, "--out", filepath.Join(t.TempDir(), "cards"))
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "no base branch")
}

// A tool named twice is refused before a directory is written.
func TestCardGenerateRefusesAToolNamedTwice(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture tool is a shell script")
	}
	bin := t.TempDir()
	require.NoError(t, testbin.WriteExecutable(filepath.Join(bin, "nova-x"), []byte("#!/bin/sh\necho 'nova-x: a fixture'\n"), 0o755))
	out := filepath.Join(t.TempDir(), "cards")
	code, _, stderr := runSprint(nil, "card", "generate", "--from", "help", "--tool", "nova-x", "--tool", "nova-x", "--bin-dir", bin, "--repo", "o/r", "--base", "dev", "--sha", strings.Repeat("ab", 20), "--out", out)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "card help-nova-x is planned twice")
	assert.NoDirExists(t, out)
}

func gitFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	gitCmd(t, dir, "init", "-q", "-b", "dev")
	gitCmd(t, dir, "remote", "add", "origin", "git@example.com:example/repo.git")
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-q", "-m", "fixture")
	return dir
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(goenv.Clean(os.Environ()), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
}

// A red brief is named on its LINT DRIFT line, a PATHS entry that names nothing
// in the checkout is red too, an --out that holds a brief is refused, and
// generate writes nothing while one brief is red.
func TestCardGenerateRedAndRefusedWritesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sha := strings.Repeat("ab", 20)
	findings := filepath.Join(dir, "f.tsv")
	require.NoError(t, os.WriteFile(findings, []byte("internal/none/x.go:1\tgone\tfix it\tinternal/none TestX\n"), 0o644))
	out := filepath.Join(dir, "cards")
	code, stdout, stderr := runSprint(nil, "card", "generate", "--from", "findings", "--file", findings, "--repo-dir", t.TempDir(), "--repo", "o/r", "--base", "dev", "--sha", sha, "--out", out)
	assert.Equal(t, 1, code, stderr)
	assert.Contains(t, stdout, "check=paths-at-base")
	assert.Contains(t, stderr, "nova-sprint card generate FAILED")
	assert.NoDirExists(t, out)

	code, _, stderr = runSprint(nil, "card", "generate", "--from", "findings", "--file", filepath.Join("..", "nova-card", "testdata", "findings.tsv"), "--repo", "o/r", "--base", "dev", "--sha", sha, "--out", dir)
	assert.Equal(t, 0, code, "an empty --out is written: %s", stderr)
	code, _, stderr = runSprint(nil, "card", "generate", "--from", "findings", "--file", filepath.Join("..", "nova-card", "testdata", "findings.tsv"), "--repo", "o/r", "--base", "dev", "--sha", sha, "--out", dir)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "already holds")
}

// With a checkout, a help card's TEST is read off the tool's package, and a
// findings card on a package with no test file says NEW: for the file it creates.
func TestCardGenerateReadsTestsOffTheCheckout(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture tool is a shell script")
	}
	bin := t.TempDir()
	for _, tool := range []string{"nova-x", "nova-y"} {
		require.NoError(t, testbin.WriteExecutable(filepath.Join(bin, tool), []byte("#!/bin/sh\necho '"+tool+": a fixture'\n"), 0o755))
	}
	repo := t.TempDir()
	for rel, text := range map[string]string{
		"cmd/nova-x/main.go":          "package main\n",
		"cmd/nova-x/firstrun_test.go": "package main\n\nfunc TestUsageBannerExamplesRun(t *testing.T) {\n\tonboarding.ExampleLines(banner, \"nova-x\")\n}\n",
		"cmd/nova-y/main.go":          "package main\n",
		"cmd/nova-y/y_test.go":        "package main\n",
		"docs/CLI.md":                 "# CLI\n",
		"internal/none/x.go":          "package none\n",
	} {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	sha := strings.Repeat("ab", 20)
	code, stdout, stderr := runSprint(nil, "card", "generate", "--from", "help", "--tool", "nova-x", "--tool", "nova-y", "--bin-dir", bin, "--repo-dir", repo, "--repo", "o/r", "--base", "dev", "--sha", sha, "--out", filepath.Join(t.TempDir(), "cards"), "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "help-nova-x\tcmd/nova-x/main.go\tcmd/nova-x TestUsageBannerExamplesRun\t1\t-\n")
	assert.Contains(t, stdout, "help-nova-y\tcmd/nova-y/main.go\tcmd/nova-y TestHelpExampleLinesRunAsPrinted\t1\t-\n")

	findings := filepath.Join(t.TempDir(), "f.tsv")
	require.NoError(t, os.WriteFile(findings, []byte("internal/none/x.go:1\twrong\tfix it\t\n"), 0o644))
	out := filepath.Join(t.TempDir(), "cards")
	code, stdout, stderr = runSprint(nil, "card", "generate", "--from", "findings", "--file", findings, "--repo-dir", repo, "--repo", "o/r", "--base", "dev", "--sha", sha, "--out", out)
	require.Equal(t, 0, code, "stdout: %s\nstderr: %s", stdout, stderr)
	brief, err := os.ReadFile(filepath.Join(out, "finding-internal-none-x.md"))
	require.NoError(t, err)
	assert.Contains(t, string(brief), "\nPATHS: internal/none/x.go, internal/none/*_test.go\nNEW: internal/none/x_test.go\n")
}
