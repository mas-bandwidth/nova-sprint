package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestReaderAddInheritsFleetTiers tests that a fleet reader added without --tiers
// inherits the tiers from other fleet readers when they all hold the same non-empty tiers.
func TestReaderAddInheritsFleetTiers(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1")
	// Set both existing fleet readers to flash,pro,heavy
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash,pro,heavy")
	// Add new fleet reader without --tiers
	ta.ok("reader add reader-m3")
	// New fleet reader should inherit flash,pro,heavy
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	readers := v.Tables[sprint.Readers]
	require.Equal(t, "flash,pro,heavy", readers["reader-m3"][sprint.ReaderTiers], "new fleet reader inherits tiers from fleet")
}

// TestReaderAddFleetTiersDiffer stores empty cell when fleet readers have different tiers.
func TestReaderAddFleetTiersDiffer(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash")
	ta.ok("reader add reader-m3")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	readers := v.Tables[sprint.Readers]
	require.Equal(t, "default", readers["reader-m3"][sprint.ReaderTiers], "empty cell when fleet tiers differ")
}

// TestReaderAddFleetNoOther stores empty cell when no other fleet readers exist.
func TestReaderAddFleetNoOther(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1 --members m1")
	ta.ok("reader add reader-m2")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	readers := v.Tables[sprint.Readers]
	require.Equal(t, "default", readers["reader-m2"][sprint.ReaderTiers], "empty cell when no other fleet readers")
}

// TestReaderAddExplicitTiersWins tests that explicit --tiers takes precedence.
func TestReaderAddExplicitTiersWins(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash,pro,heavy")
	ta.ok("reader add reader-m3 --tiers pro")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	readers := v.Tables[sprint.Readers]
	require.Equal(t, "pro", readers["reader-m3"][sprint.ReaderTiers], "explicit --tiers wins")
}

// TestInitReadersInheritsFleetTiers tests that init --readers follows the same rule.
func TestInitReadersInheritsFleetTiers(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash,pro,heavy")
	ta.ok("init --readers reader-m3")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	readers := v.Tables[sprint.Readers]
	require.Equal(t, "flash,pro,heavy", readers["reader-m3"][sprint.ReaderTiers], "init --readers inherits tiers")
}
