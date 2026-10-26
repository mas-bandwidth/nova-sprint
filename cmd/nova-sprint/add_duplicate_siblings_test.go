package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// siblingDuplicateBrief uses enough common task words that changing only the
// card ID leaves token overlap above 0.96. The set header alone grants the
// exemption; it does not relax the duplicate threshold for ordinary briefs.
func siblingDuplicateBrief(id, set string) string {
	var words []string
	for i := 0; i < 150; i++ {
		words = append(words, fmt.Sprintf("marker%03d", i))
	}
	header := ""
	if set != "" {
		header = "GENERATED-SET: " + set + "\n"
	}
	return "RESULT: " + id + " sha=0123456789ab tier: pro\n" + header +
		"REPO: mas-bandwidth/nova-sprint\nPATHS: internal/ci/serial_test.go\n\nTHE TASK. " +
		strings.Join(words, " ") + "\n\nSTEP 1. read.\n"
}

func siblingRead(t *testing.T, dir, id, set string) dupCard {
	t.Helper()
	path := filepath.Join(dir, id+".md")
	require.NoError(t, os.WriteFile(path, []byte(siblingDuplicateBrief(id, set)), 0o600))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return dupCardOf(id, string(raw))
}

func TestCardsOfOneGeneratedDirectoryAreNotDuplicatesOfEachOther(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := siblingRead(t, dir, "generated-a", "set-one")
	b := siblingRead(t, dir, "generated-b", "set-one")
	c := siblingRead(t, dir, "generated-c", "set-one")
	f, duplicate := probableDuplicate(a, b)
	require.True(t, duplicate, "fixture must actually be a probable duplicate")
	require.GreaterOrEqual(t, f.Score, 0.96)
	held, _ := dupHeld([]dupCard{a, b, c}, nil, nil)
	assert.Empty(t, held, "siblings in one generated directory are admitted together")
	assert.Empty(t, duplicatesRefused(c, []dupCard{a}, nil), "an already-open member of the same set is exempt")
}

func TestAGeneratedSiblingIsStillADuplicateOfAnUnrelatedCard(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	incoming := siblingRead(t, dir, "generated-new", "set-one")
	withoutSet := siblingRead(t, dir, "plain-open", "")
	otherSet := siblingRead(t, dir, "other-open", "set-two")
	for _, open := range []dupCard{withoutSet, otherSet} {
		held, _ := dupHeld([]dupCard{incoming}, []dupCard{open}, nil)
		f, ok := held[incoming.ID]
		require.True(t, ok, "a card outside the generated set still holds the sibling")
		assert.Equal(t, open.ID, f.Of)
		assert.GreaterOrEqual(t, f.Score, 0.96)
	}
	held, _ := dupHeld([]dupCard{incoming, otherSet}, nil, nil)
	assert.Equal(t, incoming.ID, held[otherSet.ID].Of, "different generated sets in one directory still compare")
}
