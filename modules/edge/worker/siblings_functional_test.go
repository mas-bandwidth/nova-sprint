package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFunctionalSiblingsStagedReadOnlyBesideTheWorktree (functional tier:
// real git on local origins, file:// URLs, and the bench's nova-sandbox):
// a repo whose base names a sibling at a tag behind its tip is staged with
// the sibling at the pinned sha, read-only, linked as ../<name> of the
// worktree and granted to the wall --read only; inside the wall a read
// through the link works, and a write into the tree is refused by the wall
// itself (with the tree's own write bit put back for the try).
func TestFunctionalSiblingsStagedReadOnlyBesideTheWorktree(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir()) // the Mac wall follows no link outside its grants
	require.NoError(t, err)
	t.Cleanup(func() { writable_internal(t, root) })
	commit := func(dir, file, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644))
		runGitCmd_internal(t, dir, "add", file)
		runGitCmd_internal(t, dir, "commit", "-q", "-m", file)
	}

	sibOrigin, sibSeed := filepath.Join(root, "sib.git"), filepath.Join(root, "sibseed")
	runGitCmd_internal(t, root, "init", "-q", "--bare", sibOrigin)
	runGitCmd_internal(t, root, "init", "-q", "-b", "main", sibSeed)
	commit(sibSeed, "lib.txt", "from the sibling\n")
	runGitCmd_internal(t, sibSeed, "tag", "-a", "v1.0.0", "-m", "v1.0.0")
	pin := runGitCmd_internal(t, sibSeed, "rev-parse", "HEAD")
	commit(sibSeed, "lib.txt", "past the pin\n")
	runGitCmd_internal(t, sibSeed, "push", "-q", "file://"+sibOrigin, "main", "refs/tags/v1.0.0")

	origin, seed := filepath.Join(root, "origin.git"), filepath.Join(root, "seed")
	runGitCmd_internal(t, root, "init", "-q", "--bare", origin)
	runGitCmd_internal(t, root, "init", "-q", "-b", "main", seed)
	commit(seed, SiblingsName, "# built against\n../sib file://"+sibOrigin+" v1.0.0 "+pin+"\n")
	base := runGitCmd_internal(t, seed, "rev-parse", "HEAD")
	runGitCmd_internal(t, seed, "push", "-q", "file://"+origin, "main")

	bench := filepath.Join(root, "bench")
	g := &OSGitOps{WorkRoot: filepath.Join(bench, "clones"), MirrorRoot: filepath.Join(bench, "nova-bench", "mirror"), SiblingsRoot: filepath.Join(bench, "nova-bench", "siblings")}
	jobDir := filepath.Join(bench, "jobs", "c1")
	j := Job{Repo: "file://" + origin, Kind: "work", Base: "main", Branch: "sprint/s/c1", BaseSHA: base, Consumer: "c1",
		JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail}
	ctx := context.Background()
	require.NoError(t, g.Fetch(ctx, j))
	_, err = g.StageWorktree(ctx, j)
	require.NoError(t, err)

	tree := filepath.Join(g.SiblingsRoot, "sib", pin)
	link, err := os.Readlink(filepath.Join(jobDir, "sib"))
	require.NoError(t, err)
	require.Equal(t, tree, link)
	body, err := os.ReadFile(filepath.Join(j.WorktreeDir, "..", "sib", "lib.txt"))
	require.NoError(t, err)
	require.Equal(t, "from the sibling\n", string(body), "the sibling is at its pin, not its tip")
	require.Equal(t, pin, runGitCmd_internal(t, tree, "rev-parse", "HEAD"))
	require.NoFileExists(t, filepath.Join(tree, ".git", "objects", "info", "alternates"), "the tree borrows no objects")
	for _, p := range []string{tree, filepath.Join(tree, "lib.txt")} {
		st, err := os.Stat(p)
		require.NoError(t, err)
		require.Zero(t, st.Mode().Perm()&0o222, "%s is writable", p)
	}
	cfg := (&OSReserveWall{}).effectiveConfig_internal(j)
	require.Contains(t, cfg.ReadPaths, tree)
	flags := sandboxFlags_internal(cfg, j, nil)
	require.Equal(t, []string{jobDir}, flagValues_internal(flags, "--write"), "the tree is never a --write")

	sandbox := resolveToolBin_internal("nova-sandbox")
	if !filepath.IsAbs(sandbox) {
		if os.Getenv("CI") != "" {
			t.Fatal("this functional test needs nova-sandbox on PATH or in ~/.local/bin")
		}
		t.Skip("functional: nova-sandbox is not installed here")
	}
	wall := func(argv ...string) (string, error) {
		cmd := exec.Command(sandbox, append([]string{"--net-deny", "--write", jobDir, "--read", tree, "--"}, argv...)...)
		cmd.Dir = jobDir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := wall("/bin/cat", filepath.Join(jobDir, "sib", "lib.txt"))
	require.NoError(t, err, "%s", out)
	require.Contains(t, out, "from the sibling")

	require.NoError(t, os.Chmod(tree, 0o755))
	out, err = wall("/usr/bin/touch", filepath.Join(jobDir, "sib", "written"))
	require.NoError(t, os.Chmod(tree, 0o555))
	require.Error(t, err, "a write into the tree from inside the wall: %s", out)
	require.NoFileExists(t, filepath.Join(tree, "written"))
}

// flagValues_internal are the values flags gives flag, in order.
func flagValues_internal(flags []string, flag string) []string {
	var out []string
	for i, f := range flags {
		if f == flag && i+1 < len(flags) {
			out = append(out, flags[i+1])
		}
	}
	return slices.DeleteFunc(out, func(v string) bool { return strings.TrimSpace(v) == "" })
}
