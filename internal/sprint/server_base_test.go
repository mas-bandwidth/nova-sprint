package sprint

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerSwitchRefusesABinaryBuiltOffTheSprintBase tests that ServerSwitch:
// 1. Refuses a binary whose build commit is not an ancestor of origin's sprint base
// 2. Accepts a binary whose build commit is an ancestor of origin's sprint base
func TestServerSwitchRefusesABinaryBuiltOffTheSprintBase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("binary built from side branch is refused", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		// Create a git repository with two divergent branches
		// main = sprint base, side = not an ancestor of main
		cloneDir := filepath.Join(dir, "clone")
		require.NoError(t, os.MkdirAll(cloneDir, 0o755))
		
		cmd := exec.Command("git", "init")
		cmd.Dir = cloneDir
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "config", "user.email", "test@test.com")
		cmd.Dir = cloneDir
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "config", "user.name", "Test")
		cmd.Dir = cloneDir
		require.NoError(t, cmd.Run())

		// Create commit on main (sprint base)
		require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "base.txt"), []byte("base"), 0o644))
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = cloneDir
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "commit", "-m", "base")
		cmd.Dir = cloneDir
		require.NoError(t, cmd.Run())

		// Get main commit hash
		cmd = exec.Command("git", "rev-parse", "main")
		cmd.Dir = cloneDir
		mainCommit, err := cmd.Output()
		require.NoError(t, err)
		mainCommit = bytes.TrimSpace(mainCommit)

		// Create side branch with a different commit (divergent from main)
		cmd = exec.Command("git", "checkout", "-b", "side")
		cmd.Dir = cloneDir
		require.NoError(t, cmd.Run())

		require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "side.txt"), []byte("side"), 0o644))
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = cloneDir
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "commit", "-m", "side")
		cmd.Dir = cloneDir
		require.NoError(t, cmd.Run())

		// Get side commit hash
		cmd = exec.Command("git", "rev-parse", "side")
		cmd.Dir = cloneDir
		sideCommit, err := cmd.Output()
		require.NoError(t, err)
		sideCommit = bytes.TrimSpace(sideCommit)

		// Create a candidate binary that claims to be built from the side branch
		// The version line includes proper Source info (repo, revision, dirty, build_host)
		candidate := filepath.Join(dir, "candidate")
		versionLine := "nova-sprint v1.0.0 darwin/amd64 go1.21 repo=test revision=" + string(sideCommit[:12]) + " dirty=false build_host=build-server\n"
		require.NoError(t, os.WriteFile(candidate, []byte(versionLine), 0o755))

		// Create target binary
		target := filepath.Join(dir, "target")
		require.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755))

		// ServerSwitch should refuse this binary because side branch is not ancestor of main
		err = ServerSwitch(ctx, ServerSwitchOptions{
			Binary:   candidate,
			Target:   target,
			RepoDir:  cloneDir,
			BaseRef:  "main",
			Now:      func() time.Time { return time.Now() },
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server switch REFUSED")
		assert.Contains(t, err.Error(), "not an ancestor")

		// Verify old binary is still in place
		content, _ := os.ReadFile(target)
		assert.Equal(t, "old binary", string(content))
	})

	t.Run("binary built from sprint base is accepted", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		// Create a git repository with main branch
		baseDir := filepath.Join(dir, "base")
		require.NoError(t, os.MkdirAll(baseDir, 0o755))
		
		cmd := exec.Command("git", "init")
		cmd.Dir = baseDir
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "config", "user.email", "test@test.com")
		cmd.Dir = baseDir
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "config", "user.name", "Test")
		cmd.Dir = baseDir
		require.NoError(t, cmd.Run())

		require.NoError(t, os.WriteFile(filepath.Join(baseDir, "main.txt"), []byte("main"), 0o644))
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = baseDir
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "commit", "-m", "main")
		cmd.Dir = baseDir
		require.NoError(t, cmd.Run())

		// Get main commit hash
		cmd = exec.Command("git", "rev-parse", "main")
		cmd.Dir = baseDir
		mainCommit, err := cmd.Output()
		require.NoError(t, err)
		mainCommit = bytes.TrimSpace(mainCommit)

		// Create a candidate binary that claims to be built from main
		candidate := filepath.Join(dir, "candidate")
		versionLine := "nova-sprint v1.0.0 darwin/amd64 go1.21 repo=test revision=" + string(mainCommit[:12]) + " dirty=false build_host=build-server\n"
		require.NoError(t, os.WriteFile(candidate, []byte(versionLine), 0o755))

		// Create target binary
		target := filepath.Join(dir, "target")
		require.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755))

		// ServerSwitch should accept this binary
		err = ServerSwitch(ctx, ServerSwitchOptions{
			Binary:   candidate,
			Target:   target,
			RepoDir:  baseDir,
			BaseRef:  "main",
			Now:      func() time.Time { return time.Now() },
		})

		assert.NoError(t, err)

		// Verify candidate is now in place
		content, _ := os.ReadFile(target)
		assert.Equal(t, versionLine, string(content))
	})

	t.Run("binary with no source commit is refused", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		// Create a candidate binary without proper Source info
		candidate := filepath.Join(dir, "candidate")
		versionLine := "nova-sprint v1.0.0 darwin/amd64 go1.21 build=12345\n"
		require.NoError(t, os.WriteFile(candidate, []byte(versionLine), 0o755))

		// Create target binary
		target := filepath.Join(dir, "target")
		require.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755))

		// ServerSwitch should refuse this binary
		err := ServerSwitch(ctx, ServerSwitchOptions{
			Binary:   candidate,
			Target:   target,
			RepoDir:  dir,
			BaseRef:  "main",
			Now:      func() time.Time { return time.Now() },
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server switch REFUSED")
		assert.Contains(t, err.Error(), "no source commit")
	})
}
