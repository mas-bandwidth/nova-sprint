package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// TestAuditNamesEveryBlockedStuckOrLoopedCard is the red-first test of the audit
// verb on a private store: it seeds one card of each trouble the seat's two hand
// tools looked for, asserts each flag on the right line and none on the healthy
// one, in the text and in --json, and reads the footer's counts.
func TestAuditNamesEveryBlockedStuckOrLoopedCard(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1,m2")

	now := ta.now
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }
	// a primary card in the work table, with a kind, stream, attempt and brief.
	primIn := func(id, col string, attempt int, admitted time.Duration, needs string) ntable.BatchMemberEntry {
		set := map[string]string{
			"kind": "primary", "stream": "s1",
			"attempt": itoa(attempt), "admitted": at(admitted),
			"brief": id + ": a card (s1)\n\nBody.",
		}
		if needs != "" {
			set["needs"] = needs
		}
		return ntable.BatchMemberEntry{ID: id, Expect: &ntable.MemberExpect{Absent: true},
			Create: &ntable.MemberCreateOp{Row: "s1", Col: col, Score: 1}, Set: set}
	}
	prim := func(id string, attempt int, admitted time.Duration, needs string) ntable.BatchMemberEntry {
		return primIn(id, sprint.Ready, attempt, admitted, needs)
	}
	// a work card of the primary's attempt, placed on m1 with an end.
	work := func(primary string, attempt int, end string, member, col string) ntable.BatchMemberEntry {
		return ntable.BatchMemberEntry{ID: sprint.WorkCardID(primary, attempt), Expect: &ntable.MemberExpect{Absent: true},
			Create: &ntable.MemberCreateOp{Row: member, Col: col, Score: 1},
			Set:    map[string]string{"kind": "work", "primary": primary, "stream": "s1", "attempt": itoa(attempt), "member": member, "end": end}}
	}
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	res, err := st.Run(context.Background(), store.Step{Verb: "seed audit cards", Load: []string{sprint.Work, sprint.Fleet},
		Plan: func(*sprint.Snapshot) sprint.Plan {
			return sprint.Plan{
				Rows: []sprint.RowAdd{{Table: sprint.Work, Row: "s1"}, {Table: sprint.Fleet, Row: "m1"}, {Table: sprint.Fleet, Row: "m2"}},
				Units: []sprint.Unit{
					// no holder: ready past the dealt bound (the default is six hours)
					{Key: "no-holder", Stream: "s1", Changes: []sprint.Change{{Table: sprint.Work, Entry: prim("no-holder", 1, -7*time.Hour, "")}}},
					// attempt 4
					{Key: "at-4", Stream: "s1", Changes: []sprint.Change{{Table: sprint.Work, Entry: prim("at-4", 4, -time.Minute, "")}}},
					// six failed ends
					{Key: "fails", Stream: "s1", Changes: []sprint.Change{
						{Table: sprint.Work, Entry: prim("fails", 6, -time.Minute, "")},
						{Table: sprint.Fleet, Entry: work("fails", 1, "failed", "m1", sprint.DoneFailed)},
						{Table: sprint.Fleet, Entry: work("fails", 2, "failed", "m1", sprint.DoneFailed)},
						{Table: sprint.Fleet, Entry: work("fails", 3, "failed", "m1", sprint.DoneFailed)},
						{Table: sprint.Fleet, Entry: work("fails", 4, "failed", "m1", sprint.DoneFailed)},
						{Table: sprint.Fleet, Entry: work("fails", 5, "idle-reaped", "m1", sprint.DoneFailed)},
						{Table: sprint.Fleet, Entry: work("fails", 6, "bound", "m1", sprint.DoneFailed)},
					}},
					// stuck: unchanged past --stuck 1m
					{Key: "stuck", Stream: "s1", Changes: []sprint.Change{{Table: sprint.Work, Entry: prim("stuck", 1, -5*time.Minute, "")}}},
					// needs a card that is no card: created waiting, so the needs rule admits it
					{Key: "needs-gone", Stream: "s1", Changes: []sprint.Change{{Table: sprint.Work, Entry: primIn("needs-gone", sprint.Waiting, 1, -time.Minute, "s1-gone")}}},
					// healthy: ready, recently admitted, held by m1's working card
					{Key: "healthy", Stream: "s1", Changes: []sprint.Change{
						{Table: sprint.Work, Entry: prim("healthy", 1, -time.Second, "")},
						{Table: sprint.Fleet, Entry: work("healthy", 1, "", "m1", sprint.Working)},
					}},
				},
			}
		}})
	require.NoError(t, err)
	require.Empty(t, res.Refused, "%+v", res.Refused)

	out := ta.ok("audit --stuck 1m")
	line := func(id string) string {
		t.Helper()
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, id+" ") {
				return l
			}
		}
		require.Fail(t, "no audit line for "+id, out)
		return ""
	}
	assert.Contains(t, line("no-holder"), "NO-HOLDER", out)
	assert.NotContains(t, line("no-holder"), "ATT>=4")
	assert.Contains(t, line("at-4"), "ATT>=4", out)
	assert.NotContains(t, line("at-4"), "NO-HOLDER")
	assert.Contains(t, line("fails"), "FAILS=6", out)
	assert.NotContains(t, line("fails"), "STUCK")
	assert.Contains(t, line("stuck"), "STUCK", out)
	assert.NotContains(t, line("stuck"), "NO-HOLDER")
	assert.Contains(t, line("needs-gone"), "NEEDS-GONE", out)
	assert.Contains(t, line("needs-gone"), "s1-gone:-")
	assert.Contains(t, line("healthy"), "holder=m1", "the holder is the member whose working card holds it")
	assert.Contains(t, line("healthy"), "flags=-", "a healthy card carries no flag")
	assert.Contains(t, out, "AUDIT OK cards=6 flagged=5\n", out)

	// --json: one object per card, the same flags
	var rows []sprint.AuditRow
	for _, l := range strings.Split(ta.ok("audit --stuck 1m --json"), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var r sprint.AuditRow
		require.NoError(t, json.Unmarshal([]byte(l), &r), l)
		rows = append(rows, r)
	}
	require.Len(t, rows, 6)
	byID := map[string][]string{}
	for _, r := range rows {
		byID[r.ID] = r.Flags
	}
	assert.Contains(t, byID["no-holder"], "NO-HOLDER")
	assert.Contains(t, byID["at-4"], "ATT>=4")
	assert.Contains(t, byID["fails"], "FAILS=6")
	assert.Contains(t, byID["stuck"], "STUCK")
	assert.Contains(t, byID["needs-gone"], "NEEDS-GONE")
	assert.Empty(t, byID["healthy"], "the healthy card is unflagged in --json too")
}
