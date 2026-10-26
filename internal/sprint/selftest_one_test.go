package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelftestLandAddsItsOneCardWithOne pins:
// - selftest land's add command names --one, because add refuses a single card without it
// - that add command asks for exactly one card (--count 1)
// - every command in the selftest land flow names --redis
// (docs/SPEC-SPRINT.md section 14; item 14).
func TestSelftestLandAddsItsOneCardWithOne(t *testing.T) {
	t.Parallel()

	commands := selftestLandCommands("mem:selftest.twin", "/scratch/canned-brief.md", "abc123", "sprint/selftest-canned-1")
	require.NotEmpty(t, commands, "selftest land must have commands")

	var add []string
	for _, argv := range commands {
		require.Contains(t, argv, "--redis", "every selftest land command must name --redis: %v", argv)
		if len(argv) > 0 && argv[0] == "add" {
			add = argv
		}
	}
	require.NotNil(t, add, "selftest land must add a card")
	assert.Contains(t, add, "--one", "add must name --one for a single card")

	count := -1
	for i, arg := range add {
		if arg == "--count" {
			count = i
			break
		}
	}
	require.NotEqual(t, -1, count, "add must name --count: %v", add)
	require.Less(t, count+1, len(add), "add must give --count a value: %v", add)
	assert.Equal(t, "1", add[count+1], "add must ask for exactly one card")
}
