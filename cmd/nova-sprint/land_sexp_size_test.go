package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sizeFixesFile is a fixes file of n entries, each with its own id: a few thousand
// of them stand past the 512 KiB bound the shared roadmap data was held to before
// (the schema repository's docs/roadmap.sexp is 1,081,875 bytes).
func sizeFixesFile(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fixItem("i" + strconv.Itoa(i))
	}
	return fixesFile(items...)
}

// TestTheLanderMergesRoadmapDataPastHalfAMegabyte holds the lander's decode of the
// shared roadmap data to landDataLimits (tla/LandLedgers.tla, ResolveLand): data past
// the old 512 KiB bound merges entry by entry, both sides' additions kept, as the
// by-form stage stageSexpUnion runs does (mergeByForm, then sexpEntriesMerged); data
// past the new 16 MiB bound is still refused whole, naming --max-bytes.
func TestTheLanderMergesRoadmapDataPastHalfAMegabyte(t *testing.T) {
	t.Parallel()

	const n = 9_000
	base := sizeFixesFile(n)
	require.Greater(t, len(base), 600<<10, "the base stands past 600 KiB")
	// each side adds one different entry at the end of :items
	add := func(id string) string {
		return strings.TrimSuffix(base, "))\n") + "\n\n  " + fixItem(id) + "))\n"
	}
	ours, theirs := add("ours-one"), add("theirs-one")

	byForm, err := mergeByForm("docs/fixes.sexp", []byte(base), []byte(ours), []byte(theirs))
	require.NoError(t, err, "the merge by form reads data past 512 KiB")
	require.NoError(t, sexpEntriesMerged("fixes", "docs/fixes.sexp", []byte(base), []byte(ours), []byte(theirs), byForm))
	got := string(byForm)
	assert.Contains(t, got, `"ours-one"`, "the tip's added entry is kept")
	assert.Contains(t, got, `"theirs-one"`, "the card's added entry is kept")

	// data past the lander's own 16 MiB bound is refused whole, never truncated,
	// naming that bound and not the 512 KiB one
	big := sizeFixesFile(230_000)
	require.Greater(t, len(big), landDataLimits.MaxBytes, "the data stands past the lander bound")
	_, err = mergeByForm("docs/fixes.sexp", []byte(base), []byte(big), []byte(big))
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("--max-bytes=%d", landDataLimits.MaxBytes))
	assert.Contains(t, err.Error(), "refused whole, never truncated")
}
