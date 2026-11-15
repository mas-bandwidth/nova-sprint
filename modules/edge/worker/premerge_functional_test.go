package worker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// retryOrigin_internal is an origin (bare, in a temp dir) whose card branch
// sprint/s1/p1 is an earlier attempt's head (the prior head) and whose main
// moved on after the card forked. both: main and the card changed f.txt, so
// a merge of main into the prior head conflicts; else main changed only
// main.txt. Its seed clone, the base the card forked from, and the prior
// head.
func retryOrigin_internal(t *testing.T, both bool) (origin, seed, base, prior string) {
	t.Helper()
	root := t.TempDir()
	origin, seed = filepath.Join(root, "origin.git"), filepath.Join(root, "seed")
	runGitCmd_internal(t, root, "init", "-q", "--bare", origin)
	runGitCmd_internal(t, root, "init", "-q", "-b", "main", seed)
	commit := func(file, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(seed, file), []byte(body), 0o644))
		runGitCmd_internal(t, seed, "add", file)
		runGitCmd_internal(t, seed, "commit", "-q", "-m", file+" "+body)
	}
	commit("f.txt", "base\n")
	commit("main.txt", "0\n")
	base = runGitCmd_internal(t, seed, "rev-parse", "HEAD")
	runGitCmd_internal(t, seed, "checkout", "-q", "-b", "card")
	commit("f.txt", "card\n")
	prior = runGitCmd_internal(t, seed, "rev-parse", "HEAD")
	runGitCmd_internal(t, seed, "checkout", "-q", "main")
	commit("main.txt", "1\n")
	if both {
		commit("f.txt", "main\n")
	}
	runGitCmd_internal(t, seed, "remote", "add", "origin", origin)
	runGitCmd_internal(t, seed, "push", "-q", "origin", "main", "card:refs/heads/sprint/s1/p1")
	return origin, seed, base, prior
}

// stageRetry_internal fetches and stages a work retry of origin's card,
// continuing from prior, on a fresh bench: the job, its OSGitOps and what
// staging made.
func stageRetry_internal(t *testing.T, origin, base, prior string) (Job, *OSGitOps, Staged) {
	t.Helper()
	roots := t.TempDir()
	g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones"), MirrorRoot: filepath.Join(roots, "mirror")}
	jobDir := filepath.Join(t.TempDir(), "p1.w3")
	j := Job{Repo: origin, Kind: "work", Base: "main", Branch: "sprint/s1/p1", BaseSHA: base, PriorHead: prior, Consumer: "p1.w3",
		JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail}
	ctx := context.Background()
	require.NoError(t, g.Fetch(ctx, j))
	staged, err := g.StageWorktree(ctx, j)
	require.NoError(t, err)
	return j, g, staged
}

// sealCollectRebase_internal is the worker's steps after the harness, on
// real git: the seal step in the job's tree (PATHS f.txt and work.txt),
// the collect into the worker's own clone, and the stream step's merge of
// main. The worker's clone and the rebased head, or the stream step's
// error.
func sealCollectRebase_internal(t *testing.T, g *OSGitOps, j Job, origin string, merged string) (string, string, error) {
	t.Helper()
	ctx := context.Background()
	seal := sealArgv_internal(j.JobDir, j.PriorHead, testCommitName, testCommitEmail, "seal\n", []string{"f.txt", "work.txt"})
	cmd := exec.Command(seal[0], seal[1:]...)
	cmd.Dir = j.WorktreeDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	clone := filepath.Join(t.TempDir(), "clone")
	_, err = g.Collect(ctx, Sealed{JobDir: j.JobDir, Clone: clone, Remote: origin, Base: j.PriorHead, Name: testCommitName, Email: testCommitEmail, Merged: merged})
	require.NoError(t, err)
	head, err := g.RebaseOntoStream(ctx, clone, origin, "main")
	return clone, head, err
}

// TestFunctionalRetryStagesWithTheBaseMerged (functional tier: real git on
// a local origin): a work retry starts on its prior head with main's tip
// merged in. Clean, the harness starts on the merge commit, which descends
// from both; the work it adds goes through the seal, the collect and the
// stream step, which finds main merged.
func TestFunctionalRetryStagesWithTheBaseMerged(t *testing.T) {
	t.Parallel()
	origin, seed, base, prior := retryOrigin_internal(t, false)
	j, g, staged := stageRetry_internal(t, origin, base, prior)
	tip := runGitCmd_internal(t, seed, "rev-parse", "main")
	require.NotNil(t, staged.Merge)
	require.Equal(t, PreMerge{Tip: tip, Head: staged.Head}, *staged.Merge)
	wt := j.WorktreeDir
	require.Equal(t, staged.Head, runGitCmd_internal(t, wt, "rev-parse", "HEAD"))
	require.Equal(t, prior+"\n"+tip, runGitCmd_internal(t, wt, "rev-parse", "HEAD^1", "HEAD^2"), "a merge commit of the prior head and main")
	require.Equal(t, "card\n", readFile_internal(t, filepath.Join(wt, "f.txt")))
	require.Equal(t, "1\n", readFile_internal(t, filepath.Join(wt, "main.txt")))
	require.Empty(t, runGitCmd_internal(t, wt, "status", "--porcelain"))
	require.Equal(t, "sprint/s1/p1", runGitCmd_internal(t, wt, "rev-parse", "--abbrev-ref", "HEAD"), "on the card's branch")
	require.Equal(t, testCommitEmail, runGitCmd_internal(t, wt, "log", "-1", "--format=%ae"), "the merge carries the sprint's identity")

	require.NoError(t, os.WriteFile(filepath.Join(wt, "work.txt"), []byte("work\n"), 0o644)) // the harness's work, left for the seal
	_, rebased, err := sealCollectRebase_internal(t, g, j, origin, staged.Merge.Tip)
	require.NoError(t, err)
	require.NotEmpty(t, rebased)
}

// TestFunctionalRetryStagesAConflictForTheHarness (functional tier: real
// git on a local origin) is sm-a1-c-unnamed-record-check's retry, which
// failed four times on mini-m5: main and the prior head changed the same
// lines. Staging commits the conflicted merge with its markers, so the
// harness starts on a head whose parents are the prior head and main's
// tip; its resolution, sealed and collected, meets no conflict in the
// stream step. Started on the bare prior head (the code before), the same
// resolution conflicts again.
func TestFunctionalRetryStagesAConflictForTheHarness(t *testing.T) {
	t.Parallel()
	origin, seed, base, prior := retryOrigin_internal(t, true)
	j, g, staged := stageRetry_internal(t, origin, base, prior)
	tip := runGitCmd_internal(t, seed, "rev-parse", "main")
	require.NotNil(t, staged.Merge)
	require.Equal(t, PreMerge{Tip: tip, Head: staged.Head, Conflicts: []string{"f.txt"}}, *staged.Merge)
	wt := j.WorktreeDir
	require.Equal(t, prior+"\n"+tip, runGitCmd_internal(t, wt, "rev-parse", "HEAD^1", "HEAD^2"), "a merge commit of the prior head and main")
	f := readFile_internal(t, filepath.Join(wt, "f.txt"))
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>", "card", "main"} {
		require.Contains(t, f, marker, "the conflict is left in the tree for the harness")
	}
	require.Empty(t, runGitCmd_internal(t, wt, "status", "--porcelain"), "committed: nothing for the seal's reset to drop")
	_, err := os.Stat(filepath.Join(wt, ".git", "MERGE_HEAD"))
	require.True(t, errors.Is(err, os.ErrNotExist), "no merge in progress")
	require.Contains(t, runGitCmd_internal(t, wt, "log", "-1", "--format=%B"), "f.txt", "the merge commit names the conflicted files")

	// The harness resolves the markers and leaves the resolution for the seal.
	require.NoError(t, os.WriteFile(filepath.Join(wt, "f.txt"), []byte("card and main\n"), 0o644))
	clone, rebased, err := sealCollectRebase_internal(t, g, j, origin, staged.Merge.Tip)
	require.NoError(t, err, "the resolution stands: main is already merged")
	left, err := g.ConflictMarkers(context.Background(), clone, rebased, staged.Merge.Conflicts)
	require.NoError(t, err)
	require.Empty(t, left, "resolved: no marker left")
	left, err = g.ConflictMarkers(context.Background(), clone, staged.Head, append(staged.Merge.Conflicts, "gone.txt"))
	require.NoError(t, err)
	require.Equal(t, []string{"f.txt"}, left, "staging's merge holds the markers; a path absent there holds none")

	// The same resolution on the bare prior head (the code before the fix)
	// meets the conflict again in the stream step.
	j2, g2, _ := stageRetry_internal(t, origin, base, prior)
	runGitCmd_internal(t, j2.WorktreeDir, "reset", "-q", "--hard", prior)
	require.NoError(t, os.WriteFile(filepath.Join(j2.WorktreeDir, "f.txt"), []byte("card and main\n"), 0o644))
	_, _, err = sealCollectRebase_internal(t, g2, j2, origin, "")
	var ce *ConflictError
	require.ErrorAs(t, err, &ce, "without the merge at staging the conflict comes back")
	require.Equal(t, []string{"f.txt"}, ce.Files)
}

// TestFunctionalRetryStagesNoMergeItDoesNotNeed (functional tier: real
// git): a prior head that already holds main's tip, a first attempt (no
// prior head) and a read are staged as before, with no merge.
func TestFunctionalRetryStagesNoMergeItDoesNotNeed(t *testing.T) {
	t.Parallel()
	origin, seed, base, prior := retryOrigin_internal(t, false)
	// The card merged main itself: its branch holds main's tip.
	runGitCmd_internal(t, seed, "checkout", "-q", "card")
	runGitCmd_internal(t, seed, "merge", "-q", "--no-edit", "main")
	merged := runGitCmd_internal(t, seed, "rev-parse", "HEAD")
	runGitCmd_internal(t, seed, "push", "-q", "origin", "card:refs/heads/sprint/s1/p1")
	_, _, staged := stageRetry_internal(t, origin, base, merged)
	require.Nil(t, staged.Merge, "main's tip is in the prior head already")
	require.Equal(t, merged, staged.Head)

	_, _, first := stageRetry_internal(t, origin, base, "")
	require.Nil(t, first.Merge, "a first attempt starts at its base")
	require.Equal(t, base, first.Head)

	roots := t.TempDir()
	g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones")}
	jobDir := filepath.Join(t.TempDir(), "p1.r1")
	read := Job{Repo: origin, Kind: "read", Base: "main", Branch: "sprint/s1/p1", BaseSHA: base, PriorHead: prior, Head: prior, Consumer: "p1.r1",
		JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail}
	require.NoError(t, g.Fetch(context.Background(), read))
	rs, err := g.StageWorktree(context.Background(), read)
	require.NoError(t, err)
	require.Nil(t, rs.Merge, "a read is detached at the head under review")
	require.Equal(t, prior, rs.Head)
}

// TestFunctionalRetryMergeRefusals (functional tier: real git): a merge
// staging cannot make, that is no conflict (a tip the clone does not
// hold), is staging's error, never a tree handed to the harness as merged;
// and the collect of a head whose merged base brought a gitlink is not
// refused for the base's gitlink, while one the job's own commit adds
// still is.
func TestFunctionalRetryMergeRefusals(t *testing.T) {
	t.Parallel()
	origin, seed, base, prior := retryOrigin_internal(t, false)
	j, g, _ := stageRetry_internal(t, origin, base, prior)
	runGitCmd_internal(t, j.WorktreeDir, "reset", "-q", "--hard", prior)
	_, err := g.preMerge_internal(context.Background(), g.to_internal(g.Timeouts.WithDefaults().Stage), j, strings.Repeat("ab", 20))
	require.ErrorContains(t, err, "stage: merge main at abab")
	require.Equal(t, prior, runGitCmd_internal(t, j.WorktreeDir, "rev-parse", "HEAD"))

	// main gains a gitlink (a submodule) after the card forked.
	runGitCmd_internal(t, seed, "checkout", "-q", "main")
	runGitCmd_internal(t, seed, "update-index", "--add", "--cacheinfo", "160000,"+base+",sub")
	runGitCmd_internal(t, seed, "commit", "-q", "-m", "a submodule")
	runGitCmd_internal(t, seed, "push", "-q", "origin", "main")
	j2, g2, staged := stageRetry_internal(t, origin, base, prior)
	require.NotNil(t, staged.Merge)
	require.NoError(t, os.WriteFile(filepath.Join(j2.WorktreeDir, "work.txt"), []byte("work\n"), 0o644))
	seal := sealArgv_internal(j2.JobDir, prior, testCommitName, testCommitEmail, "seal\n", []string{"work.txt"})
	cmd := exec.Command(seal[0], seal[1:]...)
	cmd.Dir = j2.WorktreeDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	ctx := context.Background()
	s := Sealed{JobDir: j2.JobDir, Clone: filepath.Join(t.TempDir(), "clone"), Remote: origin, Base: prior, Name: testCommitName, Email: testCommitEmail}
	_, err = g2.Collect(ctx, s)
	require.ErrorContains(t, err, "adds a gitlink", "without the merged tip the base's gitlink reads as the job's")
	s.Merged, s.Clone = staged.Merge.Tip, filepath.Join(t.TempDir(), "clone")
	_, err = g2.Collect(ctx, s)
	require.NoError(t, err, "the merged base's gitlink is the base's")

	// The job's own gitlink is still refused.
	runGitCmd_internal(t, j2.WorktreeDir, "update-index", "--add", "--cacheinfo", "160000,"+base+",mine")
	runGitCmd_internal(t, j2.WorktreeDir, "commit", "-q", "-m", "my submodule")
	seal = sealArgv_internal(j2.JobDir, prior, testCommitName, testCommitEmail, "seal\n", []string{"work.txt"})
	cmd = exec.Command(seal[0], seal[1:]...)
	cmd.Dir = j2.WorktreeDir
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	s.Clone = filepath.Join(t.TempDir(), "clone")
	_, err = g2.Collect(ctx, s)
	require.ErrorContains(t, err, "adds a gitlink (a submodule) at mine")
}

func readFile_internal(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// TestFunctionalRetryStagesAnUnmarkedConflict (functional tier: real git):
// a binary file both sides changed conflicts with no marker for the
// harness to find. Staging commits it as the prior head had it and names
// it apart (Unmarked), so the prompt tells the harness to compare it with
// main's.
func TestFunctionalRetryStagesAnUnmarkedConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	origin, seed := filepath.Join(root, "origin.git"), filepath.Join(root, "seed")
	runGitCmd_internal(t, root, "init", "-q", "--bare", origin)
	runGitCmd_internal(t, root, "init", "-q", "-b", "main", seed)
	commit := func(body string) {
		require.NoError(t, os.WriteFile(filepath.Join(seed, "bin.dat"), []byte(body), 0o644))
		runGitCmd_internal(t, seed, "add", "bin.dat")
		runGitCmd_internal(t, seed, "commit", "-q", "-m", "bin")
	}
	commit("base\x00\x01")
	base := runGitCmd_internal(t, seed, "rev-parse", "HEAD")
	runGitCmd_internal(t, seed, "checkout", "-q", "-b", "card")
	commit("card\x00\x02")
	prior := runGitCmd_internal(t, seed, "rev-parse", "HEAD")
	runGitCmd_internal(t, seed, "checkout", "-q", "main")
	commit("main\x00\x03")
	runGitCmd_internal(t, seed, "remote", "add", "origin", origin)
	runGitCmd_internal(t, seed, "push", "-q", "origin", "main", "card:refs/heads/sprint/s1/p1")

	j, _, staged := stageRetry_internal(t, origin, base, prior)
	require.NotNil(t, staged.Merge)
	require.Empty(t, staged.Merge.Conflicts)
	require.Equal(t, []string{"bin.dat"}, staged.Merge.Unmarked)
	require.Equal(t, "card\x00\x02", readFile_internal(t, filepath.Join(j.WorktreeDir, "bin.dat")), "as the prior head had it")
	require.Empty(t, runGitCmd_internal(t, j.WorktreeDir, "status", "--porcelain"))
	j.PreMerge = staged.Merge
	require.Contains(t, preMergeNote_internal(j), "bin.dat")
	require.Contains(t, preMergeNote_internal(j), "conflicted with no marker")
}
