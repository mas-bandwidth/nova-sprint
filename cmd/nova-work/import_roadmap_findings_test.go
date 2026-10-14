package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImportNeedsAnExplicitRepoAndRoadmapVerbsTakeDryRun pins two findings of
// the v1.3 rating: import refuses without --repo (a cold dry run, and a whole
// organization's import, read every issue and can spend an org-wide gh budget),
// and every roadmap write verb takes --dry-run, prints the edit it would make
// and writes nothing.
func TestImportNeedsAnExplicitRepoAndRoadmapVerbsTakeDryRun(t *testing.T) {
	t.Parallel()

	tree := filepath.Join(t.TempDir(), "tree.lisp")
	g, calls := countingGitHub()
	res := workMain(g).Run("import", "--org", "mas-bandwidth", "--out", tree)
	require.Equal(t, 2, res.Code, "import without --repo: exit %d\n%s%s", res.Code, res.Stdout, res.Stderr)
	assert.Contains(t, res.Stderr, "--repo")
	assert.Contains(t, res.Stderr, "nova-work import --org mas-bandwidth --repo mas-bandwidth/<name>")
	require.Zero(t, calls.Load(), "the refusal ran a query")

	dir := workRecord(t)
	readDir := func(dir string) map[string]string {
		m := map[string]string{}
		for _, f := range []string{"docs/roadmap.sexp", "docs/fixes.sexp", "ROADMAP.md", "FIXES.md"} {
			b, err := os.ReadFile(filepath.Join(dir, f))
			require.NoError(t, err)
			m[f] = string(b)
		}
		return m
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"add", []string{"roadmap", "add", "--id", "f3", "--release", "v1.0.1", "--title", "F3", "--origin", "PR #3"}},
		{"remove", []string{"roadmap", "remove", "--id", "f1"}},
		{"pull", []string{"roadmap", "pull", "--id", "f2", "--group", "g"}},
		{"done", []string{"roadmap", "done", "--id", "f1", "--evidence", "PR #9"}},
		{"note", []string{"roadmap", "note", "--id", "f1", "--text", "A note."}},
	} {
		was := readDir(dir)
		args := append(append([]string{}, tc.args...), "--dry-run", "--repo", dir)
		out := workMain(unreachable(t)).Run(args...)
		require.Equal(t, 0, out.Code, "%v: exit %d\n%s%s", tc.args, out.Code, out.Stdout, out.Stderr)
		assert.Contains(t, out.Stdout+out.Stderr, "dry_run=true", tc.name)
		assert.Contains(t, out.Stdout+out.Stderr, "WROTE file=", tc.name)
		assert.Equal(t, was, readDir(dir), "%s: a dry run wrote", tc.name)
	}

	stale := workRecord(t)
	require.NoError(t, os.WriteFile(filepath.Join(stale, "ROADMAP.md"), []byte("by hand\n"), 0o644))
	was := readDir(stale)
	res = workMain(unreachable(t)).Run("roadmap", "render", "--dry-run", "--repo", stale)
	require.Equal(t, 0, res.Code, "render --dry-run: exit %d\n%s%s", res.Code, res.Stdout, res.Stderr)
	assert.Contains(t, res.Stdout+res.Stderr, "dry_run=true")
	assert.Contains(t, res.Stdout+res.Stderr, "ROADMAP.md")
	assert.Equal(t, was, readDir(stale), "render --dry-run wrote")
}
