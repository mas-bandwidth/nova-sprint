package sprint_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// landerGit runs git as the lander's fixed wrapper does (cmd/nova-sprint/land.go,
// (*lander).git): its trimmed stdout is preserved even when the command exits
// nonzero, so a conflicting `git merge-tree --write-tree` still yields its tree
// OID and unmerged stage rows.
func landerGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=lander", "GIT_AUTHOR_EMAIL=lander@example.invalid", "GIT_COMMITTER_NAME=lander", "GIT_COMMITTER_EMAIL=lander@example.invalid")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// A forward-merge card (a MERGE-OF line) is exempt from E12 for every file the
// merge brings unchanged from the merged branch: the lander resolves the merged
// parent M and exempts each changed file whose blob at after equals its blob at M
// or in the merge-tree result T (docs/SPEC-SPRINT.md section 7, the lander's
// checks). Without the MERGE-OF line today's behaviour is kept: the same files are
// refused.
func TestAForwardMergeCardIsExemptForItsMergedFiles(t *testing.T) {
	t.Parallel()
	paths := []string{"internal/a/*.go"}

	// base on main; sprint/merged changes 20 files outside the card's PATHS; the
	// card's head merges sprint/merged into the base and hand-edits one file inside
	// PATHS. before is the base tip, after the merged head.
	setup := func(repick map[string]string) (*mergeRig, string, string, string) {
		r := &mergeRig{t: t, dir: t.TempDir()}
		r.must("init", "-q", "-b", "main")
		r.write("internal/a/a.go", "package a\n")
		r.write("README.md", "r\n")
		r.must("commit", "-q", "-m", "base")
		before := r.must("rev-parse", "HEAD")
		r.must("switch", "-q", "-c", "sprint/merged")
		for i := 1; i <= 20; i++ {
			r.write(fmt.Sprintf("pkg/x/i%02d.go", i), fmt.Sprintf("package x\n\n// merged %d\n", i))
		}
		r.must("commit", "-q", "-m", "merged work")
		mergedTip := r.must("rev-parse", "HEAD")
		r.must("update-ref", "refs/remotes/origin/sprint/merged", mergedTip)
		r.must("switch", "-q", "main")
		r.must("merge", "--no-ff", "--no-edit", "-m", "merge sprint/merged", "sprint/merged")
		r.write("internal/a/a.go", "package a\n\n// edited\n")
		for f, s := range repick {
			r.write(f, s)
		}
		r.must("commit", "-q", "-m", "hand edit")
		after := r.must("rev-parse", "HEAD")
		return r, before, after, mergedTip
	}

	t.Run("twenty merged files are exempt, the hand edit inside PATHS is not refused", func(t *testing.T) {
		t.Parallel()
		r, before, after, mergedTip := setup(nil)
		res, err := sprint.MergeOf(r.dir, r.git, before, after, "sprint/merged")
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, mergedTip, res.Parent, "M is the merged branch's tip")
		require.Len(t, res.Exempt, 20, "all twenty merged files arrive unchanged")
		for i := 1; i <= 20; i++ {
			assert.Contains(t, res.Exempt, fmt.Sprintf("pkg/x/i%02d.go", i))
		}
		require.Len(t, res.Rest, 1)
		assert.Equal(t, "internal/a/a.go", res.Rest[0], "the hand edit inside PATHS is not exempt")

		diff := r.must("diff", "-M", "--no-color", before, after)
		tracked := strings.Split(r.must("ls-tree", "-r", "--name-only", before), "\n")
		amended, refused := sprint.LandScopeMergeOf(paths, diff, tracked, res.Exempt)
		assert.Empty(t, refused, "with MERGE-OF the scope check refuses nothing")
		assert.Empty(t, amended)
		// today's behaviour kept: without the exemption the same diff is refused for all twenty
		_, refusedToday := sprint.LandScope(paths, diff, tracked)
		require.Len(t, refusedToday, 20)
		assert.ElementsMatch(t, res.Exempt, refusedToday)
	})

	t.Run("a hand edit after the merge is refused by name, and only it", func(t *testing.T) {
		t.Parallel()
		r, before, after, _ := setup(map[string]string{"pkg/x/i01.go": "package x\n\n// edited again\n"})
		res, err := sprint.MergeOf(r.dir, r.git, before, after, "sprint/merged")
		require.NoError(t, err)
		require.Len(t, res.Exempt, 19, "the file edited after the merge is not exempt")
		assert.NotContains(t, res.Exempt, "pkg/x/i01.go")
		require.Len(t, res.Rest, 2)
		assert.ElementsMatch(t, []string{"internal/a/a.go", "pkg/x/i01.go"}, res.Rest)

		diff := r.must("diff", "-M", "--no-color", before, after)
		tracked := strings.Split(r.must("ls-tree", "-r", "--name-only", before), "\n")
		_, refused := sprint.LandScopeMergeOf(paths, diff, tracked, res.Exempt)
		require.Len(t, refused, 1, "only the re-edited file is refused, by name")
		assert.Contains(t, refused[0], "pkg/x/i01.go")
	})

	t.Run("a file merged cleanly by merge-tree from both sides is exempt", func(t *testing.T) {
		t.Parallel()
		r := &mergeRig{t: t, dir: t.TempDir()}
		r.must("init", "-q", "-b", "main")
		r.write("internal/a/a.go", "package a\n")
		r.write("f.txt", "line1\nline2\nline3\n")
		r.must("commit", "-q", "-m", "base")
		r.must("switch", "-q", "-c", "sprint/merged")
		r.write("f.txt", "line1\nline2\nMERGED\n")
		r.must("commit", "-q", "-m", "merged line")
		mergedTip := r.must("rev-parse", "HEAD")
		r.must("update-ref", "refs/remotes/origin/sprint/merged", mergedTip)
		r.must("switch", "-q", "main")
		r.write("f.txt", "BASE\nline2\nline3\n")
		r.must("commit", "-q", "-m", "base line")
		before := r.must("rev-parse", "HEAD")
		r.must("merge", "--no-ff", "--no-edit", "-m", "merge sprint/merged", "sprint/merged")
		after := r.must("rev-parse", "HEAD")
		res, err := sprint.MergeOf(r.dir, r.git, before, after, "sprint/merged")
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Contains(t, res.Exempt, "f.txt", "a clean merge of both sides is exempt by T")
		assert.NotContains(t, res.Exempt, "internal/a/a.go")
	})

	t.Run("a conflicted merge still exempts its clean arrivals", func(t *testing.T) {
		t.Parallel()
		r := &mergeRig{t: t, dir: t.TempDir()}
		r.must("init", "-q", "-b", "main")
		r.write("internal/a/a.go", "package a\n")
		r.write("conflict.txt", "line1\nline2\nline3\n")
		r.must("commit", "-q", "-m", "base")
		r.must("switch", "-q", "-c", "sprint/merged")
		r.write("pkg/x/clean.go", "package x\n\n// merged\n")
		r.write("conflict.txt", "MERGED\nline2\nline3\n")
		r.must("commit", "-q", "-m", "merged work")
		mergedTip := r.must("rev-parse", "HEAD")
		r.must("update-ref", "refs/remotes/origin/sprint/merged", mergedTip)
		r.must("switch", "-q", "main")
		r.write("conflict.txt", "BASE\nline2\nline3\n")
		r.must("commit", "-q", "-m", "base line")
		before := r.must("rev-parse", "HEAD")
		_, merr := r.git("merge", "--no-ff", "--no-edit", "-m", "merge sprint/merged", "sprint/merged")
		require.Error(t, merr, "the merge conflicts on conflict.txt")
		r.write("conflict.txt", "RESOLVED\nline2\nline3\n")
		r.must("add", "conflict.txt")
		r.must("commit", "-q", "-m", "resolve conflict")
		after := r.must("rev-parse", "HEAD")

		res, err := sprint.MergeOf(r.dir, func(args ...string) (string, error) { return landerGit(r.dir, args...) }, before, after, "sprint/merged")
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Contains(t, res.Exempt, "pkg/x/clean.go", "the clean arrival is exempt despite the conflict")
		assert.NotContains(t, res.Exempt, "conflict.txt", "the conflicted file is never exempt by T")
		assert.Contains(t, res.Rest, "conflict.txt")

		diff := r.must("diff", "-M", "--no-color", before, after)
		tracked := strings.Split(r.must("ls-tree", "-r", "--name-only", before), "\n")
		_, refused := sprint.LandScopeMergeOf(paths, diff, tracked, res.Exempt)
		require.Contains(t, refused, "conflict.txt")
		assert.NotContains(t, refused, "pkg/x/clean.go")
	})

	t.Run("a merged branch already an ancestor of before exempts nothing and says why", func(t *testing.T) {
		t.Parallel()
		r := &mergeRig{t: t, dir: t.TempDir()}
		r.must("init", "-q", "-b", "main")
		r.write("internal/a/a.go", "package a\n")
		r.must("commit", "-q", "-m", "base")
		r.must("switch", "-q", "-c", "sprint/merged")
		r.write("pkg/x/clean.go", "package x\n\n// merged\n")
		r.must("commit", "-q", "-m", "merged work")
		mergedTip := r.must("rev-parse", "HEAD")
		r.must("update-ref", "refs/remotes/origin/sprint/merged", mergedTip)
		r.must("switch", "-q", "main")
		r.must("merge", "--no-ff", "--no-edit", "-m", "merge sprint/merged", "sprint/merged")
		before := r.must("rev-parse", "HEAD")
		r.write("internal/a/a.go", "package a\n\n// edited\n")
		r.must("commit", "-q", "-m", "hand edit")
		after := r.must("rev-parse", "HEAD")

		res, err := sprint.MergeOf(r.dir, r.git, before, after, "sprint/merged")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "merge-of unresolved")
		assert.Contains(t, err.Error(), "already an ancestor")
		require.Nil(t, res, "the merge brings nothing new, so nothing is exempt")
	})

	t.Run("a rename the merge brings unchanged is exempt", func(t *testing.T) {
		t.Parallel()
		r := &mergeRig{t: t, dir: t.TempDir()}
		r.must("init", "-q", "-b", "main")
		r.write("pkg/x/old.go", "package x\n\n// old\n")
		r.write("internal/a/a.go", "package a\n")
		r.must("commit", "-q", "-m", "base")
		before := r.must("rev-parse", "HEAD")
		r.must("switch", "-q", "-c", "sprint/merged")
		r.must("mv", "pkg/x/old.go", "pkg/x/new.go")
		r.must("commit", "-q", "-m", "rename")
		mergedTip := r.must("rev-parse", "HEAD")
		r.must("update-ref", "refs/remotes/origin/sprint/merged", mergedTip)
		r.must("switch", "-q", "main")
		r.must("merge", "--no-ff", "--no-edit", "-m", "merge sprint/merged", "sprint/merged")
		r.write("internal/a/a.go", "package a\n\n// edited\n")
		r.must("commit", "-q", "-m", "hand edit")
		after := r.must("rev-parse", "HEAD")
		res, err := sprint.MergeOf(r.dir, r.git, before, after, "sprint/merged")
		require.NoError(t, err)
		assert.Contains(t, res.Exempt, "pkg/x/new.go", "a fully-merged rename is exempt (rule 3)")
		assert.NotContains(t, res.Rest, "pkg/x/new.go")
		assert.NotContains(t, res.Exempt, "internal/a/a.go")
	})

	t.Run("an unresolvable branch exempts nothing and says why", func(t *testing.T) {
		t.Parallel()
		r, before, after, _ := setup(nil)
		_, err := sprint.MergeOf(r.dir, r.git, before, after, "sprint/nowhere")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "merge-of unresolved")
	})
}
