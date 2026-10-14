package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The doctor names the cause the member-down stop does not: a member whose beats the server
// refused. These pin the two refused-verb checks' thresholds, their window and their line
// shapes (docs/SPEC-SPRINT.md, "The doctor's refused-verb and beat-form checks").

func refusedAt(now time.Time, ago time.Duration, verb, member, argv, why string) RefusedVerb {
	return RefusedVerb{Epoch: 3, At: now.Add(-ago), Verb: verb, Member: member, Argv: argv, Why: why}
}

// A member with N >= 3 refused `fleet beat` verbs in the last 10 minutes is one member-beat
// FAIL naming the count, the last refusal and what it sent; one with fewer, or none, is no
// fault.
func TestRefusedBeatsNameTheMemberAtThree(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	recs := []RefusedVerb{
		refusedAt(now, time.Minute, RefusedBeatVerb, "batman", "fleet beat batman", "a beat wants --load"),
		refusedAt(now, 2*time.Minute, RefusedBeatVerb, "batman", "fleet beat batman", "a beat wants --load"),
		refusedAt(now, 3*time.Minute, RefusedBeatVerb, "batman", "fleet beat batman", "a beat wants --load"),
		refusedAt(now, time.Minute, RefusedBeatVerb, "superman", "fleet beat superman", "a beat wants --load"),
		refusedAt(now, 20*time.Minute, RefusedBeatVerb, "old", "fleet beat old", "outside the window"),
	}
	got := RefusedBeatLines(recs, now)
	require.Len(t, got, 1, "member-beat lines")
	want := "member-beat batman: 3 beats refused in 10m0s, last: a beat wants --load, argv: fleet beat batman remedy="
	assert.Equal(t, want, got[0][:len(want)], "member-beat line")
	assert.Len(t, RefusedBeats(recs, now)["superman"], 1, "one refusal is under the bound")
}

// The refused-verbs check groups every verb, not only beat: N >= 10 of one verb word in the
// window is one FAIL, and a pair under the bound is no line.
func TestRefusedVerbLinesGroupEveryVerbAtTen(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	var recs []RefusedVerb
	for i := range 10 {
		recs = append(recs, refusedAt(now, time.Duration(i)*time.Second, "queue", "m1", "queue --as m1", "the store refused it"))
	}
	for i := range 9 {
		recs = append(recs, refusedAt(now, time.Duration(i)*time.Second, "take", "m2", "take --as m2", "no card"))
	}
	got := RefusedVerbLines(recs, now)
	require.Len(t, got, 1, "refused-verbs lines")
	assert.Equal(t, "refused-verbs m1: queue x 10 in 10m0s, last: the store refused it", got[0])
}

// The window is exactly RefusedWindow: a record a window old is out, one just inside is in.
func TestRefusedWindowIsExactlyTenMinutes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	recs := []RefusedVerb{
		refusedAt(now, RefusedWindow, "queue", "m1", "queue", "old edge"),
		refusedAt(now, RefusedWindow-time.Second, "queue", "m1", "queue", "in window"),
	}
	got := RefusedIn(recs, now, RefusedWindow)
	require.Len(t, got, 1, "window")
	assert.Equal(t, "in window", got[0].Why)
}

// The record is keyed by epoch and bounded by the window: PruneRefused keeps the recent
// window and drops the rest, so the server's record stays bounded as it appends.
func TestRefusedRecordPrunesToTheWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	rec := RefusedRecord{Epoch: 7, Verbs: []RefusedVerb{
		refusedAt(now, time.Minute, RefusedBeatVerb, "batman", "fleet beat batman", "kept"),
		refusedAt(now, RefusedWindow+time.Second, RefusedBeatVerb, "batman", "fleet beat batman", "dropped"),
	}}
	got := PruneRefused(rec.Verbs, now)
	require.Len(t, got, 1, "pruned record")
	assert.Equal(t, "kept", got[0].Why)
}
