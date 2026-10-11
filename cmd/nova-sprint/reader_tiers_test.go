package main

import (
	"strings"
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
	ta.ok("reader add reader-flash --tiers flash")
	ta.ok("reader set reader-a --tiers pro,flash")

	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	readers := v.Tables[sprint.Readers]
	require.Equal(t, "flash", readers["reader-flash"][sprint.ReaderTiers])
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

// TestReaderAddInheritsFleetTier checks that a fleet reader added without --tiers
// inherits the common fleet tier when all other fleet readers agree.
func TestReaderAddInheritsFleetTier(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash,pro,heavy")
	out := ta.ok("reader add reader-m3")
	require.Contains(t, out, "READER-ADD OK", "reader add output")
	require.Contains(t, out, "tiers=flash,pro,heavy", "inherits fleet tier")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	assert.Equal(t, "flash,pro,heavy", v.Tables[sprint.Readers]["reader-m3"][sprint.ReaderTiers])
}

// TestReaderAddWithDifferingFleetTiers checks that when fleet readers differ,
// a new fleet reader added without --tiers gets the empty cell (shown as "default").
func TestReaderAddWithDifferingFleetTiers(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers pro,heavy")
	out := ta.ok("reader add reader-m3")
	require.Contains(t, out, "READER-ADD OK", "reader add output")
	assert.Contains(t, out, "tiers=default", "keeps empty cell when fleet differs")
}

// TestReaderAddWithExplicitTiers checks that --tiers explicitly overrides any
// inheritance when adding a new reader.
func TestReaderAddWithExplicitTiers(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash,pro,heavy")
	out := ta.ok("reader add reader-m3 --tiers flash")
	require.Contains(t, out, "READER-ADD OK", "reader add output")
	assert.Contains(t, out, "tiers=flash", "explicit tiers wins")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	assert.Equal(t, "flash", v.Tables[sprint.Readers]["reader-m3"][sprint.ReaderTiers])
}

// TestInitReadersInheritsFleetTier checks that init --readers inherits
// the common fleet tier when adding new fleet readers alongside existing ones.
func TestInitReadersInheritsFleetTier(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1 --members m1")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	out := ta.ok("init --readers reader-m2,reader-m3 --members m1")
	require.Contains(t, out, "INIT OK", "init output")
	var v struct {
		Tables map[string]map[string]map[string]string
	}
	ta.json("where", &v)
	assert.Equal(t, "flash,pro,heavy", v.Tables[sprint.Readers]["reader-m2"][sprint.ReaderTiers])
	assert.Equal(t, "flash,pro,heavy", v.Tables[sprint.Readers]["reader-m3"][sprint.ReaderTiers])
}

// TestDoctorReadersNoDriftWhenAllAgree checks that doctor doesn't warn when
// all fleet readers agree on their tiers.
func TestDoctorReadersNoDriftWhenAllAgree(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2,reader-m3 --members m1")
	ta.ok("reader set reader-m1 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m2 --tiers flash,pro,heavy")
	ta.ok("reader set reader-m3 --tiers flash,pro,heavy")
	out := ta.ok("doctor --area readers")
	require.Contains(t, out, "OK", "doctor reports OK")
	assert.False(t, strings.Contains(out, "WARN"), "no warnings when all agree")
}
