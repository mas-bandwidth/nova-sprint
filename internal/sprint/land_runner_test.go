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
// is on line 13.
func pinWorkflow(rev string) string {
	return "name: tlc\non: [push]\njobs:\n  records:\n    runs-on: [self-hosted, linux]\n    steps:\n" +
		"      - uses: actions/checkout@v4\n" +
		"      - name: Check records\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          job=$(mktemp -d \"$RUNNER_TEMP/rocketnet-tlc.XXXXXX\")\n" +
		"          git -c credential.helper= clone --filter=blob:none --no-checkout https://github.com/mas-bandwidth/nova-tools.git \"$job/runner\"\n" +
		"          git -C \"$job/runner\" checkout --detach " + rev + "\n" +
		"          (cd \"$job/runner\" && go build -o \"$job/tlacheck\" ./tools/tlacheck)\n" +
		"          \"$job/tlacheck\" groups --root . --stale\n"
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
		assert.Contains(t, s, "the record was held to the runner the tree pins, nova-tools "+pinRev+" (.github/workflows/tlc.yml:13, runner digest ")
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
		assert.Equal(t, ".github/workflows/tlc.yml:13 pins the TLC runner at nova-tools "+otherRev+
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
		assert.Equal(t, ".github/workflows/b.yml:13 pins the TLC runner at nova-tools "+otherRev+", and .github/workflows/a.yml:13 pins it at "+pinRev+
			"; the tree's records have one runner, so no edited model's run record can be checked", refused[0].String())
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

// The pin is read from the workflows as YAML and their run scripts as shell words
// (land_runner_pin.go; Stella's HOLD on #344): a YAML or shell comment pins nothing, a
// checkout of another repository pins nothing, a revision that is not exactly a full
// commit is refused, and a workflow the lander cannot read or parse is a refusal naming
// it, never a silent fall back to the built-in runner.
func TestTheRunnerPinIsReadAsYAMLAndShell(t *testing.T) {
	t.Parallel()
	edited := with(recordsModels, map[string]string{"tla/A.tla": strings.Replace(recordsModels["tla/A.tla"], "x' = x", "x' = 1 - x", 1)})
	pinned := runnerLike(t, "nova-tools "+pinRev)
	built, err := tlc.RunnerFiles()
	require.NoError(t, err)
	check := func(t *testing.T, workflows map[string]string, runner map[string][]byte) ([]sprint.DocFix, []string) {
		t.Helper()
		tree := with(edited, map[string]string{"tla/RUNS.tsv": runsWith(t, edited, runner)})
		for f, s := range workflows {
			tree[".github/workflows/"+f] = s
		}
		f := &fakeRunners{revs: map[string]map[string][]byte{pinRev: pinned, otherRev: runnerLike(t, "other")}}
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, f.source)
		require.NoError(t, err)
		return refused, f.reads
	}
	accepted := func(t *testing.T, workflows map[string]string) {
		t.Helper()
		refused, reads := check(t, workflows, pinned)
		assert.Empty(t, refused)
		assert.Equal(t, []string{pinRev}, reads, "the pin, and only the pin, is read")
	}

	t.Run("a YAML comment pins nothing", func(t *testing.T) {
		t.Parallel()
		accepted(t, map[string]string{"tlc.yml": "jobs:\n  tlc:\n    steps:\n" +
			"      # - run: git -C \"$job/runner\" checkout --detach " + otherRev + "\n" +
			"      - run: go install github.com/mas-bandwidth/nova-tools/tools/tlacheck@" + pinRev + "\n"})
	})
	t.Run("a shell comment pins nothing", func(t *testing.T) {
		t.Parallel()
		wf := strings.Replace(pinWorkflow(pinRev), "          set -euo pipefail\n",
			"          set -euo pipefail\n          # git -C \"$job/runner\" checkout --detach "+otherRev+"\n", 1)
		wf = strings.Replace(wf, "checkout --detach "+pinRev+"\n", "checkout --detach "+pinRev+" # was "+otherRev+"\n", 1)
		accepted(t, map[string]string{"tlc.yml": wf})
	})
	t.Run("a checkout of another repository is no pin", func(t *testing.T) {
		t.Parallel()
		other := "jobs:\n  fixtures:\n    steps:\n      - name: fixtures for tlacheck, beside nova-tools\n        run: |\n" +
			"          git clone https://github.com/mas-bandwidth/rocketnet-fixtures.git fx\n" +
			"          git -C fx checkout --detach " + otherRev + "\n" +
			"          (cd fx && git checkout " + otherRev + ")\n"
		accepted(t, map[string]string{"tlc.yml": pinWorkflow(pinRev), "fixtures.yml": other})

		refused, reads := check(t, map[string]string{"fixtures.yml": other}, built)
		assert.Empty(t, refused, "alone it pins nothing: the built-in runner holds the records")
		assert.Empty(t, reads)
	})
	t.Run("a cd into the clone then a checkout pins", func(t *testing.T) {
		t.Parallel()
		accepted(t, map[string]string{"tlc.yml": "jobs:\n  tlc:\n    steps:\n      - run: |\n" +
			"          git clone --depth 1 https://github.com/mas-bandwidth/nova-tools runner\n" +
			"          cd runner && git checkout -q " + pinRev + " && go build ./tools/tlacheck\n"})
	})
	t.Run("an actions/checkout of nova-tools at a full commit pins", func(t *testing.T) {
		t.Parallel()
		accepted(t, map[string]string{"tlc.yml": "jobs:\n  tlc:\n    steps:\n      - uses: actions/checkout@v4\n" +
			"      - uses: actions/checkout@v4\n        with:\n          repository: mas-bandwidth/nova-tools\n          ref: " + pinRev + "\n          path: runner\n"})
	})
	for name, c := range map[string]struct{ wf, want string }{
		"a parent of the commit": {strings.Replace(pinWorkflow(pinRev), pinRev, pinRev+"^", 1),
			".github/workflows/tlc.yml:13 checks out nova-tools at \"" + pinRev + "^\", which is not a full commit"},
		"a branch": {strings.Replace(pinWorkflow(pinRev), pinRev, "main", 1),
			".github/workflows/tlc.yml:13 checks out nova-tools at \"main\", which is not a full commit"},
		"an abbreviation": {strings.Replace(pinWorkflow(pinRev), pinRev, pinRev[:12], 1),
			".github/workflows/tlc.yml:13 checks out nova-tools at \"" + pinRev[:12] + "\", which is not a full commit"},
		"a tag installed": {"jobs:\n  t:\n    steps:\n      - run: go install github.com/mas-bandwidth/nova-tools/tools/tlacheck@v1.2.26\n",
			".github/workflows/tlc.yml:4 installs tlacheck at \"v1.2.26\", which is not a full commit"},
		"an actions/checkout ref that is a branch": {"jobs:\n  t:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          repository: mas-bandwidth/nova-tools\n          ref: main\n",
			".github/workflows/tlc.yml:7 checks out nova-tools at \"main\", which is not a full commit"},
	} {
		t.Run("a revision that is not a full commit is refused: "+name, func(t *testing.T) {
			t.Parallel()
			refused, reads := check(t, map[string]string{"tlc.yml": c.wf}, pinned)
			require.Len(t, refused, 1, "%v", refused)
			assert.Contains(t, refused[0].String(), c.want)
			assert.Contains(t, refused[0].String(), "so no edited model's run record can be checked")
			assert.Empty(t, reads, "nothing is read for a pin that is no commit")
		})
	}
	t.Run("a line longer than a scanner's buffer before the pin still finds it", func(t *testing.T) {
		t.Parallel()
		wf := strings.Replace(pinWorkflow(pinRev), "jobs:\n", "env:\n  BIG: "+strings.Repeat("x", 2<<20)+"\njobs:\n", 1)
		accepted(t, map[string]string{"tlc.yml": wf})
	})
	t.Run("a workflow that is not YAML is a refusal naming it", func(t *testing.T) {
		t.Parallel()
		refused, reads := check(t, map[string]string{"tlc.yml": pinWorkflow(pinRev), "broken.yml": "jobs:\n  a: [\n    b: c\n"}, built)
		require.Len(t, refused, 1, "%v", refused)
		assert.True(t, strings.HasPrefix(refused[0].String(), ".github/workflows/broken.yml:"), refused[0].String())
		assert.Contains(t, refused[0].String(), "is not YAML the lander can read to find the TLC runner it pins")
		assert.Empty(t, reads)
	})
	t.Run("a workflow that cannot be read is a refusal naming it", func(t *testing.T) {
		t.Parallel()
		tree := with(edited, map[string]string{"tla/RUNS.tsv": runsWith(t, edited, built), ".github/workflows/tlc.yml/x": "a directory named like a workflow\n"})
		refused, err := sprint.RecordsRefusals(writeTree(t, tree), editA, (&fakeRunners{}).source)
		require.NoError(t, err)
		require.Len(t, refused, 1, "%v", refused)
		assert.True(t, strings.HasPrefix(refused[0].String(), ".github/workflows/tlc.yml:1 cannot be read to find the TLC runner it pins: "), refused[0].String())
	})
}
