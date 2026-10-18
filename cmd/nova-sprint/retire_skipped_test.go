package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// tick --retire-only on a STOPPED machine (v1.5.4): the need-blocked judgment
// of a need no card has any more (the 2026-10-17 j24647) retires with the
// machine's answer, a dry run first writing nothing, and the machine stays
// STOPPED. tla/JudgmentRetire.tla RetireOnly.
func TestTickRetireOnlyRetiresAGoneNeedsJudgmentWhileStopped(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	ctx := context.Background()
	_, err = st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NNeedBlocked, Stream: "s1",
		Primaries: []string{"an earlier card"}, Count: 1, At: ta.a.now(), Marked: true, What: "an earlier card (absent) blocks 1 cards: s1-1 (s1)"}))
	require.NoError(t, err)
	open := func() bool {
		o, err := ta.m.OpenNotes(ctx)
		require.NoError(t, err)
		for _, x := range o {
			if x.Note.Type == sprint.NNeedBlocked {
				return true
			}
		}
		return false
	}
	require.True(t, open(), "the fixture's judgment is not open")

	out := ta.ok("tick --retire-only --dry-run")
	assert.Contains(t, out, "WOULD RETIRE judgment=")
	assert.Contains(t, out, "subject=\"an earlier card\"")
	assert.Contains(t, out, "DRY-RUN")
	assert.True(t, open(), "a dry run retired the judgment")

	out = ta.ok("tick --retire-only")
	assert.Contains(t, out, "RETIRED judgment=")
	assert.Contains(t, out, "answer=\"no dependents remain\"")
	assert.Contains(t, out, "TICK RETIRE OK judgments=1 subjects=1")
	assert.False(t, open(), "the judgment is still open")
	m, _, err := st.Machine(ctx)
	require.NoError(t, err)
	assert.False(t, m.Running(), "the retire started the machine")

	out = ta.ok("tick --retire-only")
	assert.Contains(t, out, "TICK RETIRE OK judgments=0 subjects=0", "a second retire")
	code, _, errs := ta.do("tick --retire-only --shadow")
	assert.Equal(t, 2, code, "--retire-only with --shadow: %s", errs)
	code, _, errs = ta.do("tick --dry-run")
	assert.Equal(t, 2, code, "--dry-run without --retire-only: %s", errs)
}

// repair --skipped names what it cannot reconcile: no open judgment by that id.
func TestRepairSkippedRefusesAJudgmentThatIsNotOpen(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	code, _, errs := ta.do("repair --skipped tick-drain-1.skip --dry-run")
	assert.Equal(t, 1, code, "%s", errs)
	assert.True(t, strings.Contains(errs, "no open judgment is tick-drain-1.skip"), "%s", errs)
}
