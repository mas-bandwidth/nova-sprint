package sprint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkMirror makes a bare mirror of a repository named name with a branch ref at
// $dir/<name>.git, the shape the bench mirror has (~/nova-bench/mirror/<name>.git).
func mkMirror(t *testing.T, dir, name, ref string) {
	t.Helper()
	work := filepath.Join(dir, "work-"+name)
	require.NoError(t, os.MkdirAll(work, 0o755))
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		require.NoError(t, cmd.Run(), "git %v", args)
	}
	run("init", "--quiet", "-b", ref)
	require.NoError(t, os.WriteFile(filepath.Join(work, "README.md"), []byte(name), 0o644))
	run("add", "README.md")
	run("-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", name)
	// a bare mirror, as the bench mirror is
	cmd := exec.Command("git", "clone", "--quiet", "--bare", work, filepath.Join(dir, name+".git"))
	require.NoError(t, cmd.Run())
}

func TestParseSiblingsReadsTheHeader(t *testing.T) {
	t.Parallel()
	brief := "SIBLINGS: mas-bandwidth/serialize@abc, mas-bandwidth/serialize.c@def\n"
	got := ParseSiblings(brief)
	require.Len(t, got, 2)
	assert.Equal(t, Sibling{Name: "serialize", Path: "mas-bandwidth/serialize", Ref: "abc"}, got[0])
	assert.Equal(t, Sibling{Name: "serialize.c", Path: "mas-bandwidth/serialize.c", Ref: "def"}, got[1])
	assert.Empty(t, ParseSiblings("REPO: x/y\n"))
}

// At staging the member clones each SIBLINGS repository at its ref into
// $JOB/siblings/<name> from the bench mirror where one exists, and JOB.md names each
// as SIBLING_<NAME>=$JOB/siblings/<name> (docs/SPEC-STAGING.md, the siblings).
func TestStageSiblingsClonesFromTheMirrorIntoTheJobsSiblings(t *testing.T) {
	dir := t.TempDir()
	job := filepath.Join(dir, "jobs", "j1")
	mirror := filepath.Join(dir, "mirror")
	require.NoError(t, os.MkdirAll(mirror, 0o755))
	mkMirror(t, mirror, "serialize", "abc")

	lines, err := StageSiblings(job, mirror, []Sibling{{Name: "serialize", Path: "mas-bandwidth/serialize", Ref: "abc"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"SIBLING_SERIALIZE=$JOB/siblings/serialize"}, lines)
	clone := filepath.Join(job, "siblings", "serialize")
	_, err = os.Stat(filepath.Join(clone, ".git"))
	require.NoError(t, err, "the sibling is cloned into $JOB/siblings/serialize")
	out, err := exec.Command("git", "-C", clone, "rev-parse", "--verify", "abc").Output()
	require.NoError(t, err, "the clone carries the staged ref")
	assert.NotEmpty(t, strings.TrimSpace(string(out)))
	_, err = os.Stat(filepath.Join(clone, "README.md"))
	require.NoError(t, err, "the sibling's ref is materialized in the worktree the harness reads")
}

// A sibling that cannot be staged refuses the stage with its name and the card returns
// to ready (a bench fault, not the worker's).
func TestStageSiblingsRefusesWithTheNameWhenItCannotStage(t *testing.T) {
	dir := t.TempDir()
	job := filepath.Join(dir, "job")
	mirror := filepath.Join(dir, "mirror")
	require.NoError(t, os.MkdirAll(mirror, 0o755))
	mkMirror(t, mirror, "serialize", "abc")

	orig := cloneRepo
	t.Cleanup(func() { cloneRepo = orig })
	cloneRepo = func(dst, src, ref string) error {
		return os.ErrNotExist
	}
	_, err := StageSiblings(job, mirror, []Sibling{{Name: "serialize", Path: "mas-bandwidth/serialize", Ref: "abc"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "serialize", "the refusal names the sibling")
	assert.Contains(t, err.Error(), "cannot stage")
}

func TestStageSiblingsNamingIsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	job := filepath.Join(dir, "job")
	mirror := filepath.Join(dir, "mirror")
	require.NoError(t, os.MkdirAll(mirror, 0o755))
	mkMirror(t, mirror, "serialize.c", "def")
	lines, err := StageSiblings(job, mirror, []Sibling{{Name: "serialize.c", Path: "mas-bandwidth/serialize.c", Ref: "def"}})
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.True(t, strings.HasPrefix(lines[0], "SIBLING_SERIALIZE.C="), lines[0])
	assert.True(t, strings.HasSuffix(lines[0], "=$JOB/siblings/serialize.c"), lines[0])
}
