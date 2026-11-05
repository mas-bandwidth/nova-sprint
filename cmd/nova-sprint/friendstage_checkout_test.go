package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A card's BASE can exist only on the remote (sprint/schema-100). A fresh clone has
// only origin/<BASE>, so the stage's checkout of the bare name exits 128 unless the
// name resolves to origin/<name>. A full sha and a local branch still check out.
func TestStageCheckoutResolvesARemoteOnlyBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main", "-c", "protocol.file.allow=always"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	seed := filepath.Join(root, "seed")
	bare := filepath.Join(root, "remote.git")
	git(root, "init", "-q", seed)
	git(seed, "commit", "-q", "--allow-empty", "-m", "one")
	git(seed, "checkout", "-q", "-b", "sprint/schema-100")
	git(seed, "commit", "-q", "--allow-empty", "-m", "two")
	want := git(seed, "rev-parse", "HEAD")
	git(root, "clone", "-q", "--bare", seed, bare)
	clone := filepath.Join(root, "clone")
	git(root, "clone", "-q", bare, clone)
	git(clone, "checkout", "-q", "main")
	git(clone, "branch", "-D", "sprint/schema-100")

	run := func(ctx context.Context, dir string, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		return string(out), err
	}
	_, err := run(ctx, clone, "rev-parse", "--verify", "-q", "sprint/schema-100")
	require.Error(t, err, "the clone has no local branch of the remote-only base")

	require.NoError(t, stageCheckout(ctx, run, clone, "sprint/s1-1.w1.g1.e0", "sprint/schema-100"))
	require.Equal(t, want, git(clone, "rev-parse", "HEAD"))

	require.NoError(t, stageCheckout(ctx, run, clone, "sprint/s1-2.w1.g1.e0", want))
	require.Equal(t, want, git(clone, "rev-parse", "HEAD"))

	require.NoError(t, stageCheckout(ctx, run, clone, "sprint/s1-3.w1.g1.e0", "main"))
	require.Equal(t, git(clone, "rev-parse", "main"), git(clone, "rev-parse", "HEAD"))
}
