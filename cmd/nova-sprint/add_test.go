package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// add --plan <dir> is the plan review a seat reads before admitting a stream
// (docs/SPRINT-COORDINATOR.md, the plan review; docs/SPEC-CARD-CONTRACT.md, the
// shared-path rule): it prints the overlap table — one line per path two or more
// cards name in PATHS, with the cards, ledger or not, and the edge if any — and
// exits 0 without admitting, so the seat sees the overlaps first and can add the
// needs edge or split the path.
func TestAddPlanPrintsTheOverlapTableWithoutAdmitting(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	writeHeaderBrief(t, dir, "q1", "-", "internal/x.go internal/q1.go")
	writeHeaderBrief(t, dir, "q2", "-", "internal/q2.go internal/x.go")
	code, out, errs := ta.do("add --plan " + dir)
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "internal/x.go  q1,q2  not-ledger  no-edge\n")
	assert.Equal(t, "", errs, "a plan review refuses nothing")
	assert.False(t, ta.placed("q1") || ta.placed("q2"), "nothing admitted")
}

// A many-brief add prints the overlap table before it refuses the whole set on
// the first non-ledger overlap without an edge, writing nothing
// (docs/SPEC-CARD-CONTRACT.md, the shared-path rule).
func TestAddManyPrintsTheOverlapTableBeforeTheRefusal(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	writeHeaderBrief(t, dir, "q1", "-", "internal/x.go internal/q1.go")
	writeHeaderBrief(t, dir, "q2", "-", "internal/q2.go internal/x.go")
	code, out, errs := ta.do("add --stream s --brief-dir " + dir)
	require.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, out, "internal/x.go  q1,q2  not-ledger  no-edge\n")
	assert.Contains(t, errs, "internal/x.go is named in PATHS by q1 and q2, and neither has a soft dependency on the other (LAND-AFTER: <id>) or needs the other (DEPENDS-ON: <id>)")
	assert.False(t, ta.placed("q1") || ta.placed("q2"), "nothing written")
}
