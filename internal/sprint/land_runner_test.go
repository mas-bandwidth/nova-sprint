package sprint_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
)

// The revisions of the rig: pinRev is the nova-tools commit the tree's CI builds tlacheck
// from, otherRev one it does not.
const (
	pinRev   = "0c97d5958bf2b3cb1ad24e6e1b58f5b04e70347a"
	otherRev = "1111111111111111111111111111111111111111"
)

// pinWorkflow is a CI workflow pinning tlacheck as rocketnet's .github/workflows/tlc.yml
// does: a nova-tools clone checked out at a full commit, tlacheck built from it. The pin
// is on line 6.
func pinWorkflow(rev string) string {
	return "name: tlc\non: [push]\njobs:\n  records:\n    steps:\n" +
		"      - run: git -C \"$job/runner\" checkout --detach " + rev + "\n" +
		"      - run: git clone https://github.com/mas-bandwidth/nova-tools.git \"$job/runner\" && (cd \"$job/runner\" && go build -o \"$job/tlacheck\" ./tools/tlacheck)\n"
}

// runnerLike is the runner this binary was built with, its suite.go marked by mark: a
// runner of another revision, whose fingerprints differ from the built-in one's.
func runnerLike(t *testing.T, mark string) map[string][]byte {
	t.Helper()
	files, err := tlc.RunnerFiles()
	require.NoError(t, err)
	out := map[string][]byte{}
	for p, b := range files {
		out[p] = b
	}
	out[tlc.RunnerDir+"/suite.go"] = append(append([]byte{}, files[tlc.RunnerDir+"/suite.go"]...), []byte("\n// "+mark+"\n")...)
	return out
}

// runsWith is the tla/RUNS.tsv the runner writes for files: one record per case, its
// fingerprint the one runner gives it.
func runsWith(t *testing.T, files map[string]string, runner map[string][]byte) string {
	t.Helper()
	dir := writeTree(t, files)
	src := tlc.Source{TLADir: filepath.Join(dir, "tla"), Plan: []byte(files["tla/CASES.tsv"]), Runner: runner}
	cases, err := tlc.ParseCases(strings.NewReader(files["tla/CASES.tsv"]))
	require.NoError(t, err)
	var records []tlc.Record
	for _, c := range cases {
		fp, n, err := src.Fingerprint(c.Config)
		require.NoError(t, err)
		records = append(records, tlc.Record{Config: c.Config, Module: c.Module, InputSHA256: fp, InputFiles: n,
			JarSHA256: strings.Repeat("a", 64), JavaVersion: "21.0.12.1", Host: "linux-amd64", CPUs: 4,
			StartedUTC: "2026-10-06T12:00:00.000000+00:00", Workers: 2, Generated: "2", Distinct: "1", Seconds: "0.500",
			Exit: 0, Result: "PASS", Expected: c.Expected, Property: c.Property, Budget: "110", Mode: "bounded"})
	}
	var b bytes.Buffer
	require.NoError(t, tlc.WriteRecords(&b, records))
	return b.String()
}

// writeTree writes files into a fresh directory, the merged tree of a fake merge.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for f, s := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(s), 0o600))
	}
	return dir
}

// editA is the diff of a merge that edits tla/A.tla, which MCA reads.
const editA = "diff --git a/tla/A.tla b/tla/A.tla\n--- a/tla/A.tla\n+++ b/tla/A.tla\n@@ -5 +5 @@\n-Next == x' = x\n+Next == x' = 1 - x\n"

// fakeRunners is a RunnerSource serving revs from memory and counting its reads; a
// revision it does not hold is an error, as a fetch of an unknown commit is.
type fakeRunners struct {
	revs  map[string]map[string][]byte
	reads []string
}

func (f *fakeRunners) source(rev string) (map[string][]byte, error) {
	f.reads = append(f.reads, rev)
	files, ok := f.revs[rev]
	if !ok {
		return nil, errors.New("fatal: remote error: upload-pack: not our ref " + rev)
	}
	return files, nil
}

// The lander holds a tree's run records to the tree's own pinned runner (docs/SPEC-SPRINT.md
// section 7, "The run records"; Stella's find on rocketnet-cookie-key-rotates-on-its-period):
// records a pinned runner wrote are accepted although the runner built into nova-sprint
// fingerprints them otherwise; a record neither runner wrote is refused naming both; a
// tree that pins nothing is held to the built-in runner as before, and its refusal says
// so; a pin that cannot be read, or two that disagree, is one refusal on the pin.
func TestTheLanderHoldsRecordsToTheTreesPinnedRunner(t *testing.T) {
	t.Parallel()
	edited := with(recordsModels, map[string]string{"tla/A.tla": strings.Replace(recordsModels["tla/A.tla"], "x' = x", "x' = 1 - x", 1)})
	pinned := runnerLike(t, "nova-tools "+pinRev)
	built, err := tlc.RunnerFiles()
	require.NoError(t, err)
	builtName := sprint.BuiltRunnerName()
	runners := func() *fakeRunners { return &fakeRunners{revs: map[string]map[string][]byte{pinRev: pinned}} }

	t.Run("records the pinned runner wrote are accepted", func(t *testing.T) {
		t.Parallel()
		tree := with(edited, map[string]string{".github/workflows/tlc.yml": pinWorkflow(pinRev), "tla/RUNS.tsv": runsWith(t, edited, pinned)})
		require.NotEqual(t, runsWith(t, edited, pinned), runsWith(t, edited, built), "the rig's pinned runner fingerprints otherwise than the built-in one")
		f := runners()
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, f.source)
		require.NoError(t, err)
		assert.Empty(t, refused)
		assert.Equal(t, []string{pinRev}, f.reads, "the pinned revision is read, once")
	})
	t.Run("a record neither the pin nor the built-in runner wrote is refused naming both", func(t *testing.T) {
		t.Parallel()
		stray := runnerLike(t, "nova-tools "+otherRev)
		tree := with(edited, map[string]string{".github/workflows/tlc.yml": pinWorkflow(pinRev), "tla/RUNS.tsv": runsWith(t, edited, stray)})
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, runners().source)
		require.NoError(t, err)
		require.Len(t, refused, 1, "%v", refused)
		s := refused[0].String()
		assert.Contains(t, s, "tla/RUNS.tsv:2 has no current run record for the case MCA.cfg (group alpha) and the change edits tla/A.tla; ")
		assert.Contains(t, s, "the record was held to the runner the tree pins, nova-tools "+pinRev+" (.github/workflows/tlc.yml:6, runner digest ")
		assert.Contains(t, s, "; it matches neither that nor "+builtName)
		assert.Contains(t, s, "run make tlc TLC_GROUP=alpha")
		assert.NotContains(t, s, "\n", "one line")
	})
	t.Run("a record the built-in runner wrote is refused when the tree pins another", func(t *testing.T) {
		t.Parallel()
		tree := with(edited, map[string]string{".github/workflows/tlc.yml": pinWorkflow(pinRev), "tla/RUNS.tsv": runsWith(t, edited, built)})
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, runners().source)
		require.NoError(t, err)
		require.Len(t, refused, 1, "%v", refused)
		assert.Contains(t, refused[0].String(), "nova-tools "+pinRev)
		assert.Contains(t, refused[0].String(), ", and it is the one "+builtName+" gives the case, not the tree's pin")
	})
	t.Run("a tree that pins nothing is held to the built-in runner", func(t *testing.T) {
		t.Parallel()
		f := &fakeRunners{}
		tree := with(edited, map[string]string{"tla/RUNS.tsv": runsWith(t, edited, built)})
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, f.source)
		require.NoError(t, err)
		assert.Empty(t, refused)

		tree["tla/RUNS.tsv"] = runsWith(t, edited, pinned)
		refused, err = sprint.RecordsRefusals(writeTree(t, tree), editA, f.source)
		require.NoError(t, err)
		require.Len(t, refused, 1, "%v", refused)
		assert.Contains(t, refused[0].String(), "; the record was held to "+builtName+"; the tree pins no runner (no .github/workflows file builds tlacheck at a nova-tools commit); run make tlc")
		assert.Empty(t, f.reads, "no pin, no read")
	})
	t.Run("a workflow that installs tlacheck at a commit pins it too", func(t *testing.T) {
		t.Parallel()
		wf := "jobs:\n  tlc:\n    steps:\n      - run: go install github.com/mas-bandwidth/nova-tools/tools/tlacheck@" + pinRev + "\n"
		tree := with(edited, map[string]string{".github/workflows/records.yaml": wf, "tla/RUNS.tsv": runsWith(t, edited, pinned)})
		f := runners()
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, f.source)
		require.NoError(t, err)
		assert.Empty(t, refused)
		assert.Equal(t, []string{pinRev}, f.reads)
	})
	t.Run("a tree that holds the runner is held to its own files, not a pin", func(t *testing.T) {
		t.Parallel()
		own := runnerLike(t, "the tree's own")
		tree := with(edited, map[string]string{".github/workflows/tlc.yml": pinWorkflow(pinRev), "tla/RUNS.tsv": runsWith(t, edited, own)})
		for p, b := range own {
			tree[p] = string(b)
		}
		f := runners()
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, f.source)
		require.NoError(t, err)
		assert.Empty(t, refused)
		assert.Empty(t, f.reads)
	})
	t.Run("a pin the lander cannot read is one refusal on the pin", func(t *testing.T) {
		t.Parallel()
		tree := with(edited, map[string]string{".github/workflows/tlc.yml": pinWorkflow(otherRev), "tla/RUNS.tsv": runsWith(t, edited, pinned)})
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, runners().source)
		require.NoError(t, err)
		require.Len(t, refused, 1, "%v", refused)
		assert.Equal(t, ".github/workflows/tlc.yml:6 pins the TLC runner at nova-tools "+otherRev+
			", which the lander could not read, so no edited model's run record can be checked: fatal: remote error: upload-pack: not our ref "+otherRev,
			refused[0].String())
	})
	t.Run("two workflows pinning two revisions are refused naming both", func(t *testing.T) {
		t.Parallel()
		tree := with(edited, map[string]string{
			".github/workflows/a.yml": pinWorkflow(pinRev), ".github/workflows/b.yml": pinWorkflow(otherRev),
			"tla/RUNS.tsv": runsWith(t, edited, pinned),
		})
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, runners().source)
		require.NoError(t, err)
		require.Len(t, refused, 1, "%v", refused)
		assert.Contains(t, refused[0].String(), pinRev+" (.github/workflows/a.yml:6) and "+otherRev+" (.github/workflows/b.yml:6)")
	})
}

// The lander's RunnerSource reads a cached nova-tools revision's runner files byte for
// byte, trailing newline included, without fetching (a local bare repository; no network).
func TestTheRunnerSourceReadsACachedRevision(t *testing.T) {
	t.Parallel()
	want := runnerLike(t, "cached")
	work := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	git(work, "init", "-q")
	for p, b := range want {
		f := filepath.Join(work, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(f), 0o755))
		require.NoError(t, os.WriteFile(f, b, 0o600))
	}
	git(work, "add", "-A")
	git(work, "commit", "-q", "-m", "runner")
	rev := git(work, "rev-parse", "HEAD")
	cache := filepath.Join(t.TempDir(), "nova-tools.git")
	git(t.TempDir(), "clone", "-q", "--bare", work, cache)

	got, err := sprint.GitRunnerSource(cache)(rev)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
