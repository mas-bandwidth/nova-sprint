//go:build functional

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/goenv"
)

// A directory generated from a ledger in a fixture checkout is admitted whole by
// `nova-sprint add --brief-dir` into a real store, and the one-wave plan holds
// there: every card is wave 1, and the cards that share the ledger's path are
// chained with DEPENDS-ON (each on the one before it, in ledger order), so the
// add admits them with no --allow-shared-paths. A build is the functional tier's
// (nova-tools#4328). The checkout is on the sprint branch its stream lands on,
// since add refuses a card cut on dev outside the promotion stream
// (docs/SPEC-SPRINT.md section 7, the sprint branch).
func TestAGeneratedDirectoryIsAdmittedWholeAndItsWavesHold(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(goenv.Clean(os.Environ()), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	}
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(repo, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	write("cmd/a/a_test.go", "package main\n")
	write("cmd/b/b_test.go", "package main\n")
	write("internal/c/c_test.go", "package c\n")
	// the ledger's own test, whose package every card's START and PATHS name
	// (internal/ci/*.go), is at the base as it is in the real tree
	write("internal/ci/serial_test.go", "package ci\n\nimport \"testing\"\n\nfunc TestEveryTestOpensWithTParallel(t *testing.T) { t.Parallel() }\n")
	write("internal/ci/testdata/serial-tests_allowlist.txt", "# ceiling: 3\ncmd/a/a_test.go:TestA serial: t.Setenv\ncmd/b/b_test.go:TestB serial: t.Chdir\ninternal/c/c_test.go:TestC serial: os.Setenv\n")
	// origin is a bare twin of example/repo holding the sprint branch: add reads every
	// brief at the tip of its BASE: in the lander's clone of its REPO:
	origin := filepath.Join(t.TempDir(), "example", "repo.git")
	git("init", "-q", "-b", "sprint/s")
	git("init", "-q", "--bare", "-b", "sprint/s", origin)
	git("remote", "add", "origin", origin)
	git("add", ".")
	git("commit", "-q", "-m", "fixture")
	git("push", "-q", "origin", "HEAD:refs/heads/sprint/s")

	out := filepath.Join(t.TempDir(), "cards")
	exit, stdout, stderr := runCard("generate", "--from", "ledger", "--ledger", "serial-tests", "--repo-dir", repo, "--out", out)
	require.Equal(t, 0, exit, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stdout, "CARDS OK dir="+out+" cards=3 waves=1 tier=flash")
	manifest, err := os.ReadFile(filepath.Join(out, "manifest.tsv"))
	require.NoError(t, err)
	assert.Contains(t, string(manifest), "serial-tests-cmd-a-a\tcmd/a/a_test.go\tinternal/ci TestEveryTestOpensWithTParallel\t1\t-\n")
	assert.Contains(t, string(manifest), "serial-tests-cmd-b-b\tcmd/b/b_test.go\tinternal/ci TestEveryTestOpensWithTParallel\t1\tserial-tests-cmd-a-a\n")
	assert.Contains(t, string(manifest), "serial-tests-internal-c-c\tinternal/c/c_test.go\tinternal/ci TestEveryTestOpensWithTParallel\t1\tserial-tests-cmd-b-b\n")
	brief, err := os.ReadFile(filepath.Join(out, "serial-tests-cmd-a-a.md"))
	require.NoError(t, err)
	assert.Contains(t, string(brief), "REPO: example/repo\nBASE: sprint/s\nKIND: ledger\n")
	assert.Contains(t, string(brief), "\nSTOP: the ledger row for cmd/a/a_test.go in internal/ci/testdata/serial-tests_allowlist.txt shrinks from 1 to 0 and the class test TestEveryTestOpensWithTParallel stays green, and the STEP 4 gate passes\n")

	// the cards the sprint admits are the same cut with REPO: naming the twin, so
	// the lander clones it rather than example/repo over the network
	admit := filepath.Join(t.TempDir(), "cards")
	exit, stdout, stderr = runCard("generate", "--from", "ledger", "--ledger", "serial-tests", "--repo-dir", repo, "--repo", origin, "--out", admit)
	require.Equal(t, 0, exit, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stdout, "CARDS OK dir="+admit+" cards=3 waves=1 tier=flash")

	// The sprint, through the helper: initialize, seed its hook gate, then
	// admit the generated directory.
	sprint := newBuiltSprint(t)
	code, text := sprint.run("init", "--members", "m1", "--readers", "r1")
	require.Equal(t, 0, code, text)
	// The card admission uses the same nova-hook gate as every coordinator verb.
	// Seed the test store with a proven hook record; delivery is tested in nova-sprint.
	sprint.proveCoordinator()

	// the generated directory is admitted whole with no --allow-shared-paths: the
	// cards are chained, so no two independent cards share the ledger's path
	code, text = sprint.run("add", "--stream", "s", "--brief-dir", admit)
	require.Equal(t, 0, code, text)
	assert.Contains(t, text, "ADD OK stream=s cards=3 before=- moved=3 refused=0")

	// the admitted cards hold the chain in the store, not only in manifest.tsv: b
	// needs a and c needs b, so no two independent cards share the ledger's path
	code, text = sprint.run("card", "serial-tests-cmd-b-b")
	require.Equal(t, 0, code, text)
	assert.Contains(t, text, "ready")
	assert.Contains(t, text, "needs serial-tests-cmd-a-a")
	code, text = sprint.run("card", "serial-tests-cmd-a-a")
	require.Equal(t, 0, code, text)
	assert.Contains(t, text, "ready")
	code, text = sprint.run("card", "serial-tests-internal-c-c")
	require.Equal(t, 0, code, text)
	assert.Contains(t, text, "needs serial-tests-cmd-b-b")
}
