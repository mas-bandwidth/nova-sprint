package friend

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkSiblingMirror makes the bare mirror of a repository named name at $dir/<name>.git, the
// shape the bench mirror has (~/nova-bench/mirror/<name>.git), with a branch ref.
func mkSiblingMirror(t *testing.T, dir, name, ref string) {
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
	cmd := exec.Command("git", "clone", "--quiet", "--bare", work, filepath.Join(dir, name+".git"))
	require.NoError(t, cmd.Run())
}

// A brief with a SIBLINGS header stages each sibling into $JOB/siblings/<name> and writes a
// JOB.md naming it SIBLING_<NAME>=$JOB/siblings/<name>, so the harness is pointed there
// (docs/SPEC-CARD-CONTRACT.md, the SIBLINGS header; docs/SPEC-STAGING.md, the siblings).
func TestStageWritesSiblingLinesAndCloneIntoTheJob(t *testing.T) {
	dir := t.TempDir()
	mirror := filepath.Join(dir, "mirror")
	require.NoError(t, os.MkdirAll(mirror, 0o755))
	require.NoError(t, os.MkdirAll(JobDir(dir, "j1"), 0o755))
	mkSiblingMirror(t, mirror, "serialize", "abc")

	s := &Stager{Dir: dir, SiblingMirror: mirror}
	p := Packet{Card: "c1", Job: "j1", Repo: "mas-bandwidth/nova-sprint", Base: "main", Branch: "sprint/c1", Attempt: 1,
		Brief: "RESULT: c1 sha=000000000000 tier: pro\nSIBLINGS: mas-bandwidth/serialize@abc\n"}
	require.NoError(t, s.stageAndWrite(p, "1111111111111111111111111111111111111111"))

	job := JobDir(dir, "j1")
	body, err := os.ReadFile(filepath.Join(job, JobFile))
	require.NoError(t, err)
	assert.Contains(t, string(body), "SIBLING_SERIALIZE=$JOB/siblings/serialize",
		"JOB.md names the sibling so the harness is pointed at it")
	_, err = os.Stat(filepath.Join(job, "siblings", "serialize", "README.md"))
	require.NoError(t, err, "the sibling is cloned into $JOB/siblings/serialize")
}

// A brief with no SIBLINGS header stages no sibling and writes JOB.md with no SIBLING line.
func TestStageWithoutASiblingsHeaderWritesNoSiblingLine(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(JobDir(dir, "j1"), 0o755))
	s := &Stager{Dir: dir, SiblingMirror: filepath.Join(dir, "mirror")}
	p := Packet{Card: "c1", Job: "j1", Repo: "mas-bandwidth/nova-sprint", Base: "main", Branch: "sprint/c1", Attempt: 1,
		Brief: "RESULT: c1 sha=000000000000 tier: pro\n"}
	require.NoError(t, s.stageAndWrite(p, "1111111111111111111111111111111111111111"))

	body, err := os.ReadFile(filepath.Join(JobDir(dir, "j1"), JobFile))
	require.NoError(t, err)
	assert.NotContains(t, string(body), "SIBLING_")
}
