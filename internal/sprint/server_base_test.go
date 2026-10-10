package sprint_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

func TestCheckServerBaseRefusesCommitOutsideOriginBase(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "config", "user.email", "test@example.invalid")
	git(t, repo, "config", "user.name", "test")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "base")
	base := output(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "-q", "-b", "side")
	off := output(t, repo, "rev-parse", "HEAD")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "side")
	off = output(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "-q", "main")
	got, err := sprint.CheckServerBase(context.Background(), "nova-sprint v1 linux/amd64 go1 commit="+off, repo, "main")
	require.NoError(t, err)
	require.False(t, got.On)
	require.Contains(t, got.Why, "not an ancestor")
	require.Equal(t, base, got.Tip)
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	require.NoError(t, cmd.Run())
}

func output(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	b, err := cmd.Output()
	require.NoError(t, err)
	return string(b[:len(b)-1])
}
