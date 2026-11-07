package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
)

// TestPreferSpreadsByLoad (directive 62): within a rank the fewest working
// come first, so the row first by name is not filled to width while its
// peers idle; the rank order (wanted rank and up, nearest first, then
// below, highest first) and room first still hold.
func TestPreferSpreadsByLoad(t *testing.T) {
	cand := func(name, rank string, working, width int) starlark.Value {
		return newStruct("name", name, "rank", rank, "working", working, "width", width)
	}
	cands := starlark.Tuple{
		cand("amd-1", "frontier", 3, 8), cand("amd-2", "frontier", 0, 8), cand("amd-3", "frontier", 1, 8),
		cand("f1", "pro", 0, 1), cand("full", "frontier", 8, 8),
	}
	l, err := preferred(cands, rung("heavy"))
	require.NoError(t, err)
	var got []string
	for i := range l.Len() {
		got = append(got, string(l.Index(i).(starlark.String)))
	}
	assert.Equal(t, []string{"amd-2", "amd-3", "amd-1", "f1", "full"}, got)
}
