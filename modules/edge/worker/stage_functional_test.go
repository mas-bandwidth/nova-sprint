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
			seal := sealArgv_internal(jobDir, prior, testCommitName, testCommitEmail, "seal\n", []string{"work.txt"})
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
			runGitCmd_internal(t, clone, "merge-base", "--is-ancestor", collected, rebased)
			require.Equal(t, "3", runGitCmd_internal(t, clone, "rev-list", "--count", newMain+".."+rebased), "the card's commits and the merge of main")

			// The done-when apply: the rebased head, back in the job's tree.
			bundle := filepath.Join(t.TempDir(), "rebased.bundle")
			require.NoError(t, g.Export(ctx, clone, base, bundle))
			apply := applyArgv_internal(bundle)
			cmd = exec.Command(apply[0], apply[1:]...)
			cmd.Dir = wt
			out, err = cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.Equal(t, rebased, runGitCmd_internal(t, wt, "rev-parse", "HEAD"))

			// A stream gate red at the head is judged at the tip the merge
			// used, in a worktree of its own that leaves the job's tree at
			// the head, and is dropped after; a stale one is replaced.
			tipUsed, err := g.StreamTip(ctx, clone)
			require.NoError(t, err)
			require.Equal(t, newMain, tipUsed)
			baseDir := filepath.Join(jobDir, BaseTreeDir)
			require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "stale"), 0o755))
			for _, argv := range [][]string{baseArgv_internal(tipUsed, baseDir), dropBaseArgv_internal(baseDir)} {
				cmd = exec.Command(argv[0], argv[1:]...)
				cmd.Dir = wt
				out, err = cmd.CombinedOutput()
				require.NoError(t, err, "%s", out)
				if argv[3] == "base" {
					require.Equal(t, newMain, runGitCmd_internal(t, baseDir, "rev-parse", "HEAD"))
					require.Equal(t, "late.txt\nmain.txt", runGitCmd_internal(t, baseDir, "ls-files"), "the base's tree is the tip, without the card's work")
					require.NoFileExists(t, filepath.Join(baseDir, "stale"))
					require.NoError(t, os.WriteFile(filepath.Join(baseDir, "built.o"), []byte("x"), 0o644))
				}
			}
			require.NoDirExists(t, baseDir)
			require.Equal(t, rebased, runGitCmd_internal(t, wt, "rev-parse", "HEAD"))
			require.Empty(t, runGitCmd_internal(t, wt, "status", "--porcelain"))
			require.NotContains(t, runGitCmd_internal(t, wt, "worktree", "list"), baseDir)
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
	cmd := exec.Command("git", "merge", "main") // conflicts on f.txt
	cmd.Dir = seed
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Nova Test", "GIT_AUTHOR_EMAIL=test@nova.local", "GIT_COMMITTER_NAME=Nova Test", "GIT_COMMITTER_EMAIL=test@nova.local")
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), "CONFLICT", "the merge stops on the conflict, not on a missing identity")
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

// TestFunctionalSealCommitsOnlyPaths (functional tier: real git) is the
// seal step on a tree the harness left dirty inside and outside the card's
// PATHS: rel-rs-ffi-aliasing's head was a seal commit of 259 build files
// under wire-compat/target/, untracked and not ignored. The seal commits
// only what PATHS names (a glob, a declared new directory, a deletion) and
// prints what it left out; a tree dirty only outside PATHS commits nothing.
func TestFunctionalSealCommitsOnlyPaths(t *testing.T) {
	t.Parallel()
	for _, kase := range []string{"mixed", "outside-only"} {
		t.Run(kase, func(t *testing.T) {
			t.Parallel()
			wt, jobDir := t.TempDir(), t.TempDir()
			write := func(file, body string) {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(wt, file)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(wt, file), []byte(body), 0o644))
			}
			runGitCmd_internal(t, wt, "init", "-q", "-b", "main")
			write(".gitignore", "/target\n")
			write("README.md", "readme\n")
			write("wire-compat/src/lib.rs", "lib\n")
			write("wire-compat/src/old.rs", "old\n")
			runGitCmd_internal(t, wt, "add", "-A")
			runGitCmd_internal(t, wt, "commit", "-q", "-m", "base")
			base := runGitCmd_internal(t, wt, "rev-parse", "HEAD")

			write("wire-compat/target/debug/libwire.rlib", "build output\n")
			write("target/debug/ignored.o", "ignored\n")
			write("README.md", "readme, edited outside PATHS\n")
			if kase == "mixed" {
				write("wire-compat/src/lib.rs", "lib, fixed\n")
				runGitCmd_internal(t, wt, "add", "README.md") // staged outside PATHS: still left out
				write("wire-compat/tests/alias.rs", "test\n")
				write("crates/ffi/src/glue.rs", "glue\n") // under a directory a glob matches
				require.NoError(t, os.Remove(filepath.Join(wt, "wire-compat/src/old.rs")))
			}
			seal := sealArgv_internal(jobDir, base, testCommitName, testCommitEmail, "seal\n", []string{"wire-compat/src/*.rs", "wire-compat/tests/", "crates/*"})
			cmd := exec.Command(seal[0], seal[1:]...)
			cmd.Dir = wt
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
			if kase == "outside-only" {
				require.Equal(t, base, runGitCmd_internal(t, wt, "rev-parse", "HEAD"), "nothing inside PATHS: no commit")
				require.FileExists(t, filepath.Join(jobDir, SealNone))
				require.NoFileExists(t, filepath.Join(jobDir, SealBundle))
			} else {
				require.Equal(t, "A\tcrates/ffi/src/glue.rs\nM\twire-compat/src/lib.rs\nD\twire-compat/src/old.rs\nA\twire-compat/tests/alias.rs",
					runGitCmd_internal(t, wt, "diff", "--name-status", base, "HEAD"), "the seal commit holds PATHS only")
				require.FileExists(t, filepath.Join(jobDir, SealBundle))
			}
			require.Contains(t, string(out), "wire-compat/target/", "the seal names what it left out")
			require.Contains(t, string(out), "README.md")
		})
	}
}
