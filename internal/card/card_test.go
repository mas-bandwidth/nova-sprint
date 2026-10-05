package card

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/goenv"
)

// readCheckout reads the branch off a checkout's HEAD, and a detached HEAD names
// none, so generate refuses rather than writing BASE: HEAD into every card. The
// branch is read by git symbolic-ref, never by comparing a name to HEAD.
func TestReadCheckoutReadsTheBranchAndNoneAtADetachedHEAD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644))
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(goenv.Clean(os.Environ()), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	}
	git("init", "-q", "-b", "dev")
	git("remote", "add", "origin", "git@example.com:example/repo.git")
	git("add", ".")
	git("commit", "-q", "-m", "fixture")

	var h cardgen.Header
	require.NoError(t, readCheckout(dir, &h))
	assert.Equal(t, "dev", h.Base)
	assert.Equal(t, "example/repo", h.Repo)
	assert.Regexp(t, "^[0-9a-f]{40}$", h.Sha)

	git("checkout", "-q", "--detach")
	detached := cardgen.Header{}
	require.NoError(t, readCheckout(dir, &detached))
	assert.Empty(t, detached.Base, "a detached HEAD is no branch")
	assert.Equal(t, h.Sha, detached.Sha)
}

func TestRepoOfURL(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "mas-bandwidth/nova-tools", repoOfURL("git@example.com:mas-bandwidth/nova-tools.git"))
	assert.Equal(t, "mas-bandwidth/nova-tools", repoOfURL("https://example.com/mas-bandwidth/nova-tools"))
	assert.Equal(t, "", repoOfURL("nonsense"))
}
