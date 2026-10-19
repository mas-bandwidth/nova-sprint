//go:build functional

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/gitrun"
	"github.com/stretchr/testify/require"
)

// TestLinearMergeHeadReplaysAForwardMerge exercises the production adapter on
// a real repository. The configured linear base receives one commit per source
// card, no merge commit, the reviewed forward head's exact tree, and a second
// invocation resumes without duplicating a card.
func TestLandLinearBaseFunctional(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	env := []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=linear-test", "GIT_AUTHOR_EMAIL=linear@example.invalid",
		"GIT_COMMITTER_NAME=linear-test", "GIT_COMMITTER_EMAIL=linear@example.invalid",
	}
	git := func(args ...string) string {
		t.Helper()
		res, err := gitrun.Run(ctx, gitrun.Options{C: dir, Env: env, OwnRepo: true}, args...)
		require.NoError(t, err, "git %v: %s", args, res.Stderr)
		return strings.TrimSpace(string(res.Stdout))
	}
	git("init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "root"), []byte("root\n"), 0o600))
	git("add", "root")
	git("commit", "-q", "-m", "base")
	base := git("rev-parse", "HEAD")
	git("switch", "-q", "-c", "source")
	for _, card := range []string{"alpha.w1", "beta.w1", "gamma.w1"} {
		sourceTip := git("rev-parse", "HEAD")
		git("switch", "-q", "-c", "topic-"+card)
		require.NoError(t, os.WriteFile(filepath.Join(dir, card), []byte(card+"\n"), 0o600))
		git("add", card)
		git("commit", "-q", "-m", "work "+card)
		git("switch", "-q", "source")
		git("merge", "-q", "--no-ff", "-m", "land "+card+" (sprint stream source)", "topic-"+card)
		require.NotEqual(t, sourceTip, git("rev-parse", "HEAD"))
	}
	git("switch", "-q", "-c", "forward", base)
	git("merge", "-q", "--no-ff", "-m", "forward merge", "source")
	forward := git("rev-parse", "HEAD")
	wantTree := git("rev-parse", "HEAD^{tree}")
	git("switch", "-q", "-c", "linear", base)

	a := newApp(os.Getenv)
	defer a.close()
	l := &lander{a: a, identity: env[1:]}
	c := landCard{id: "forward.w1", head: forward, linear: true, brief: "# Forward branch"}
	card, infra, _ := l.linearMergeHead(ctx, dir, "linear", c)
	require.Empty(t, card)
	require.Empty(t, infra)
	require.Equal(t, wantTree, git("rev-parse", "HEAD^{tree}"))
	require.Empty(t, git("rev-list", "--min-parents=2", base+"..HEAD"))
	require.Equal(t, "3", git("rev-list", "--count", base+"..HEAD"))
	log := git("log", "--format=%B", base+"..HEAD")
	require.Contains(t, log, "Nova-Card: alpha.w1")
	require.Contains(t, log, "Nova-Card: beta.w1")
	require.Contains(t, log, "Nova-Card: gamma.w1")

	tip := git("rev-parse", "HEAD")
	card, infra, _ = l.linearMergeHead(ctx, dir, "linear", c)
	require.Empty(t, card)
	require.Empty(t, infra)
	require.Equal(t, tip, git("rev-parse", "HEAD"), "resume must not duplicate a replayed card")
}
