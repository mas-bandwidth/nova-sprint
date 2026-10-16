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
// on exit 2 (pass fault), not exit 1 (card's own refusal), and only inside the
// 15-minute window after a switch.
func TestCheckRollbackOnLandFailureExitCodes(t *testing.T) {
	t.Parallel()
	refusal := &LandError{Msg: "land failed: batch refused (exit 1)", Code: 1}
	fault := &LandError{Msg: "land failed: store did not answer (exit 2)", Code: 2}
	for _, tc := range []struct {
		name               string
		after              time.Duration // since the switch, the window being 15m
		landErr            *LandError
		rolled             bool
		rolledMsg          string
		target, targetMsg  string
		stateKept, stateGo string // the state file's check, when either is set
	}{
		{"exit 1 (card refusal) does not trigger rollback", 5 * time.Minute, refusal,
			false, "exit 1 (card refusal) must not trigger rollback",
			"binary-v2", "target must stay at candidate after exit 1", "state file must remain when rollback is skipped", ""},
		{"exit 2 (pass fault) triggers rollback", 5 * time.Minute, fault,
			true, "exit 2 (pass fault) must trigger rollback",
			"binary-v1", "target must be restored after exit 2", "", "state file must be removed after rollback"},
		{"exit 2 outside window does not trigger rollback", 20 * time.Minute, fault,
			false, "failure outside window must not roll back",
			"binary-v2", "target must stay at candidate after window expires", "", ""},
		{"exit 1 outside window does not trigger rollback", 20 * time.Minute, refusal,
			false, "exit 1 outside window must not roll back",
			"binary-v2", "target must stay at candidate after window expires", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := fSwitched(t, fSwitchT0, 15*time.Minute)
			rolledBack, err := CheckRollbackOnLandFailure(target, tc.landErr, fSwitchT0.Add(tc.after))
			require.NoError(t, err)
			assert.Equal(t, tc.rolled, rolledBack, tc.rolledMsg)
			cur, err := os.ReadFile(target)
			require.NoError(t, err)
			assert.Equal(t, tc.target, string(cur), tc.targetMsg)

			_, err = os.Stat(target + ".switch.json")
			if tc.stateKept != "" {
				assert.NoError(t, err, tc.stateKept)
			}
			if tc.stateGo != "" {
				assert.True(t, os.IsNotExist(err), tc.stateGo)
			}
		})
	}
}

var fSwitchT0 = time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)

// fSwitched is a target nova-sprint holding binary-v1 switched at t0, with rollback, to a
// candidate holding binary-v2 (a zero t0 and window leave the switch its own defaults); it
// hands back the target's path.
func fSwitched(t *testing.T, t0 time.Time, window time.Duration) string {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "nova-sprint")
	candidate := filepath.Join(dir, "nova-sprint-candidate")
	require.NoError(t, os.WriteFile(target, []byte("binary-v1"), 0o755))
	require.NoError(t, os.WriteFile(candidate, []byte("binary-v2"), 0o755))
	opts := ServerSwitchOptions{Binary: candidate, Target: target, Rollback: true, Window: window}
	if !t0.IsZero() {
		opts.Now = func() time.Time { return t0 }
	}
	require.NoError(t, ServerSwitch(context.Background(), opts), "server switch must succeed")
	return target
}
