package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The sprint-21 review could not measure what the lints cost: a refusal at add or brief
// left no trace in the store, so whether name-in-prose or the TEST-format rule churned was
// unknown. These tests pin that every add or brief lint refusal is written into the
// server's refused-verb record with the verb, the card id and the rule tokens it drew
// (paths-cover-named, test-named and so on) in Why, so a later review counts refusals per
// rule from the record (docs/SPEC-SPRINT.md, "The doctor's refused-verb and beat-form
// checks"); an accepted add records nothing.

// readRefused is the sprint's own refused-verb record as the doctor reads it.
func readRefused(t *testing.T, st *store.Store) []sprint.RefusedVerb {
	t.Helper()
	dr, err := st.DoctorRead(context.Background())
	require.NoError(t, err)
	return dr.Refused
}

// TestAddAndBriefLintRefusalsAreRecorded: an add whose card brief draws the brief lints at
// its base (paths-cover-named and its kind) refuses and writes one refused-verb record
// naming the verb, the card id and the rule tokens it drew; an accepted add records
// nothing.
func TestAddAndBriefLintRefusalsAreRecorded(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)

	// a refused add brief: REPO and BASE are named, so the brief lints at the base run and
	// refuse; the base is never reached in a unit test, so no git and no network, and the
	// refusal records the rule tokens it drew
	var stderr bytes.Buffer
	brief := "RESULT: c sha=0123456789ab tier: pro\nREPO: owner/repo\nBASE: sprint/s\nPATHS: internal/a.go\nSTART: internal/a.go\nTEST: internal/x TestY\n\nTHE TASK. Do the work."
	code := ta.a.holdBriefBase("add", st, false, &stderr, briefCheck{id: "c", brief: brief})
	require.Equal(t, 2, code, stderr.String())
	recs := readRefused(t, st)
	require.Len(t, recs, 1, "the refusal records one verb: %v", recs)
	r := recs[0]
	assert.Equal(t, "add", r.Verb, "the record names the verb")
	assert.Equal(t, "c", r.Card, "the record names the card id")
	assert.Contains(t, r.Why, "paths-cover-named", "the record carries the rule tokens it drew: %s", r.Why)
	assert.Contains(t, r.Why, "token=", "the record's Why names the rule tokens: %s", r.Why)

	// a card lint refusal at add records it too
	stderr.Reset()
	cardBrief := "RESULT: c sha=0123456789ab tier: pro\nPATHS: internal/x/*.go\nTEST: internal/other TestY\n\nTHE TASK. Fix x."
	code = ta.a.holdCardChecks("add", st, &stderr, briefCheck{id: "c", brief: cardBrief})
	require.Equal(t, 2, code, stderr.String())
	recs = readRefused(t, st)
	require.Len(t, recs, 2, "the card lint refusal records a second verb: %v", recs)
	assert.Equal(t, "add", recs[1].Verb, "the card lint record names the verb")
	assert.Contains(t, recs[1].Why, "test-outside-paths", "the card lint record carries its rule token: %s", recs[1].Why)

	// an accepted add records nothing
	stderr.Reset()
	code = ta.a.holdBriefBase("add", st, false, &stderr, briefCheck{brief: "\nTHE TASK. Read the module.\n"})
	require.Equal(t, 0, code, stderr.String())
	recs = readRefused(t, st)
	require.Len(t, recs, 2, "an accepted add records nothing new: %v", recs)
}
