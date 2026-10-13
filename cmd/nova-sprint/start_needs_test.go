package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// A held need is found at check and while the sprint runs (v1.2.17,
// a-missing-or-blocked-need-is-found-at-check-and-while-running): start computes the unresolved
// needs live, before the check record's gate, so a hold raised after a clean check refuses the
// start; brief check names a held need of a brief not yet added through add's own plan. The
// store is the in-memory twin on the test's clock (tla/NeedsBlocked.tla StartOnlyResolved).

// openBound opens the bound judgment on card, opened ago before the test's clock (the tick's
// AtRedealBound raises it at a card's attempt bound; no decision taken).
func (ta *testApp) openBound(card, stream string, ago time.Duration) {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "machine"})
	require.NoError(ta.t, err)
	res, err := st.Run(context.Background(), store.Step{Verb: "bound", Load: []string{sprint.Work}, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NBound, Stream: stream, Primaries: []string{card}, Count: 1,
			At: s.Now.Add(-ago), Decisions: append([]string(nil), sprint.Decisions[sprint.NBound]...)}}}
	}})
	require.NoError(ta.t, err)
	require.Empty(ta.t, res.Refused)
}

func TestStartRefusesAHeldNeedAfterACleanCheck(t *testing.T) {
	t.Parallel()
	ta := gateOn(t)
	ta.seedClean()
	ta.ok("check cards")
	ta.ok("start")
	ta.ok("stop --reason 'a card reaches its bound' --until 30m")

	// while STOPPED, a1 reaches its bound and no one decides; a2 needs a1
	ta.openBound("a1", "s1", 3*time.Hour)
	code, out, errs := ta.do("start")
	require.Equal(t, 1, code, "refused: %s%s", out, errs)
	assert.Contains(t, errs, "run nova-sprint check cards: a2 needs a1 (held)")
	assert.Contains(t, errs, "nothing was written")
	assert.False(t, ta.machineRunning(), "the record is clean and its digest matches: the live needs refuse")

	code, out, _ = ta.do("check cards")
	require.Equal(t, 1, code, out)
	assert.Contains(t, out, "CHECK needs a2 a1 held: a1 is held by judgment ")
	assert.Contains(t, out, "remedy=nova-sprint rework a1 --fix <text>")
	assert.Contains(t, out, "CHECK CARDS FAILED kinds=needs:1")
}

// brief check holds an unadded brief to add's plan, whose needs arm now sees a hold.
func TestBriefCheckNamesAHeldNeed(t *testing.T) {
	t.Parallel()
	ta := gateOn(t)
	ta.seedClean()
	ta.openBound("a1", "s1", time.Hour)
	dir := t.TempDir()
	brief := filepath.Join(dir, "n2.md")
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief("RESULT: n2 sha=0123456789ab tier: pro\nNeeds: a1\nPATHS: internal/n2.go\nTHE TASK. Do the work after a1.")), 0o600))
	code, out, errs := ta.do("brief check --stream s1 " + brief)
	require.Equal(t, 1, code, "%s%s", out, errs)
	assert.Contains(t, out, "CHECK "+brief+" plan: "+brief+": needs a1 (held): a1 is held by judgment ")
}
