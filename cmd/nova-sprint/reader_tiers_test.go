package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reader set and reader add --tiers write the tiers cell, where prints it
// (all when empty), and a tier that is not a route writes nothing.
func TestReaderSetPrintsTiersAndRefusesABadTier(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("reader add reader-m1 --tiers flash")
	ta.ok("reader set reader-a --tiers pro,flash")

	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	readers := v.Tables[sprint.Readers]
	require.Equal(t, "flash", readers["reader-m1"][sprint.ReaderTiers])
	require.Equal(t, "flash,pro", readers["reader-a"][sprint.ReaderTiers], "stored in ladder order")
	require.Equal(t, "default", readers["reader-b"][sprint.ReaderTiers], "an empty cell prints default: flash on a fleet reader while the store holds routes")

	code, _, errs := ta.do("reader set reader-a --tiers no-such")
	assert.NotEqual(t, 0, code, errs)
	ta.json("where", &v)
	assert.Equal(t, "flash,pro", v.Tables[sprint.Readers]["reader-a"][sprint.ReaderTiers], "a bad tier writes nothing")

	ta.ok("reader set reader-a --tiers all")
	ta.json("where", &v)
	assert.Equal(t, "flash,pro,heavy,frontier", v.Tables[sprint.Readers]["reader-a"][sprint.ReaderTiers], "all names every tier")
	ta.ok("reader set reader-a --tiers default")
	ta.json("where", &v)
	assert.Equal(t, "default", v.Tables[sprint.Readers]["reader-a"][sprint.ReaderTiers])

	code, _, errs = ta.do("reader set reader-nope --tiers flash")
	assert.NotEqual(t, 0, code, errs)
	assert.Contains(t, errs, "nothing was changed")
}

// TestReaderAddInheritsCommonFleetTier checks that a fleet reader added without --tiers
// inherits the common fleet tier when all other fleet readers agree on it.
func TestReaderAddInheritsCommonFleetTier(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1,m3")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash,pro,heavy")
	ta.ok("reader add reader-m3")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	assert.Equal(t, "flash,pro,heavy", v.Tables[sprint.Readers]["reader-m3"][sprint.ReaderTiers])
}

// TestReaderAddWhenFleetReadersDiffer stores empty cell with from=default message.
func TestReaderAddWhenFleetReadersDiffer(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1,m3")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash,pro")
	code, out, _ := ta.do("reader add reader-m3")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "from=default")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	assert.Equal(t, "default", v.Tables[sprint.Readers]["reader-m3"][sprint.ReaderTiers])
}

// TestReaderAddWithExplicitTiers stores the explicit tier regardless of fleet state.
func TestReaderAddWithExplicitTiers(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1,m3")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash,pro,heavy")
	ta.ok("reader add reader-m3 --tiers flash")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	assert.Equal(t, "flash", v.Tables[sprint.Readers]["reader-m3"][sprint.ReaderTiers])
}
