package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-10-17 (Glenn: "add this to v1.7 for card lint"): one change carded twice
// is named by add and preflight, and admitted only with --not-a-duplicate-of.
func TestAProbableDuplicateIsNamedAndASharedFileAloneIsNot(t *testing.T) {
	t.Parallel()
	read := func(id string) dupCard {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("testdata", "duplicate", id+".md"))
		require.NoError(t, err)
		return dupCardOf(id, string(b))
	}
	anyLength, strict := read("go-timestamp-text-any-length"), read("schema-go-timestamp-strict-rfc3339")
	d, ok := probableDuplicate(anyLength, strict)
	require.True(t, ok, "the timestamp pair is one change carded twice")
	assert.Equal(t, "schema-go-timestamp-strict-rfc3339", d.Of)
	assert.GreaterOrEqual(t, d.Score, duplicateThreshold)
	assert.Contains(t, d.Files, "internal/codegen/gotable/json.go", "the shared file is named")
	assert.NotEmpty(t, d.Shared, "the shared words are named")
	assert.Contains(t, d.String(), "schema-go-timestamp-strict-rfc3339")

	budget, held := read("v17-land-gate-run-has-a-budget"), read("v17-land-gate-benches-include-held-members")
	_, ok = probableDuplicate(budget, held)
	assert.False(t, ok, "both change cmd/nova-sprint/landgo.go, for different work")
	_, ok = probableDuplicate(anyLength, budget)
	assert.False(t, ok, "another repository is never a duplicate")

	open := []dupCard{strict, budget}
	refused := duplicatesRefused(anyLength, open, nil)
	require.Len(t, refused, 1, "without the override the duplicate is refused")
	assert.Equal(t, "schema-go-timestamp-strict-rfc3339", refused[0].Of)
	assert.Empty(t, duplicatesRefused(anyLength, open, map[string]string{"schema-go-timestamp-strict-rfc3339": "rfc3339 strictness is a second change"}),
		"--not-a-duplicate-of with a reason admits it")
	assert.Len(t, duplicatesRefused(anyLength, open, map[string]string{"schema-go-timestamp-strict-rfc3339": ""}), 1,
		"an override with no reason is no override")
}
