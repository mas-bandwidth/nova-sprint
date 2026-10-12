package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

func writeBrief(t *testing.T, lead string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "brief.md")
	require.NoError(t, os.WriteFile(path, []byte(lead+"\n\n"+swarm.ChildRulesParagraph()), 0o600))
	return path
}

func heavyBriefFile(t *testing.T) string {
	t.Helper()
	return writeBrief(t, "tier: heavy")
}

// TestAddStoresTierFromBriefLine1Heavy verifies that add stores heavy tier from line 1.
func TestAddStoresTierFromBriefLine1Heavy(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --one --brief-file " + heavyBriefFile(t))

	cardOut := ta.json("card s1-1 --json")
	assert.Contains(t, cardOut, `"tier":"heavy"`, "the card should have tier=heavy stored")
}

// TestAddStoresTierFromBriefLine1Pro verifies that add stores pro tier from line 1.
func TestAddStoresTierFromBriefLine1Pro(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --one --brief-file " + proBriefFile(t))

	cardOut := ta.json("card s1-1 --json")
	assert.Contains(t, cardOut, `"tier":"pro"`, "the card should have tier=pro stored")
}
