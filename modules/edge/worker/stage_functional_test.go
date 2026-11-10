package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// shallowOrigin_internal is an origin whose card branch sprint/s1/p1 (one
// commit, the prior head) forked from main 40 commits below main's tip,
// below a depth-1 fetch, 90 commits above main's root: its main tip and the
// prior head.
func shallowOrigin_internal(t *testing.T) (origin, seed, base, prior string) {
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
	for i := range 90 {
		commit("main.txt", strconv.Itoa(i)+"\n")
	}
	runGitCmd_internal(t, seed, "checkout", "-q", "-b", "card")
	commit("card.txt", "card\n")
	prior = runGitCmd_internal(t, seed, "rev-parse", "HEAD")
	runGitCmd_internal(t, seed, "checkout", "-q", "main")
	for i := 90; i < 130; i++ {
		commit("main.txt", strconv.Itoa(i)+"\n")
	}
	base = runGitCmd_internal(t, seed, "rev-parse", "HEAD")
	runGitCmd_internal(t, seed, "remote", "add", "origin", origin)
	runGitCmd_internal(t, seed, "push", "-q", "origin", "main", "card:refs/heads/sprint/s1/p1")
	return origin, seed, base, prior
}

// TestFunctionalShallowStage (functional tier: real git on a local origin)
// stages a work job that continues its card branch, forked far below the
// stream's tip, on a fresh bench and on one whose mirror and work clone are
// deep, from before shallow staging. On the fresh bench the mirror and the
// work clone are shallow, holding a fraction of main's history, yet the
// prior head and the base share their fork point (deepened for), so the
// job's commit seals, collects, rebases onto main after main moves on, and
// applies back in the job's tree; the deep clones stay deep.
func TestFunctionalShallowStage(t *testing.T) {
	t.Parallel()
	for _, kase := range []string{"fresh", "deep"} {
		t.Run(kase, func(t *testing.T) {
			t.Parallel()
			origin, seed, base, prior := shallowOrigin_internal(t)
			roots := t.TempDir()
			g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones"), MirrorRoot: filepath.Join(roots, "mirror")}
			mirror, work := filepath.Join(g.MirrorRoot, "origin.git"), filepath.Join(g.WorkRoot, "origin")
			if kase == "deep" { // the layout before shallow staging
				runGitCmd_internal(t, roots, "clone", "-q", "--mirror", origin, mirror)
				runGitCmd_internal(t, roots, "clone", "-q", "--no-checkout", "--reference", mirror, origin, work)
			}
			ctx := context.Background()
			jobDir := filepath.Join(t.TempDir(), "p1.w2")
			j := Job{Repo: origin, Kind: "work", Base: "main", Branch: "sprint/s1/p1", BaseSHA: base, PriorHead: prior, Consumer: "p1.w2",
				JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail}
			require.NoError(t, g.Fetch(ctx, j))
			head, err := g.StageWorktree(ctx, j)
			require.NoError(t, err)
			require.Equal(t, prior, head, "the job continues from its prior head")
			tip, err := g.BranchTip(ctx, j)
			require.NoError(t, err)
			require.Equal(t, prior, tip)

			if kase == "fresh" {
				for _, gitDir := range []string{mirror, filepath.Join(work, ".git")} {
					require.FileExists(t, filepath.Join(gitDir, "shallow"), "staging fetched %s deep", gitDir)
				}
				n, err := strconv.Atoi(runGitCmd_internal(t, work, "rev-list", "--count", base))
				require.NoError(t, err)
				require.Less(t, n, 130, "the work clone holds all of main's history")
			} else {
				require.NoFileExists(t, filepath.Join(mirror, "shallow"))
				require.NoFileExists(t, filepath.Join(work, ".git", "shallow"))
			}
			wt := j.WorktreeDir
			require.Equal(t, runGitCmd_internal(t, seed, "merge-base", base, prior), runGitCmd_internal(t, wt, "merge-base", base, "HEAD"), "the job's tree finds the fork point")

			// The harness's commit, the seal step and the collect.
			require.NoError(t, os.WriteFile(filepath.Join(wt, "work.txt"), []byte("work\n"), 0o644))
			runGitCmd_internal(t, wt, "add", "work.txt")
			runGitCmd_internal(t, wt, "commit", "-q", "-m", "work")
			seal := sealArgv_internal(jobDir, prior, testCommitName, testCommitEmail, "seal\n")
			cmd := exec.Command(seal[0], seal[1:]...)
			cmd.Dir = wt
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
			clone := filepath.Join(t.TempDir(), "clone")
			collected, err := g.Collect(ctx, Sealed{JobDir: jobDir, Clone: clone, Remote: origin, Base: prior, Name: testCommitName, Email: testCommitEmail})
			require.NoError(t, err)

			// Main moves on; the rebase onto it needs the fork point.
			require.NoError(t, os.WriteFile(filepath.Join(seed, "late.txt"), []byte("late\n"), 0o644))
			runGitCmd_internal(t, seed, "add", "late.txt")
			runGitCmd_internal(t, seed, "commit", "-q", "-m", "late")
			runGitCmd_internal(t, seed, "push", "-q", "origin", "main")
			rebased, err := g.RebaseOntoStream(ctx, clone, origin, "main")
			require.NoError(t, err)
			require.NotEqual(t, collected, rebased)
			newMain := runGitCmd_internal(t, seed, "rev-parse", "main")
			runGitCmd_internal(t, clone, "merge-base", "--is-ancestor", newMain, rebased)
			require.Equal(t, "card.txt\nlate.txt\nmain.txt\nwork.txt", runGitCmd_internal(t, clone, "ls-tree", "--name-only", rebased))
			require.Equal(t, "2", runGitCmd_internal(t, clone, "rev-list", "--count", newMain+".."+rebased), "only the card's commits are replayed")

			// The done-when apply: the rebased head, back in the job's tree.
			bundle := filepath.Join(t.TempDir(), "rebased.bundle")
			require.NoError(t, g.Export(ctx, clone, base, bundle))
			apply := applyArgv_internal(bundle)
			cmd = exec.Command(apply[0], apply[1:]...)
			cmd.Dir = wt
			out, err = cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.Equal(t, rebased, runGitCmd_internal(t, wt, "rev-parse", "HEAD"))
		})
	}
}

// TestFunctionalStreamStepKeepsAResolvedConflict (functional tier: real git
// on a local origin): a card head that merged main and resolved a conflict
// is brought onto a main that moved on again; the step merges the stream
// in, so the resolution stands and the result descends from the head.
func TestFunctionalStreamStepKeepsAResolvedConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	origin, seed, clone := filepath.Join(root, "origin.git"), filepath.Join(root, "seed"), filepath.Join(root, "clone")
	runGitCmd_internal(t, root, "init", "-q", "--bare", origin)
	runGitCmd_internal(t, root, "init", "-q", "-b", "main", seed)
	commit := func(file, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(seed, file), []byte(body), 0o644))
		runGitCmd_internal(t, seed, "add", file)
		runGitCmd_internal(t, seed, "commit", "-q", "-m", file+" "+body)
	}
	commit("f.txt", "base\n")
	runGitCmd_internal(t, seed, "checkout", "-q", "-b", "card")
	commit("f.txt", "card\n")
	runGitCmd_internal(t, seed, "checkout", "-q", "main")
	commit("f.txt", "main\n")
	runGitCmd_internal(t, seed, "checkout", "-q", "card")
	cmd := exec.Command("git", "merge", "-q", "main") // conflicts on f.txt
	cmd.Dir = seed
	require.Error(t, cmd.Run())
	commit("f.txt", "resolved\n") // the merge commit, with the resolution
	head := runGitCmd_internal(t, seed, "rev-parse", "HEAD")
	runGitCmd_internal(t, seed, "checkout", "-q", "main")
	commit("late.txt", "late\n") // main moves on
	runGitCmd_internal(t, seed, "remote", "add", "origin", origin)
	runGitCmd_internal(t, seed, "push", "-q", "origin", "main", "card")
	runGitCmd_internal(t, root, "clone", "-q", "-b", "card", origin, clone)
	runGitCmd_internal(t, clone, "config", "user.name", testCommitName)
	runGitCmd_internal(t, clone, "config", "user.email", testCommitEmail)

	g := &OSGitOps{WorkRoot: filepath.Join(root, "clones")}
	got, err := g.RebaseOntoStream(context.Background(), clone, origin, "main")
	require.NoError(t, err, "a resolved conflict comes back")
	runGitCmd_internal(t, clone, "merge-base", "--is-ancestor", head, got)
	runGitCmd_internal(t, clone, "merge-base", "--is-ancestor", runGitCmd_internal(t, seed, "rev-parse", "main"), got)
	require.Equal(t, "resolved", runGitCmd_internal(t, clone, "show", got+":f.txt"))
}
