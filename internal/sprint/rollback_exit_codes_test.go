package sprint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckRollbackOnLandFailureExitCodes verifies that rollback only happens
// on exit 2 (pass fault), not exit 1 (card's own refusal).
func TestCheckRollbackOnLandFailureExitCodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("exit 1 (card refusal) does not trigger rollback", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		target := filepath.Join(dir, "nova-sprint")
		candidate := filepath.Join(dir, "nova-sprint-candidate")

		require.NoError(t, os.WriteFile(target, []byte("binary-v1"), 0o755))
		require.NoError(t, os.WriteFile(candidate, []byte("binary-v2"), 0o755))

		t0 := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
		now := t0
		window := 15 * time.Minute

		err := ServerSwitch(ctx, ServerSwitchOptions{
			Binary:   candidate,
			Target:   target,
			Rollback: true,
			Window:   window,
			Now:      func() time.Time { return now },
		})
		require.NoError(t, err)

		// 5 minutes later, a land fails with exit 1 (card refusal)
		now = t0.Add(5 * time.Minute)
		landErr := &LandError{Msg: "land failed: batch refused (exit 1)", Code: 1}
		rolledBack, err := CheckRollbackOnLandFailure(target, landErr, now)
		require.NoError(t, err)
		assert.False(t, rolledBack, "exit 1 (card refusal) must not trigger rollback")

		// Target must still be candidate (no rollback)
		cur, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "binary-v2", string(cur), "target must stay at candidate after exit 1")

		// State file must still exist (window still open)
		stateFile := target + ".switch.json"
		_, err = os.Stat(stateFile)
		assert.NoError(t, err, "state file must remain when rollback is skipped")
	})

	t.Run("exit 2 (pass fault) triggers rollback", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		target := filepath.Join(dir, "nova-sprint")
		candidate := filepath.Join(dir, "nova-sprint-candidate")

		require.NoError(t, os.WriteFile(target, []byte("binary-v1"), 0o755))
		require.NoError(t, os.WriteFile(candidate, []byte("binary-v2"), 0o755))

		t0 := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
		now := t0
		window := 15 * time.Minute

		err := ServerSwitch(ctx, ServerSwitchOptions{
			Binary:   candidate,
			Target:   target,
			Rollback: true,
			Window:   window,
			Now:      func() time.Time { return now },
		})
		require.NoError(t, err)

		// 5 minutes later, a land fails with exit 2 (pass fault)
		now = t0.Add(5 * time.Minute)
		landErr := &LandError{Msg: "land failed: store did not answer (exit 2)", Code: 2}
		rolledBack, err := CheckRollbackOnLandFailure(target, landErr, now)
		require.NoError(t, err)
		assert.True(t, rolledBack, "exit 2 (pass fault) must trigger rollback")

		// Target must be rolled back to v1
		restored, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "binary-v1", string(restored), "target must be restored after exit 2")

		// State file must be removed after rollback
		stateFile := target + ".switch.json"
		_, err = os.Stat(stateFile)
		assert.True(t, os.IsNotExist(err), "state file must be removed after rollback")
	})

	t.Run("exit 2 outside window does not trigger rollback", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		target := filepath.Join(dir, "nova-sprint")
		candidate := filepath.Join(dir, "nova-sprint-candidate")

		require.NoError(t, os.WriteFile(target, []byte("binary-v1"), 0o755))
		require.NoError(t, os.WriteFile(candidate, []byte("binary-v2"), 0o755))

		t0 := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
		now := t0
		window := 15 * time.Minute

		err := ServerSwitch(ctx, ServerSwitchOptions{
			Binary:   candidate,
			Target:   target,
			Rollback: true,
			Window:   window,
			Now:      func() time.Time { return now },
		})
		require.NoError(t, err)

		// 20 minutes later (past 15 minute window), a land fails with exit 2
		now = t0.Add(20 * time.Minute)
		landErr := &LandError{Msg: "land failed: store did not answer (exit 2)", Code: 2}
		rolledBack, err := CheckRollbackOnLandFailure(target, landErr, now)
		require.NoError(t, err)
		assert.False(t, rolledBack, "failure outside window must not roll back")

		// Target must stay at candidate version
		cur, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "binary-v2", string(cur), "target must stay at candidate after window expires")
	})

	t.Run("exit 1 outside window does not trigger rollback", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		target := filepath.Join(dir, "nova-sprint")
		candidate := filepath.Join(dir, "nova-sprint-candidate")

		require.NoError(t, os.WriteFile(target, []byte("binary-v1"), 0o755))
		require.NoError(t, os.WriteFile(candidate, []byte("binary-v2"), 0o755))

		t0 := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
		now := t0
		window := 15 * time.Minute

		err := ServerSwitch(ctx, ServerSwitchOptions{
			Binary:   candidate,
			Target:   target,
			Rollback: true,
			Window:   window,
			Now:      func() time.Time { return now },
		})
		require.NoError(t, err)

		// 20 minutes later (past 15 minute window), a land fails with exit 1
		now = t0.Add(20 * time.Minute)
		landErr := &LandError{Msg: "land failed: batch refused (exit 1)", Code: 1}
		rolledBack, err := CheckRollbackOnLandFailure(target, landErr, now)
		require.NoError(t, err)
		assert.False(t, rolledBack, "exit 1 outside window must not roll back")

		// Target must stay at candidate version
		cur, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "binary-v2", string(cur), "target must stay at candidate after window expires")
	})
}
