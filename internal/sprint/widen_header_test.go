package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const widenHeaderCarry = "CARRY: c1 attempt 2 head=0123456789abcdef0123456789abcdef01234567"

const widenHeaderBrief = "c: widen only the header (w) tier: pro\n" +
	"REPO: mas-bandwidth/nova-sprint\n" +
	"BASE: sprint/v17\n" +
	"PATHS: a.go\n" +
	"SHARED: none\n" +
	"\n" +
	"The prose:\n" +
	"  PATHS: quoted.go\n" +
	"SHARED: quoted.txt\n" +
	"CARRY: quoted\n"

// A widen edits the header's scope lines only (this card's own bug: the prose that quotes a
// PATHS: or CARRY: line was rewritten by every widen). PathsWidened and SharedWidened read
// and write index 1 up to HeaderEnd, so a quoted line below the header is byte-identical; a
// header that names PATHS or SHARED twice is left whole for a hand.
func TestAWidenChangesOnlyTheHeaderScopeLines(t *testing.T) {
	t.Parallel()

	wantPaths := "c: widen only the header (w) tier: pro\n" +
		widenHeaderCarry + "\n" +
		"REPO: mas-bandwidth/nova-sprint\n" +
		"BASE: sprint/v17\n" +
		"PATHS: a.go,b.go\n" +
		"SHARED: none\n" +
		"\n" +
		"The prose:\n" +
		"  PATHS: quoted.go\n" +
		"SHARED: quoted.txt\n" +
		"CARRY: quoted\n"
	got := PathsWidened(widenHeaderBrief, []string{"b.go"}, widenHeaderCarry)
	assert.Equal(t, wantPaths, got)
	for _, prose := range []string{"  PATHS: quoted.go\n", "SHARED: quoted.txt\n", "CARRY: quoted\n"} {
		assert.Contains(t, got, "\n"+prose, "the prose line is byte-identical")
	}

	wantShared := "c: widen only the header (w) tier: pro\n" +
		"REPO: mas-bandwidth/nova-sprint\n" +
		"BASE: sprint/v17\n" +
		"PATHS: a.go\n" +
		"SHARED: b.txt\n" +
		"\n" +
		"The prose:\n" +
		"  PATHS: quoted.go\n" +
		"SHARED: quoted.txt\n" +
		"CARRY: quoted\n"
	got = SharedWidened(widenHeaderBrief, []string{"b.txt"})
	assert.Equal(t, wantShared, got)
	for _, prose := range []string{"  PATHS: quoted.go\n", "SHARED: quoted.txt\n", "CARRY: quoted\n"} {
		assert.Contains(t, got, "\n"+prose, "the prose line is byte-identical")
	}

	assert.Equal(t, 5, HeaderEnd(widenHeaderBrief), "the header ends at the blank line")

	twice := "c: twice (w) tier: pro\nPATHS: a.go\nPATHS: b.go\n\n  PATHS: quoted.go\n"
	assert.Equal(t, twice, PathsWidened(twice, []string{"c.go"}, widenHeaderCarry),
		"a header naming PATHS twice is not widened")

	twiceShared := "c: twice (w) tier: pro\nSHARED: a.go\nSHARED: b.go\n\nSHARED: quoted.txt\n"
	assert.Equal(t, twiceShared, SharedWidened(twiceShared, []string{"c.go"}),
		"a header naming SHARED twice is not widened")

	crossPaths := "c: crossed (w) tier: pro\nPATHS: a.go\nSHARED: x.txt\nSHARED: y.txt\n\nPATHS: quoted.go\n"
	assert.Equal(t, crossPaths, PathsWidened(crossPaths, []string{"c.go"}, widenHeaderCarry),
		"a header naming SHARED twice is not widened by PathsWidened")

	crossShared := "c: crossed (w) tier: pro\nPATHS: a.go\nPATHS: b.go\nSHARED: x.txt\n\nSHARED: quoted.txt\n"
	assert.Equal(t, crossShared, SharedWidened(crossShared, []string{"c.go"}),
		"a header naming PATHS twice is not widened by SharedWidened")

	assert.Equal(t, crossPaths, WidenBrief(crossPaths, []string{"c.go"}, widenHeaderCarry),
		"a header naming SHARED twice is not widened by WidenBrief")
}
