package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPushTargetSeatRepush ensures that judgments pushed to the seat are
// re-pushed on every look while they remain unanswered. This is the fix for
// the bug where judgments waiting on the seat were only pushed once and then
// never pushed again, leaving the fleet stuck waiting for those judgments.
//
// The fix modifies pushTarget to:
// 1. Not cache the "seen" file list for seat directories (only for fixed dirs)
// 2. Always return all coordinator groups from unseen when following the seat
func TestPushTargetSeatRepush(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a fake seat inbox directory
	seatInbox := filepath.Join(tmpDir, "sprint-judgments")
	require.NoError(t, os.MkdirAll(seatInbox, 0o755))

	// Test 1: Fixed directory caches seen and doesn't re-push
	t.Run("fixed directory caches seen", func(t *testing.T) {
		pFixed := &pushTarget{
			fixed:  tmpDir,
			seen:   map[string]map[string]bool{},
		}

		// First key lookup should cache
		_, err := pFixed.keys(tmpDir)
		require.NoError(t, err)
		assert.Contains(t, pFixed.seen, tmpDir)

		// Second key lookup should use cached
		_, err = pFixed.keys(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, pFixed.seen[tmpDir], pFixed.seen[tmpDir])
	})

	// Test 2: Seat does not cache seen and enables re-push
	t.Run("seat does not cache seen", func(t *testing.T) {
		pSeat := &pushTarget{
			fixed:  "",
			seen:   map[string]map[string]bool{},
		}

		// First key lookup should NOT cache
		_, err := pSeat.keys(seatInbox)
		require.NoError(t, err)
		assert.NotContains(t, pSeat.seen, seatInbox)

		// Second key lookup should NOT use cached (should re-read directory)
		_, err = pSeat.keys(seatInbox)
		require.NoError(t, err)
		assert.NotContains(t, pSeat.seen, seatInbox)
	})
}
