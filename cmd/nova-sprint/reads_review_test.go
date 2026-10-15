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

// TestReadsReviewShowsWhoHoldsEachReviewRead is the red-first test of the reads
// --review verb on a private store: it puts three primaries in review, one with
// a read reading for a member, one whose read was withdrawn by a hold and
// another that ended with no verdict, and one with no read at all; it asserts
// the holder and durations on the first, both outcomes on the second, and
// NO-READ-OUT on the third, and the same in --json.
func TestReadsReviewShowsWhoHoldsEachReviewRead(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.inReview(3)

	now := ta.now
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }
	// a read card placed on the readers table, in the col given.
	placed := func(primary, reader, col string, begun time.Duration) ntable.BatchMemberEntry {
		set := map[string]string{"kind": "read", "primary": primary, "attempt": "1", "reader": reader, "asked": at(-2 * time.Minute)}
		if begun != 0 {
			set["begun"] = at(begun)
		}
		return ntable.BatchMemberEntry{ID: sprint.ReadCardID(primary, 1, reader), Expect: &ntable.MemberExpect{Absent: true},
			Create: &ntable.MemberCreateOp{Row: reader, Col: col, Score: 1}, Set: set}
	}
	// a read card retired off the readers table, kept with its outcome.
	retire := func(primary, reader, by, reason string) ntable.BatchMemberEntry {
		set := map[string]string{"retired": at(-time.Minute), "retired_by": by}
		if reason != "" {
			set["reason"] = reason
		}
		return ntable.BatchMemberEntry{ID: sprint.ReadCardID(primary, 1, reader),
			Expect: &ntable.MemberExpect{Place: &ntable.PlaceExpect{Row: reader, Col: sprint.Asked}},
			Remove: true, Set: set}
	}

	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	// the reads, placed first (a batch cannot create and retire one record)
	_, err = st.Run(context.Background(), store.Step{Verb: "seed review reads", Load: []string{sprint.Work, sprint.Fleet, sprint.Readers},
		Plan: func(*sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Units: []sprint.Unit{
				{Key: "s1-1", Stream: "s1", Changes: []sprint.Change{
					{Table: sprint.Readers, Entry: placed("s1-1", "reader-a", sprint.Reading, -time.Minute)},
				}},
				{Key: "s1-2", Stream: "s1", Changes: []sprint.Change{
					{Table: sprint.Readers, Entry: placed("s1-2", "reader-a", sprint.Asked, 0)},
					{Table: sprint.Readers, Entry: placed("s1-2", "reader-b", sprint.Asked, 0)},
				}},
			}}
		}})
	require.NoError(t, err)
	// s1-2's reads retired: one withdrawn by a hold, one handed back with no verdict
	_, err = st.Run(context.Background(), store.Step{Verb: "retire s1-2 reads", Load: []string{sprint.Work, sprint.Fleet, sprint.Readers},
		Plan: func(*sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Units: []sprint.Unit{
				{Key: "s1-2", Stream: "s1", Changes: []sprint.Change{
					{Table: sprint.Readers, Entry: retire("s1-2", "reader-a", sprint.RetiredByHold, "")},
					{Table: sprint.Readers, Entry: retire("s1-2", "reader-b", sprint.RetiredByReturned, "no verdict (ran=false verdict=\"\")")},
				}},
			}}
		}})
	require.NoError(t, err)

	out := ta.ok("reads --review")
	line := func(id string) string {
		t.Helper()
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, id+" ") {
				return l
			}
		}
		require.Fail(t, "no review line for "+id, out)
		return ""
	}
	readLine := func(primary, reader string) string {
		t.Helper()
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, sprint.ReadCardID(primary, 1, reader)+" ") {
				return l
			}
		}
		require.Fail(t, "no read line for "+primary+" "+reader, out)
		return ""
	}
	// the first: the holder (reader-a) and its duration while it reads
	l := readLine("s1-1", "reader-a")
	assert.Contains(t, l, "reader=reader-a")
	assert.Contains(t, l, "state=reading")
	assert.Contains(t, l, "for=1m0s", out)
	assert.Contains(t, l, "outcome=-", out)
	// the second: both outcomes, the withdrawn and the no-verdict
	l = readLine("s1-2", "reader-a")
	assert.Contains(t, l, "state=withdrawn", out)
	assert.Contains(t, l, "outcome=hold", out)
	l = readLine("s1-2", "reader-b")
	assert.Contains(t, l, "state=returned", out)
	assert.Contains(t, l, "no verdict (ran=false", out)
	// the third: no read out, no judgment
	assert.Contains(t, line("s1-3"), "NO-READ-OUT", out)
	assert.NotContains(t, line("s1-1"), "NO-READ-OUT", out)
	assert.NotContains(t, line("s1-2"), "NO-READ-OUT", out)
	assert.Contains(t, out, "READS OK review=3 no-read-out=1\n", out)

	// --json: one object per primary, the same facts
	var rows []ReviewRow
	for _, l := range strings.Split(ta.ok("reads --review --json"), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var r ReviewRow
		require.NoError(t, json.Unmarshal([]byte(l), &r), l)
		rows = append(rows, r)
	}
	require.Len(t, rows, 3)
	byID := map[string]ReviewRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	require.Len(t, byID["s1-1"].Reads, 1)
	assert.Equal(t, "reader-a", byID["s1-1"].Reads[0].Reader)
	assert.Equal(t, "reading", byID["s1-1"].Reads[0].State)
	assert.Equal(t, "1m0s", byID["s1-1"].Reads[0].For)
	require.Len(t, byID["s1-2"].Reads, 2)
	states := map[string]string{}
	outcomes := map[string]string{}
	for _, r := range byID["s1-2"].Reads {
		states[r.Reader] = r.State
		outcomes[r.Reader] = r.Outcome
	}
	assert.Equal(t, "withdrawn", states["reader-a"])
	assert.Equal(t, "returned", states["reader-b"])
	assert.Equal(t, "hold", outcomes["reader-a"])
	assert.Contains(t, outcomes["reader-b"], "no verdict (ran=false")
	assert.True(t, byID["s1-3"].NoReadOut, "s1-3 has no read out")
	assert.False(t, byID["s1-1"].NoReadOut)
	assert.False(t, byID["s1-2"].NoReadOut)
}
