package swarm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initBaseRepo makes a throwaway git repository under t.TempDir() holding a Go package
// with one existing test, a Python test file, and returns its directory and base commit.
func initBaseRepo(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	git("init", "-q", "-b", "main")
	write("go.mod", "module example.com/m\n\ngo 1.21\n")
	write("pkg/decide/entry.go", "package decide\n")
	write("pkg/decide/entry_test.go", "package decide\n\nimport \"testing\"\n\nfunc TestExisting(t *testing.T) {}\n")
	write("tests/test_x.py", "def test_one():\n    pass\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	return dir, git("rev-parse", "HEAD")
}

// baseCard is a card whose header carries a base-sha, PATHS and the lines the other base
// checks read, so a test can look at one check at a time.
func baseCard(sha, paths, doneWhen string) []byte {
	return []byte("RESULT: c1 sha=000000000000 tier: pro\n" +
		"base-sha: " + sha + "\n" +
		"PATHS: " + paths + "\n" +
		"KIND: fix\n" +
		"LEG: go\n" +
		"DEADLINE: 2700\n" +
		"DONE-WHEN: " + doneWhen + "\n" +
		"\n" +
		"THE TASK.\n")
}

func findingsWith(fs []CardHeaderFinding, check string) []CardHeaderFinding {
	var out []CardHeaderFinding
	for _, f := range fs {
		if f.Check == check {
			out = append(out, f)
		}
	}
	return out
}

// A PATHS entry that resolves at base-sha passes; one that names nothing there draws
// paths-at-base, the class-9 failure.
func TestLintCardBaseResolvesPathsAtBaseSha(t *testing.T) {
	t.Parallel()
	dir, sha := initBaseRepo(t)
	bc := BaseCheck{Repo: dir}

	present := LintCardBase(baseCard(sha, "pkg/decide/entry.go", "go test ./pkg/decide -run TestNew"), bc)
	assert.Empty(t, findingsWith(present, "paths-at-base"), "%+v", present)

	missing := LintCardBase(baseCard(sha, "pkg/decide/missing.go", "go test ./pkg/decide -run TestNew"), bc)
	got := findingsWith(missing, "paths-at-base")
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Excerpt, "does not exist at base-sha")
}

// A base check with no repository handed over is MISSING, never a pass.
func TestLintCardBaseWithoutARepoIsMissing(t *testing.T) {
	t.Parallel()
	_, sha := initBaseRepo(t)
	got := findingsWith(LintCardBase(baseCard(sha, "pkg/decide/entry.go", "go test ./pkg/decide -run TestNew"), BaseCheck{}), "paths-at-base")
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Excerpt, "MISSING")
}

// DONE-WHEN names a literal test: one absent at base can be red there (no finding); one
// that already exists at base cannot, and draws donewhen-test-name.
func TestLintCardBaseReadsDoneWhenTestNames(t *testing.T) {
	t.Parallel()
	dir, sha := initBaseRepo(t)
	bc := BaseCheck{Repo: dir}

	absent := LintCardBase(baseCard(sha, "pkg/decide/entry.go", "go test ./pkg/decide -run TestNew"), bc)
	assert.Empty(t, findingsWith(absent, "donewhen-test-name"), "%+v", absent)

	present := LintCardBase(baseCard(sha, "pkg/decide/entry.go", "go test ./pkg/decide -run TestExisting"), bc)
	got := findingsWith(present, "donewhen-test-name")
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Excerpt, "exists at base-sha")

	pytestAbsent := LintCardBase(baseCard(sha, "pkg/decide/entry.go", "pytest tests/test_x.py::test_missing"), bc)
	assert.Empty(t, findingsWith(pytestAbsent, "donewhen-test-name"), "%+v", pytestAbsent)

	pytestPresent := LintCardBase(baseCard(sha, "pkg/decide/entry.go", "pytest tests/test_x.py::test_one"), bc)
	assert.Len(t, findingsWith(pytestPresent, "donewhen-test-name"), 1)
}

// goTestTargets is the package arguments of one `go test` command: flags and their values
// are left out, import paths are kept.
func TestGoTestTargetsReadsPackagesAndNotFlags(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"./pkg/decide", "./x/..."}, goTestTargets("go test -count=1 ./pkg/decide ./x/... -timeout 600s"))
	assert.Equal(t, []string{"github.com/org/repo/x"}, goTestTargets("go test github.com/org/repo/x"))
	assert.Empty(t, goTestTargets("go test -run TestA"))
}

// pytestTargets is the file and directory arguments of one pytest command: flags, the
// values of value-taking flags, and node ids are left out.
func TestPytestTargetsReadsFilesAndNotFlags(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"tests/test_x.py"}, pytestTargets("pytest -k test_x tests/test_x.py"))
	assert.Empty(t, pytestTargets("pytest tests/test_x.py::test_one"))
	assert.Equal(t, []string{"tests/sub"}, pytestTargets("pytest --rootdir tests tests/sub"))
}

// The leg and p95 checks: a leg the table carries passes, one it does not is refused, and
// a deadline below the kind's p95 wall is refused.
func TestLintCardBaseChecksLegAndDeadline(t *testing.T) {
	t.Parallel()
	dir, sha := initBaseRepo(t)
	card := baseCard(sha, "pkg/decide/entry.go", "go test ./pkg/decide -run TestNew")

	good := BaseCheck{Repo: dir, Legs: FleetLegs{"go": true}, P95: KindP95{"fix": 2000}}
	fs := LintCardBase(card, good)
	assert.Empty(t, findingsWith(fs, "leg-in-fleet"), "%+v", fs)
	assert.Empty(t, findingsWith(fs, "deadline-p95"), "%+v", fs)

	bad := BaseCheck{Repo: dir, Legs: FleetLegs{"sbcl": true}, P95: KindP95{"fix": 3000}}
	fs = LintCardBase(card, bad)
	require.Len(t, findingsWith(fs, "leg-in-fleet"), 1)
	require.Len(t, findingsWith(fs, "deadline-p95"), 1)

	// No evidence handed over is MISSING, never a pass.
	fs = LintCardBase(card, BaseCheck{Repo: dir})
	assert.Contains(t, findingsWith(fs, "leg-in-fleet")[0].Excerpt, "MISSING")
	assert.Contains(t, findingsWith(fs, "deadline-p95")[0].Excerpt, "MISSING")
}

// A STEP that runs `git push` or `gh` is refused: the wall holds no credential.
func TestLintCardBaseRefusesPushSteps(t *testing.T) {
	t.Parallel()
	dir, sha := initBaseRepo(t)
	card := []byte("RESULT: c1 sha=000000000000 tier: pro\n" +
		"base-sha: " + sha + "\n" +
		"PATHS: none\n" +
		"KIND: fix\n" +
		"\n" +
		"THE TASK.\n\n" +
		"STEP 1. Run git push origin main.\n")
	fs := LintCardBase(card, BaseCheck{Repo: dir})
	require.Len(t, findingsWith(fs, "no-push-steps"), 1)
}

// deadlineSeconds reads a number of seconds, a minutes spelling and the `finish within`
// form; anything else is no deadline.
func TestDeadlineSecondsReadsTheForms(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"2700", 2700, true},
		{"2700s", 2700, true},
		{"45m", 2700, true},
		{"finish within 45 minutes", 2700, true},
		{"later", 0, false},
	} {
		got, ok := deadlineSeconds(tt.in)
		assert.Equal(t, tt.ok, ok, tt.in)
		if tt.ok {
			assert.Equal(t, tt.want, got, tt.in)
		}
	}
}

// ReadFleetLegs reads the fleet's leg table: one leg per line, a header row skipped, legs
// lower cased.
func TestReadFleetLegsReadsTheTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "legs.txt")
	require.NoError(t, os.WriteFile(p, []byte("leg\nGO\nsbcl\n# comment\n\n"), 0o644))
	got, err := ReadFleetLegs(p)
	require.NoError(t, err)
	assert.Equal(t, FleetLegs{"go": true, "sbcl": true}, got)

	_, err = ReadFleetLegs(filepath.Join(dir, "absent.txt"))
	require.Error(t, err)
}

// ReadKindP95 reads `<kind> <seconds>` rows; a header row is skipped, a later bad row is
// an error.
func TestReadKindP95ReadsTheTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "p95.txt")
	require.NoError(t, os.WriteFile(p, []byte("kind seconds\nfix 2000\nread 1500s\n"), 0o644))
	got, err := ReadKindP95(p)
	require.NoError(t, err)
	assert.Equal(t, KindP95{"fix": 2000, "read": 1500}, got)

	bad := filepath.Join(dir, "bad.txt")
	require.NoError(t, os.WriteFile(bad, []byte("fix 2000\nread notanumber\n"), 0o644))
	_, err = ReadKindP95(bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a number")
}

// contractSha is the `sha=<hex>` on the contract line, or "".
func TestContractShaReadsTheContractLine(t *testing.T) {
	t.Parallel()
	assert.Equal(t, testBaseSha40, contractSha("RESULT: c1 sha="+testBaseSha40+" tier: pro"))
	assert.Equal(t, "", contractSha("RESULT: c1 tier: pro"))
}

// oneLineCap caps a value at n bytes for an excerpt.
func TestOneLineCapCapsTheValue(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "short", oneLineCap("short", 10))
	assert.Equal(t, "abcdefghij...", oneLineCap("abcdefghijklmnop", 10))
}

// doneWhenTests reads every literal test a DONE-WHEN names: a go -run, a pytest node id, a
// pytest -k, and a cargo test name; a go test with no -run names none.
func TestDoneWhenTestsReadsTheRunners(t *testing.T) {
	t.Parallel()
	tests, why := doneWhenTests("go test ./pkg -run TestA")
	require.Empty(t, why)
	require.Len(t, tests, 1)
	assert.Equal(t, "go", tests[0].runner)
	assert.Equal(t, "TestA", tests[0].name)

	tests, why = doneWhenTests("pytest tests/test_x.py::test_one")
	require.Empty(t, why)
	require.Len(t, tests, 1)
	assert.Equal(t, "pytest", tests[0].runner)
	assert.Equal(t, "test_one", tests[0].name)

	tests, why = doneWhenTests("pytest -k test_one tests/")
	require.Empty(t, why)
	require.Len(t, tests, 1)
	assert.Equal(t, "test_one", tests[0].name)

	tests, why = doneWhenTests("cargo test my_test")
	require.Empty(t, why)
	require.Len(t, tests, 1)
	assert.Equal(t, "cargo", tests[0].runner)
	assert.Equal(t, "my_test", tests[0].name)

	_, why = doneWhenTests("go test ./pkg")
	assert.Contains(t, why, "no `-run")
}

// pytestTakesValue says whether a pytest option word is followed by its value.
func TestPytestTakesValueReadsTheOption(t *testing.T) {
	t.Parallel()
	assert.True(t, pytestTakesValue("-k"))
	assert.True(t, pytestTakesValue("--rootdir"))
	assert.False(t, pytestTakesValue("--rootdir=tests"))
	assert.False(t, pytestTakesValue("-ktest_x"))
	assert.False(t, pytestTakesValue("tests/test_x.py"))
}

// newTestFile is a literal path whose base name is a test file; a glob is never one.
func TestNewTestFileReadsTheBaseName(t *testing.T) {
	t.Parallel()
	assert.True(t, newTestFile("pkg/x_test.go"))
	assert.True(t, newTestFile("tests/test_x.py"))
	assert.False(t, newTestFile("pkg/x.go"))
	assert.False(t, newTestFile("pkg/*_test.go"))
}

// pushStepLines is every line inside a STEP that runs `git push` or `gh`; a RULES paragraph
// is not read as a command.
func TestPushStepLinesReadsTheSteps(t *testing.T) {
	t.Parallel()
	raw := []byte("STEP 1. Run git push origin main.\n" +
		"STEP 2. Do the thing.\n" +
		"gh pr merge\n" +
		"RULES.\n" +
		"no gh, no push\n")
	got := pushStepLines(raw)
	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].line)
	assert.Equal(t, 3, got[1].line)
}
