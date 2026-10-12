package swarm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStageCardWritesConfiguredIdentityIntoCloneImmediately(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	config := filepath.Join(root, "xdg")
	require.NoError(t, os.MkdirAll(filepath.Join(config, "git"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(config, "git", "config"), []byte("[user]\n\tname = Host\n\temail = glenn@networknext.com\n"), 0o600))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("NOVA_SPRINT_GIT_NAME", "Machine")
	t.Setenv("NOVA_SPRINT_GIT_EMAIL", "machine@example.invalid")

	source := filepath.Join(root, "source")
	require.NoError(t, os.MkdirAll(source, 0o700))
	gitTest(t, source, "init", "-q")
	gitTest(t, source, "config", "user.name", "fixture")
	gitTest(t, source, "config", "user.email", "fixture@example.invalid")
	require.NoError(t, os.WriteFile(filepath.Join(source, "README"), []byte("base\n"), 0o600))
	gitTest(t, source, "add", "README")
	gitTest(t, source, "commit", "-q", "-m", "base")
	sha := gitTest(t, source, "rev-parse", "HEAD")
	target := filepath.Join(root, "job", "repo")
	_, err := StageCard(StageOptions{Base: &CardBase{Repo: source, Sha: sha}, TargetDir: target, BenchHome: root, Branch: "sprint/test"})
	require.NoError(t, err)
	require.Equal(t, "Machine", gitTest(t, target, "config", "--local", "user.name"))
	require.Equal(t, "machine@example.invalid", gitTest(t, target, "config", "--local", "user.email"))
}

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}
