package tlc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckedFilesListsTheResultAndInputListFiles: CheckedFiles is ResultFiles
// plus InputListFiles.
func TestCheckedFilesListsTheResultAndInputListFiles(t *testing.T) {
	t.Parallel()
	want := append(append([]string{}, ResultFiles...), InputListFiles...)
	assert.Equal(t, want, CheckedFiles())
}

// TestCheckRunnerAcceptsMatchingRunnerFiles: a root whose runner files are the
// ones this binary was built from passes.
func TestCheckRunnerAcceptsMatchingRunnerFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(RunnerDir))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, name := range CheckedFiles() {
		raw, err := sources.ReadFile(name)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), raw, 0o644))
	}
	assert.NoError(t, CheckRunner(root), "CheckRunner")
}

// TestCheckRunnerNoticesAChangedRunnerFile: a changed runner file is refused,
// naming the file.
func TestCheckRunnerNoticesAChangedRunnerFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(RunnerDir))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	raw, err := sources.ReadFile("outcome.go")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outcome.go"), append(raw, []byte("\n// changed\n")...), 0o644))
	err = CheckRunner(root)
	require.Error(t, err, "CheckRunner accepted a changed runner file")
	assert.ErrorContains(t, err, "outcome.go")
}

// TestCheckRunnerNoopsWithoutARunnerDir: a root with no pkg/tlc has nothing to
// check.
func TestCheckRunnerNoopsWithoutARunnerDir(t *testing.T) {
	t.Parallel()
	assert.NoError(t, CheckRunner(t.TempDir()), "CheckRunner on an empty root")
}

// TestFingerprintNoticesAChangedModule: editing a case's module changes its
// fingerprint, so the group it belongs to becomes stale.
func TestFingerprintNoticesAChangedModule(t *testing.T) {
	t.Parallel()
	root, cases, src := recordsPlan(t)
	c := cases[0]
	fresh := currentRecord(t, src, c)

	path := filepath.Join(root, "tla", c.Module)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(raw, []byte("\n\\* edited\n")...), 0o644))

	fp, _, err := src.Fingerprint(c.Config)
	require.NoError(t, err)
	assert.NotEqual(t, fresh.InputSHA256, fp, "editing a module did not change the fingerprint")
	stale, err := StaleGroups(src, cases, []Record{fresh})
	require.NoError(t, err)
	assert.Equal(t, []string{c.Group}, stale)
}

// TestFingerprintNoticesAChangedConfig: editing a case's configuration changes
// its fingerprint.
func TestFingerprintNoticesAChangedConfig(t *testing.T) {
	t.Parallel()
	root, cases, src := recordsPlan(t)
	c := cases[0]
	fp0, _, err := src.Fingerprint(c.Config)
	require.NoError(t, err)
	path := filepath.Join(root, "tla", c.Config)
	require.NoError(t, os.WriteFile(path, []byte("SPECIFICATION Spec\nCONSTANT N = 3\n"), 0o644))
	fp1, _, err := src.Fingerprint(c.Config)
	require.NoError(t, err)
	assert.NotEqual(t, fp0, fp1, "editing a configuration did not change the fingerprint")
}

// TestFingerprintNoticesAnAddedModule: adding a module the case extends covers a
// new input and changes its fingerprint.
func TestFingerprintNoticesAnAddedModule(t *testing.T) {
	t.Parallel()
	root, cases, src := recordsPlan(t)
	c := cases[0]
	fp0, files0, err := src.Fingerprint(c.Config)
	require.NoError(t, err)

	modulePath := filepath.Join(root, "tla", c.Module)
	raw, err := os.ReadFile(modulePath)
	require.NoError(t, err)
	header, body, _ := strings.Cut(string(raw), "\n")
	edited := header + "\nEXTENDS AddedModule\n" + body
	require.NoError(t, os.WriteFile(modulePath, []byte(edited), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tla", "AddedModule.tla"),
		[]byte("---- MODULE AddedModule ----\nVARIABLE y\n====\n"), 0o644))

	fp1, files1, err := src.Fingerprint(c.Config)
	require.NoError(t, err)
	assert.NotEqual(t, fp0, fp1, "adding a module did not change the fingerprint")
	assert.Greater(t, files1, files0, "adding a module must cover more inputs")
}
