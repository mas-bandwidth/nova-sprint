package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const briefWidenHeaderCarry = "CARRY: c1 attempt 2 head=0123456789abcdef0123456789abcdef01234567"

const briefWidenHeaderBrief = "c: widen only the header (w) tier: pro\n" +
	"REPO: mas-bandwidth/nova-sprint\n" +
	"BASE: sprint/v17\n" +
	"PATHS: a.go\n" +
	"SHARED: none\n" +
	"\n" +
	"The prose:\n" +
	"  PATHS: quoted.go\n" +
	"SHARED: quoted.txt\n" +
	"CARRY: quoted\n"

// brief --widen edits the header's PATHS line only (this card's own bug: the union of the
// first header PATHS line was written over every PATHS line, header or prose, and over a
// quoted CARRY: line). widenBrief reads and writes index 1 up to sprint.HeaderEnd; a header
// that names PATHS or SHARED twice is left whole for the verb to refuse.
func TestBriefWidenChangesOnlyTheHeaderPaths(t *testing.T) {
	t.Parallel()

	want := "c: widen only the header (w) tier: pro\n" +
		briefWidenHeaderCarry + "\n" +
		"REPO: mas-bandwidth/nova-sprint\n" +
		"BASE: sprint/v17\n" +
		"PATHS: a.go,b.go\n" +
		"SHARED: none\n" +
		"\n" +
		"The prose:\n" +
		"  PATHS: quoted.go\n" +
		"SHARED: quoted.txt\n" +
		"CARRY: quoted\n"
	got := widenBrief(briefWidenHeaderBrief, []string{"b.go"}, briefWidenHeaderCarry)
	assert.Equal(t, want, got)
	for _, prose := range []string{"  PATHS: quoted.go\n", "SHARED: quoted.txt\n", "CARRY: quoted\n"} {
		assert.Contains(t, got, "\n"+prose, "the prose line is byte-identical")
	}

	twice := "c: twice (w) tier: pro\nPATHS: a.go\nPATHS: b.go\n\n  PATHS: quoted.go\n"
	assert.Equal(t, twice, widenBrief(twice, []string{"c.go"}, briefWidenHeaderCarry),
		"a header naming PATHS twice is not widened")

	twiceShared := "c: twice (w) tier: pro\nSHARED: a.go\nSHARED: b.go\n\nSHARED: quoted.txt\n"
	assert.Equal(t, twiceShared, widenBrief(twiceShared, []string{"c.go"}, briefWidenHeaderCarry),
		"a header naming SHARED twice is not widened")

	crossPaths := "c: crossed (w) tier: pro\nPATHS: a.go\nSHARED: x.txt\nSHARED: y.txt\n\nPATHS: quoted.go\n"
	assert.Equal(t, crossPaths, widenBrief(crossPaths, []string{"c.go"}, briefWidenHeaderCarry),
		"a header naming SHARED twice is not widened")

	crossShared := "c: crossed (w) tier: pro\nPATHS: a.go\nPATHS: b.go\nSHARED: x.txt\n\nSHARED: quoted.txt\n"
	assert.Equal(t, crossShared, widenBrief(crossShared, []string{"c.go"}, briefWidenHeaderCarry),
		"a header naming PATHS twice is not widened")
}
