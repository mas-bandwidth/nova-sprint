package swarm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardcontract"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
)

// stageTestGit runs git in dir with a clean, fixed environment (no host global config),
// so the test's setup commits never depend on the machine's own identity.
func stageTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=setup", "GIT_AUTHOR_EMAIL=setup@example.invalid",
		"GIT_COMMITTER_NAME=setup", "GIT_COMMITTER_EMAIL=setup@example.invalid")
	res, err := gitrun.Run(context.Background(), gitrun.Options{C: dir, Env: env, OwnRepo: dir != ""}, args...)
	require.NoError(t, err, "git %v: %s", args, res.Stderr)
	return strings.TrimSpace(string(res.Stdout))
}

// TestReworkAtTipCarriesTheConfiguredIdentity proves the rework-at-tip commit
// (restageAtTip), the machine's own commit, carries the configured identity even when the
// host's global git config names the owner (docs/SPEC-SPRINT.md, the machine's commit
// identity). The staging git runs with a planted global config naming the owner; the commit
// must still be the configured identity.
func TestReworkAtTipCarriesTheConfiguredIdentity(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	require.NoError(t, os.Mkdir(home, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
		[]byte("[user]\n\tname = Owner\n\temail = owner@example.com\n"), 0o600))
	// The staging git (stageGit) reads the process environment: plant a HOME naming the
	// owner and a different author, so the configured identity is the only thing that can
	// make the commit the machine's.
	t.Setenv("HOME", home)
	t.Setenv("GIT_AUTHOR_NAME", "owner")
	t.Setenv("GIT_AUTHOR_EMAIL", "owner@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "owner")
	t.Setenv("GIT_COMMITTER_EMAIL", "owner@example.com")

	bare := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	require.NoError(t, os.Mkdir(work, 0o755))
	stageTestGit(t, "", "init", "-q", "--bare", "-b", "main", bare)
	stageTestGit(t, work, "init", "-q", "-b", "main")
	stageTestGit(t, work, "remote", "add", "origin", bare)
	require.NoError(t, os.WriteFile(filepath.Join(work, "base"), []byte("base\n"), 0o600))
	stageTestGit(t, work, "add", "base")
	stageTestGit(t, work, "commit", "-q", "-m", "base")
	stageTestGit(t, work, "push", "-q", "origin", "HEAD:refs/heads/main")

	// The previous attempt's head, on its own branch, pushed so a rework can carry it.
	stageTestGit(t, work, "switch", "-q", "-c", "feat")
	require.NoError(t, os.WriteFile(filepath.Join(work, "carried"), []byte("carried\n"), 0o600))
	stageTestGit(t, work, "add", "carried")
	stageTestGit(t, work, "commit", "-q", "-m", "the work of attempt 1")
	prev := stageTestGit(t, work, "rev-parse", "HEAD")
	stageTestGit(t, work, "push", "-q", "origin", "HEAD:refs/heads/feat")

	// The base branch moves on: the rework is staged at its tip.
	stageTestGit(t, work, "switch", "-q", "main")
	require.NoError(t, os.WriteFile(filepath.Join(work, "tip"), []byte("tip\n"), 0o600))
	stageTestGit(t, work, "add", "tip")
	stageTestGit(t, work, "commit", "-q", "-m", "the base moved")
	stageTestGit(t, work, "push", "-q", "origin", "HEAD:refs/heads/base")

	// A clone, as a worker's is: no local identity set, so only the identity passed to the
	// rework-at-tip commit can name it.
	clone := filepath.Join(root, "clone")
	stageTestGit(t, root, "clone", "-q", bare, clone)
	identity := config.GitIdentity{Name: "nova-sprint", Email: "nova-sprint@mas-bandwidth.com"}
	var fetchTime, checkoutTime time.Duration
	carry, what, out, err := restageAtTip(context.Background(), stageGit, clone, "sprint/card", "base",
		Rework{Prev: prev, From: 1}, identity.Env(), &fetchTime, &checkoutTime)
	require.NoError(t, err, "%s: %s", what, out)
	require.NotNil(t, carry)
	require.Equal(t, cardcontract.CarryOK, carry.State)

	got := stageTestGit(t, clone, "log", "-1", "--format=%an <%ae>|%cn <%ce>")
	require.Equal(t, "nova-sprint <nova-sprint@mas-bandwidth.com>|nova-sprint <nova-sprint@mas-bandwidth.com>", got)
}

// TestStageJobCloneCarriesTheConfiguredIdentity proves the live launcher entry point
// (StageJobAs) writes the sprint row's git_identity into the worker's clone's local git
// config at clone time — not the pool's identity and not the host's global config — so a
// commit the worker makes in the clone carries the configured identity
// (docs/SPEC-SPRINT.md, the machine's commit identity). StageCloneIdentityAs is the step
// that writes the local config; StageJobAs is the launcher's staging that calls it.
func TestStageJobCloneCarriesTheConfiguredIdentity(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	require.NoError(t, os.Mkdir(home, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
		[]byte("[user]\n\tname = Owner\n\temail = owner@example.com\n"), 0o600))
	t.Setenv("HOME", home)
	t.Setenv("GIT_AUTHOR_NAME", "owner")
	t.Setenv("GIT_AUTHOR_EMAIL", "owner@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "owner")
	t.Setenv("GIT_COMMITTER_EMAIL", "owner@example.com")

	pool := filepath.Join(root, "pool")
	job := filepath.Join(root, "job")
	require.NoError(t, os.MkdirAll(pool, 0o755))
	require.NoError(t, os.MkdirAll(job, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pool, "identity.tsv"),
		[]byte("owner\tname\temail\nowner\tbob\tbob@example.com\n"), 0o644))

	work := filepath.Join(root, "work")
	require.NoError(t, os.Mkdir(work, 0o755))
	stageTestGit(t, work, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(work, "README"), []byte("base\n"), 0o600))
	stageTestGit(t, work, "add", "README")
	stageTestGit(t, work, "commit", "-q", "-m", "base")

	require.NoError(t, StageJobAs(pool, job, work, config.DefaultGitIdentity))

	require.Equal(t, "nova-sprint", stageTestGit(t, work, "config", "user.name"))
	require.Equal(t, "nova-sprint@mas-bandwidth.com", stageTestGit(t, work, "config", "user.email"))

	require.NoError(t, StageCloneIdentityAs(work, StagingIdentity{Owner: "owner", Name: "bob", Email: "bob@example.com"}, config.DefaultGitIdentity))
	require.Equal(t, "nova-sprint", stageTestGit(t, work, "config", "user.name"))
	require.Equal(t, "nova-sprint@mas-bandwidth.com", stageTestGit(t, work, "config", "user.email"))

	require.NoError(t, StageJob(pool, job, work))
	require.Equal(t, "bob", stageTestGit(t, work, "config", "user.name"))
}
