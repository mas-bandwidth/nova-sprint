package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

func seedDrained(ta *testApp, cards []ntable.BatchMemberEntry) {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "migration"})
	require.NoError(ta.t, err)
	var qc []sprint.QueuedChange
	for i := range cards {
		qc = append(qc, sprint.QueuedChange{Entry: &cards[i], Verb: "seed", Actor: "tester"})
	}
	res, err := st.Run(context.Background(), store.Step{Verb: sprint.DrainVerb, Load: []string{sprint.Work},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Drain(s, qc, sprint.MachineActor)
		}})
	require.NoError(ta.t, err)
	require.Empty(ta.t, res.Refused)
}

func TestStartRefusesOnAStaleHeadUntilItIsReworked(t *testing.T) {
	t.Parallel()
	const head = "0123456789abcdef0123456789abcdef01234567"
	ta := gateOn(t)

	// Clean streams and baseline
	ta.seedClean()

	// Seed a card in review holding an attempt and head predating its brief
	seedDrained(ta, []ntable.BatchMemberEntry{
		seedCard("stale", "s1", sprint.Review, map[string]string{
			"brief":                  lintedBrief("stale", "sprint/s1", "internal/c.go"),
			"attempt":                "1",
			sprint.FieldBriefAttempt: "1",
			"head":                   head,
		}),
	})

	// check cards reports exactly one stale-head finding naming it and exits 1
	code, out, _ := ta.do("check cards")
	require.Equal(t, 1, code, out)
	assert.Equal(t, []string{"stale-head stale"}, checkLines(out))
	assert.Contains(t, out, "CHECK stale-head stale head "+head+" of attempt 1 was made under an earlier brief remedy=nova-sprint rework stale --fix 'brief replaced'")

	// start refuses naming the kind
	code, _, errs := ta.do("start")
	require.Equal(t, 1, code, errs)
	assert.Contains(t, errs, "the last check found stale-head:1")
	assert.False(t, ta.machineRunning())

	// after rework <card> --fix 'brief replaced' the check is clean
	ta.ok("rework stale --fix 'brief replaced'")
	code, out, errs = ta.do("check cards")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "CHECK CARDS OK")
	assert.NotContains(t, out, "stale-head")

	// a card re-briefed with --keep-head is not a finding
	seedDrained(ta, []ntable.BatchMemberEntry{
		seedCard("kept", "s1", sprint.Review, map[string]string{
			"brief":                  lintedBrief("kept", "sprint/s1", "internal/x/one.go"),
			"attempt":                "1",
			sprint.FieldBriefAttempt: "1",
			"brief_keep_head":        "true",
			"head":                   head,
		}),
	})
	code, out, errs = ta.do("check cards")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.NotContains(t, out, "stale-head")

	// a card never attempted is not a finding
	ta.seed(nil, nil, []ntable.BatchMemberEntry{
		seedCard("never", "s1", sprint.Ready, map[string]string{
			"brief": lintedBrief("never", "sprint/s1", "internal/x/two.go"),
		}),
	})
	code, out, errs = ta.do("check cards")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.NotContains(t, out, "stale-head")
}
