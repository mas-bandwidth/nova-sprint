package sprint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerSwitchRefusesABinaryBuiltOffTheSprintBase verifies that:
// - a binary stamped from a side branch is refused
// - a binary stamped from the base is switched
// - a binary with no source commit is refused
// (docs/SPEC-SPRINT.md, server switch section).
func TestServerSwitchRefusesABinaryBuiltOffTheSprintBase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("binary with no source commit is refused", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// Create a fake binary with no build info (like a hand-compiled binary)
		binary := filepath.Join(dir, "nova-sprint-nostamp")
		require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\necho \"nova-sprint devel linux/amd64 go1.26\""), 0o755))

		target := filepath.Join(dir, "target")
		require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o755))

		err := ServerSwitch(ctx, ServerSwitchOptions{
			Binary: binary,
			Target: target,
		})
		require.Error(t, err, "server switch should refuse binary with no source commit")
		assert.Contains(t, err.Error(), "cannot read build source")
	})

	t.Run("binary from side branch is refused", func(t *testing.T) {
		t.Parallel()
		// Create a twin repository with main and a side branch
		repoDir := t.TempDir()
		setupTwinRepo(t, repoDir, "main", "side-branch")

		// Create a binary stamped from side-branch
		sideBranchBin := filepath.Join(repoDir, "nova-sprint-side")
		require.NoError(t, os.WriteFile(sideBranchBin, []byte("#!/bin/sh\necho \"nova-sprint 0.0.0 linux/amd64 go1.26 build=side-branch\""), 0o755))

		target := filepath.Join(repoDir, "target")
		require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o755))

		err := ServerSwitch(ctx, ServerSwitchOptions{
			Binary: sideBranchBin,
			Target: target,
		})
		require.Error(t, err, "server switch should refuse binary from side branch")
		assert.Contains(t, err.Error(), "not an ancestor of origin/main")
	})

	t.Run("binary from main is allowed", func(t *testing.T) {
		t.Parallel()
		// Create a twin repository with main
		repoDir := t.TempDir()
		setupTwinRepo(t, repoDir, "main", "")

		// Create a binary stamped from main
		mainBin := filepath.Join(repoDir, "nova-sprint-main")
		mainRev, err := gitRevParse(repoDir, "main")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(mainBin, []byte("#!/bin/sh\necho \"nova-sprint 0.0.0 linux/amd64 go1.26 build="+mainRev+"\""), 0o755))

		target := filepath.Join(repoDir, "target")
		require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o755))

		err = ServerSwitch(ctx, ServerSwitchOptions{
			Binary: mainBin,
			Target: target,
		})
		require.NoError(t, err, "server switch should allow binary from main")

		// Verify target was updated
		content, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "#!/bin/sh\necho \"nova-sprint 0.0.0 linux/amd64 go1.26 build="+mainRev+"\"", string(content))
	})
}

// setupTwinRepo creates a minimal git repository with main branch.
func setupTwinRepo(t *testing.T, dir, mainBranch string, sideBranch string) {
	t.Helper()

	// Initialize repo
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create initial commit on main
	testFile := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("initial"), 0o644))
	runGit(t, dir, "add", "test.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial commit")

	// Create side branch if requested
	if sideBranch != "" {
		runGit(t, dir, "checkout", "-q", "-b", sideBranch)
		sideFile := filepath.Join(dir, "side.txt")
		require.NoError(t, os.WriteFile(sideFile, []byte("side"), 0o644))
		runGit(t, dir, "add", "side.txt")
		runGit(t, dir, "commit", "-q", "-m", "side commit")
		runGit(t, dir, "checkout", "-q", mainBranch)
	}

	// Set up a bare origin to fetch from
	originDir := t.TempDir()
	runGit(t, originDir, "init", "-q", "--bare")
	runGit(t, dir, "remote", "add", "origin", originDir)
	runGit(t, dir, "push", "-q", "origin", mainBranch)
	if sideBranch != "" {
		runGit(t, dir, "push", "-q", "origin", sideBranch)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = nil
	cmd.Stderr = nil
	require.NoError(t, cmd.Run())
}

func gitRevParse(dir string, rev string) (string, error) {
	cmd := exec.Command("git", "rev-parse", rev)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
