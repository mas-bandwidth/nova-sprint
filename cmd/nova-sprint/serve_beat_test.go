package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The member's beat is what nova-tools pkg/member sends (Beat, drainArgs, noRoomArgs):
// `fleet beat <member> [--stop-returns <n>] [--no-room <why>] [--draining] [--live <set>]
// --load <percent>`. The optional words may stand in any order and --load anywhere among
// them; --load is required, because the server cannot measure the worker's machine. A
// member that drains (its binary was replaced) or is at its disk floor sends --draining or
// --no-room and used to be refused on every beat, so its row read down for the length of
// its drain. Both repositories' tests pin this list (docs/SPEC-SPRINT.md, the fleet; the
// beat; docs/CLI.md). README: the tests pin the wire.
func TestTheServerBeatTakesDrainingAndNoRoom(t *testing.T) {
	t.Parallel()

	accepted := [][]string{
		{"fleet", "beat", "m", "--draining", "--load", "70"},
		{"fleet", "beat", "m", "--no-room", "free disk 0.0 GiB on /slots, under the floor of 10 GiB", "--load", "70"},
		{"fleet", "beat", "m", "--draining", "--no-room", "free disk under the floor", "--load", "70"},
		{"fleet", "beat", "m", "--live", "c1@1,c2@1:held", "--load", "70"},
		// every optional word, any order, --load anywhere among them
		{"fleet", "beat", "m", "--stop-returns", "2", "--no-room", "why", "--draining", "--live", "c1@1", "--load", "3"},
		{"fleet", "beat", "m", "--load", "70", "--draining"},
		{"fleet", "beat", "m", "--load", "1", "--stop-returns", "1"},
	}
	for _, argv := range accepted {
		as, words, why := workerVerb(argv)
		require.Empty(t, why, "the server runs %v", argv)
		assert.Equal(t, "m", as, "%v", argv)
		assert.Equal(t, 2, words, "%v", argv)
	}

	// an unknown word is refused, and the refusal names it
	_, _, why := workerVerb([]string{"fleet", "beat", "m", "--cores", "4", "--load", "1"})
	require.NotEmpty(t, why, "an unknown word is refused")
	assert.Contains(t, why, "--cores", "the refusal names the word it did not take: %s", why)

	// a beat with no --load is refused: the server cannot measure the worker's machine
	_, _, why = workerVerb([]string{"fleet", "beat", "m"})
	require.NotEmpty(t, why, "a beat with no --load is refused")
	assert.Contains(t, why, "--load", "%s", why)

	// a known word with a value of the wrong shape is still refused
	for _, argv := range [][]string{
		{"fleet", "beat", "m", "--stop-returns", "-1", "--load", "1"},
		{"fleet", "beat", "m", "--stop-returns", "x", "--load", "1"},
		{"fleet", "beat", "m", "--load", "notanumber"},
		{"fleet", "beat", "m", "--live", "", "--load", "1"},
		{"fleet", "beat", "m", "--live", "not a set", "--load", "1"},
	} {
		_, _, why = workerVerb(argv)
		assert.NotEmpty(t, why, "refused: %v", argv)
	}
}

// TestTheServerBeatTakesFacts: a member's beat carries its gate facts (--facts <list>), the
// member's own measure of its machine, which the deal holds a GATE-NEEDS card against. The
// server takes the word anywhere among the others, and refuses a list that is not fact words.
func TestTheServerBeatTakesFacts(t *testing.T) {
	t.Parallel()
	for _, argv := range [][]string{
		{"fleet", "beat", "m", "--stop-returns", "0", "--facts", "linux,amd64,landlock,tla,go", "--load", "0.4"},
		{"fleet", "beat", "m", "--live", "-", "--load", "0.4", "--facts", "darwin,arm64"},
	} {
		as, words, why := workerVerb(argv)
		require.Empty(t, why, "the server runs %v", argv)
		assert.Equal(t, "m", as)
		assert.Equal(t, 2, words)
	}
	for _, argv := range [][]string{
		{"fleet", "beat", "m", "--facts", "", "--load", "1"},
		{"fleet", "beat", "m", "--facts", "linux,a b", "--load", "1"},
		{"fleet", "beat", "m", "--facts"},
	} {
		_, _, why := workerVerb(argv)
		assert.NotEmpty(t, why, "refused: %v", argv)
	}
}
