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

// initDeltaRepo makes a throwaway git repository holding a delta-shaped C++ test
// source that already registers DeltaBitsWiderThan32, and returns its directory and
// base commit.
func initDeltaRepo(t *testing.T) (dir, sha string) {
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
	write("test/delta_test.cpp", "#include \"unit_test.h\"\nREGISTER_UNIT_TEST(DeltaBitsWiderThan32)\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	return dir, git("rev-parse", "HEAD")
}

// runnerCard is a brief whose TEST line is the runner form `<runner> <file> <case>`
// and whose PATHS names the C++ test source.
func runnerCard(sha, test string) []byte {
	return []byte("RESULT: c1 sha=" + sha + " tier: heavy\n" +
		"REPO: example.com/m\n" +
		"BASE: sprint/x\n" +
		"base-sha: " + sha + "\n" +
		"PATHS: test/delta_test.cpp\n" +
		"KIND: fix-red\n" +
		"TEST: " + test + "\n" +
		"\n" +
		"THE TASK.\n")
}

// The absent-at-base check runs on every form the card lint accepts. A runner-form
// TEST naming a case the base already registers is refused; one naming a new case
// passes; a brief handed no tree draws MISSING.
func TestTestAbsentAtBaseForEveryRunner(t *testing.T) {
	t.Parallel()
	dir, sha := initDeltaRepo(t)
	bb := BriefBase{Repo: dir, Sha: sha}

	refused, _ := LintBrief(runnerCard(sha, "make test/delta_test.cpp DeltaBitsWiderThan32"), bb)
	got := findingsWith(refused, "donewhen-test-name")
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Excerpt, "exists")

	newCase, _ := LintBrief(runnerCard(sha, "make test/delta_test.cpp SomeNewCase"), bb)
	assert.Empty(t, findingsWith(newCase, "donewhen-test-name"), "%+v", newCase)

	missingTarget, _ := LintBrief(runnerCard(sha, "make test/nonexistent.cpp SomeNewCase"), bb)
	missTarget := findingsWith(missingTarget, "donewhen-test-name")
	require.Len(t, missTarget, 1)
	assert.Contains(t, missTarget[0].Excerpt, "target must exist")

	missing, _ := LintBrief(runnerCard(sha, "make test/delta_test.cpp SomeNewCase"), BriefBase{})
	miss := findingsWith(missing, "donewhen-test-name")
	require.Len(t, miss, 1)
	assert.Contains(t, miss[0].Excerpt, "MISSING")
}

// The base card check (LintCardBase) reads the same runner form off DONE-WHEN: a
// case the base registers is refused, a new one passes.
func TestLintCardBaseReadsRunnerFormDoneWhen(t *testing.T) {
	t.Parallel()
	dir, sha := initDeltaRepo(t)
	bc := BaseCheck{Repo: dir}
	card := func(doneWhen string) []byte {
		return []byte("RESULT: c1 sha=" + sha + " tier: pro\n" +
			"base-sha: " + sha + "\n" +
			"PATHS: test/delta_test.cpp\n" +
			"KIND: fix\n" +
			"LEG: go\n" +
			"DEADLINE: 2700\n" +
			"DONE-WHEN: " + doneWhen + "\n" +
			"\n" +
			"THE TASK.\n")
	}

	present := LintCardBase(card("make test/delta_test.cpp DeltaBitsWiderThan32"), bc)
	got := findingsWith(present, "donewhen-test-name")
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Excerpt, "exists at base-sha")

	absent := LintCardBase(card("make test/delta_test.cpp SomeNewCase"), bc)
	assert.Empty(t, findingsWith(absent, "donewhen-test-name"), "%+v", absent)

	missingTarget := LintCardBase(card("make test/nonexistent.cpp SomeNewCase"), bc)
	missTarget := findingsWith(missingTarget, "donewhen-test-name")
	require.Len(t, missTarget, 1)
	assert.Contains(t, missTarget[0].Excerpt, "target must exist")
}
