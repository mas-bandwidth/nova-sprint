package sprint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelftestLandCheckSeesTheLandOnOriginMain pins that the land check reads
// origin main and sees the landing commit the lander wrote there: green on a
// lander that merges the canned card into main and pushes it (docs/SPEC-SPRINT.md
// section 14; item 14).
func TestSelftestLandCheckSeesTheLandOnOriginMain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	goodBin := filepath.Join(dir, "good-lander.sh")
	require.NoError(t, os.WriteFile(goodBin, []byte(goodLander), 0o755))

	err := SelftestLand(context.Background(),
		WithSelftestScratch(filepath.Join(dir, "scratch")),
		WithSelftestBinary(goodBin),
	)
	require.NoError(t, err, "selftest land must be green when the lander lands the canned card on origin main")
}

// TestSelftestLandCleansAnOldScratchAndKeepsNothingAfterAFailure pins:
//   - selftest land must not fail on a scratch a previous run left: an old `work`
//     directory is removed before the clone, so the run passes
//   - on a failure, the four names selftest land owns under the scratch (origin.git,
//     work, selftest.twin, canned-brief.md) are gone, and the scratch itself and
//     anything else in it remain
//
// (docs/SPEC-SPRINT.md section 14; item 14).
func TestSelftestLandCleansAnOldScratchAndKeepsNothingAfterAFailure(t *testing.T) {
	t.Parallel()

	owned := []string{"origin.git", "work", "selftest.twin", "canned-brief.md"}

	t.Run("an old scratch with a work directory passes", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		scratch := filepath.Join(dir, "scratch")
		require.NoError(t, os.MkdirAll(filepath.Join(scratch, "work"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(scratch, "work", "leftover.txt"), []byte("old\n"), 0o644))

		err := SelftestLand(context.Background(),
			WithSelftestScratch(scratch),
			WithSelftestLander(func(_ context.Context, _ string) error { return nil }),
		)
		require.NoError(t, err, "a leftover work directory must not fail the clone")
	})

	t.Run("a failing lander leaves the four owned names gone", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		scratch := filepath.Join(dir, "scratch")
		require.NoError(t, os.MkdirAll(scratch, 0o755))

		err := SelftestLand(context.Background(),
			WithSelftestScratch(scratch),
			WithSelftestLander(func(_ context.Context, _ string) error {
				return errors.New("lander broken: failed to merge batch")
			}),
		)
		require.Error(t, err, "a broken lander must fail selftest land")
		assert.Contains(t, err.Error(), "lander broken")

		require.DirExists(t, scratch, "the scratch directory itself must remain")
		for _, name := range owned {
			_, statErr := os.Stat(filepath.Join(scratch, name))
			require.True(t, os.IsNotExist(statErr), "%s must be removed after a failure", name)
		}
	})
}
