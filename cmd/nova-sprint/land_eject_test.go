package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The eject's cause is its class, not its words: the head in the words changes with every
// attempt, and a rework that fails the same way is the same cause.
func TestEjectCauseIsTheClassOfTheReason(t *testing.T) {
	t.Parallel()
	for why, cause := range map[string]string{
		"the head abc of c1: git refused to merge it: fatal: refusing to merge unrelated histories": "refused",
		"the head abc of c1 does not merge: CONFLICT (content)":                                     "conflict",
		"the head abc of c1 is missing: origin holds no such commit":                                "missing",
		"the head c1 of c1 is not a commit id (a finish without --head records the card's id)":      "not-commit",
		"the head abc of c1 fails the lander's checks: it changes files outside its PATHS (E12): x": "checks",
		"the head abc of c1 fails the tree gate: go vet":                                            "gate",
		"something else": "other",
	} {
		assert.Equal(t, cause, sprint.EjectCauseOf(why), why)
	}
	assert.True(t, strings.HasPrefix(sprint.NMergeStuck, "merge stuck"))
}
