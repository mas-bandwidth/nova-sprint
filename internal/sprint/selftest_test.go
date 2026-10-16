package sprint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/testbin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// WithSelftestLander overrides the lander execution for testing.
func WithSelftestLander(fn func(ctx context.Context, repoDir string) error) SelftestLandOption {
	return func(c *selftestLandConfig) { c.lander = fn }
}

// TestSelftestLandLandsACannedCardAndSwitchRollsBack pins:
// - selftest land lands a canned card on a scratch clone with this binary; green on a good binary, red on a broken lander
// - server switch keeps the old binary and rolls back on a failed land in the window
// (docs/SPEC-SPRINT.md section 14; item 14).
func TestSelftestLandLandsACannedCardAndSwitchRollsBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("selftest land green on good lander stub", func(t *testing.T) {
		t.Parallel()
		scratch := t.TempDir()
		var landedCard string
		err := SelftestLand(ctx,
			WithSelftestScratch(scratch),
			WithSelftestLander(func(_ context.Context, repoDir string) error {
				landedCard = "s1-1"
				return nil
			}),
		)
		require.NoError(t, err, "selftest land must succeed on a good lander")
		assert.Equal(t, "s1-1", landedCard, "canned card must land")
	})

	t.Run("selftest land red on broken lander stub", func(t *testing.T) {
		t.Parallel()
		scratch := t.TempDir()
		err := SelftestLand(ctx,
			WithSelftestScratch(scratch),
			WithSelftestLander(func(_ context.Context, repoDir string) error {
				return errors.New("lander broken: failed to merge batch")
			}),
		)
		require.Error(t, err, "selftest land must fail on a broken lander")
		assert.Contains(t, err.Error(), "lander broken")
	})

	t.Run("selftest land red on real path broken lander exiting non-zero", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		brokenBin := filepath.Join(dir, "broken-lander.sh")
		require.NoError(t, testbin.WriteExecutable(brokenBin, []byte("#!/bin/sh\nexit 1\n"), 0o755))

		err := SelftestLand(ctx,
			WithSelftestScratch(filepath.Join(dir, "scratch")),
			WithSelftestBinary(brokenBin),
		)
		require.Error(t, err, "selftest land must be red when binary exits non-zero")
		assert.Contains(t, err.Error(), "failed")
	})

	t.Run("selftest land red on real path no-op lander exiting zero without landing commit", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		noopBin := filepath.Join(dir, "noop-lander.sh")
		require.NoError(t, testbin.WriteExecutable(noopBin, []byte("#!/bin/sh\nexit 0\n"), 0o755))

		err := SelftestLand(ctx,
			WithSelftestScratch(filepath.Join(dir, "scratch")),
			WithSelftestBinary(noopBin),
		)
		require.Error(t, err, "selftest land must be red when binary exits 0 without landing canned card")
		assert.Contains(t, err.Error(), "landing commit not found in origin main")
	})

	t.Run("selftest land green on real path good binary", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		goodBin := filepath.Join(dir, "good-lander.sh")
		require.NoError(t, os.WriteFile(goodBin, []byte(goodLander), 0o755))

		err := SelftestLand(ctx,
			WithSelftestScratch(filepath.Join(dir, "scratch")),
			WithSelftestBinary(goodBin),
		)
		require.NoError(t, err, "selftest land must be green on a good binary that lands the canned card")
	})

	t.Run("selftest land green on real path good binary with relative path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		goodBin := filepath.Join(dir, "good-lander.sh")
		require.NoError(t, os.WriteFile(goodBin, []byte(goodLander), 0o755))

		cwd, err := os.Getwd()
		require.NoError(t, err)
		relBin, err := filepath.Rel(cwd, goodBin)
		require.NoError(t, err)

		err = SelftestLand(ctx,
			WithSelftestScratch(filepath.Join(dir, "scratch")),
			WithSelftestBinary(relBin),
		)
		require.NoError(t, err, "selftest land must work when binary is specified via relative path")
	})

	t.Run("server switch keeps old binary and rolls back on failed land in window", func(t *testing.T) {
		t.Parallel()
		target := fSwitched(t, fSwitchT0, 15*time.Minute)
		cur, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "binary-v2", string(cur), "target must be switched to candidate")
		prev, err := os.ReadFile(target + ".prev")
		require.NoError(t, err)
		assert.Equal(t, "binary-v1", string(prev), "previous binary must be kept at target.prev")

		// 5 minutes later, a land fails in the window:
		rolledBack, err := CheckRollbackOnLandFailure(target, errors.New("land failed: test check failed"), fSwitchT0.Add(5*time.Minute))
		require.NoError(t, err)
		assert.True(t, rolledBack, "failed land within window must trigger rollback")
		restored, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "binary-v1", string(restored), "target must be restored to previous binary after rollback")
	})

	t.Run("server switch keeps candidate if window expires without failed land", func(t *testing.T) {
		t.Parallel()
		target := fSwitched(t, fSwitchT0, 15*time.Minute)
		// 20 minutes later (past 15 minute window), a land fails:
		rolledBack, err := CheckRollbackOnLandFailure(target, errors.New("unrelated failure after window"), fSwitchT0.Add(20*time.Minute))
		require.NoError(t, err)
		assert.False(t, rolledBack, "failure outside window must not roll back")
		cur, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "binary-v2", string(cur), "target must stay at candidate version")
	})

	t.Run("server switch explicit rollback restores previous binary", func(t *testing.T) {
		t.Parallel()
		target := fSwitched(t, time.Time{}, 0)
		require.NoError(t, ServerRollback(ctx, target), "explicit rollback must succeed")
		restored, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "binary-v1", string(restored), "explicit rollback must restore previous binary")
	})
}

// goodLander is a lander script that lands the canned card on origin main.
const goodLander = `#!/bin/sh
if [ "$1" = "land" ]; then
	git merge -q --no-ff sprint/selftest-canned-1 -m "land selftest-1"
	git push -q origin main
fi
exit 0
`
