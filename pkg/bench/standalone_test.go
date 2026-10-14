package bench

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/testkit/fsx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandaloneLineAcceptsACloneDirectory(t *testing.T) {
	t.Parallel()
	dir := fsx.Tree(t, map[string]string{".git/objects/keep": ""})
	out, code := runLineIn(t, StandaloneLine(dir), nil)
	assert.Equal(t, 0, code, "a tree with a .git directory:\n%s", out)
}

func TestStandaloneLineRefusesAWorktreePointerAtAPathTheHostLacks(t *testing.T) {
	t.Parallel()
	gone := "/Volumes/nova/ai/rowan/working/clone/.git/worktrees/x"
	dir := fsx.Tree(t, map[string]string{".git": "gitdir: " + gone + "\n"})
	out, code := runLineIn(t, StandaloneLine(dir), nil)
	require.Equal(t, NotStandalone, code, "refusal exit:\n%s", out)
	for _, want := range []string{"is not standalone", gone, "does not exist on this host", "clone made on the bench"} {
		assert.Contains(t, out, want)
	}
}

func TestStandaloneLineRefusesAWorktreePointerAtAPathThatExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	other := filepath.Join(root, "mirror", "worktrees", "x")
	require.NoError(t, os.MkdirAll(other, 0o755))
	fsx.Write(t, filepath.Join(dir, ".git"), "gitdir: "+other+"\n")
	out, code := runLineIn(t, StandaloneLine(dir), nil)
	require.Equal(t, NotStandalone, code, "a worktree pointer at an existing path:\n%s", out)
	assert.NotContains(t, out, "does not exist")
}

func TestStandaloneLineRefusesATreeWithNoGit(t *testing.T) {
	t.Parallel()
	out, code := runLineIn(t, StandaloneLine(t.TempDir()), nil)
	require.Equal(t, NotStandalone, code, "exit:\n%s", out)
	assert.Contains(t, out, "has no .git directory")
}

func TestStageLineEndsWithTheStandaloneCheck(t *testing.T) {
	t.Parallel()
	s := MirrorStage{Mirror: "nova-bench/mirror/x.git", Remote: "https://example.invalid/x.git", Ref: GateRef("s", strings.Repeat("a", 40)), Sha: strings.Repeat("a", 40)}
	line := StageLine(s, "nova-bench/runs/r/repo")
	assert.True(t, strings.HasSuffix(line, StandaloneLine("nova-bench/runs/r/repo")+"; }"),
		"the stage line does not end with the standalone check:\n%s", line)
}

func TestWriteTreeRefusesAWorktreeGitFileWithGit(t *testing.T) {
	t.Parallel()
	dir := fsx.Tree(t, map[string]string{".git": "gitdir: /Volumes/nova/x/.git/worktrees/y\n"})
	var b bytes.Buffer
	err := WriteTree(&b, dir, true)
	require.Error(t, err, "WriteTree with git of a worktree")
	assert.Contains(t, err.Error(), "clone made on the bench")
	b.Reset()
	require.NoError(t, WriteTree(&b, dir, false), "a worktree's .git left out is fine")
}
