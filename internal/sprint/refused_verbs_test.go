package sprint

import (
	"testing"
	"time"
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
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	recs := []RefusedVerb{
		refusedAt(now, time.Minute, RefusedBeatVerb, "batman", "fleet beat batman", "a beat wants --load"),
		refusedAt(now, 2*time.Minute, RefusedBeatVerb, "batman", "fleet beat batman", "a beat wants --load"),
		refusedAt(now, 3*time.Minute, RefusedBeatVerb, "batman", "fleet beat batman", "a beat wants --load"),
		refusedAt(now, time.Minute, RefusedBeatVerb, "superman", "fleet beat superman", "a beat wants --load"),
		refusedAt(now, 20*time.Minute, RefusedBeatVerb, "old", "fleet beat old", "outside the window"),
	}
	got := RefusedBeatLines(recs, now)
	if len(got) != 1 {
		t.Fatalf("member-beat lines = %v", got)
	}
	want := "member-beat batman: 3 beats refused in 10m0s, last: a beat wants --load, argv: fleet beat batman remedy="
	if got[0][:len(want)] != want {
		t.Fatalf("member-beat line = %q", got[0])
	}
	if len(RefusedBeats(recs, now)["superman"]) != 1 {
		t.Fatalf("one refusal is under the bound")
	}
}

// The refused-verbs check groups every verb, not only beat: N >= 10 of one verb word in the
// window is one FAIL, and a pair under the bound is no line.
func TestRefusedVerbLinesGroupEveryVerbAtTen(t *testing.T) {
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	var recs []RefusedVerb
	for i := range 10 {
		recs = append(recs, refusedAt(now, time.Duration(i)*time.Second, "queue", "m1", "queue --as m1", "the store refused it"))
	}
	for i := range 9 {
		recs = append(recs, refusedAt(now, time.Duration(i)*time.Second, "take", "m2", "take --as m2", "no card"))
	}
	got := RefusedVerbLines(recs, now)
	if len(got) != 1 {
		t.Fatalf("refused-verbs lines = %v", got)
	}
	want := "refused-verbs m1: queue x 10 in 10m0s, last: the store refused it"
	if got[0] != want {
		t.Fatalf("refused-verbs line = %q, want %q", got[0], want)
	}
}

// The window is exactly RefusedWindow: a record a window old is out, one just inside is in.
func TestRefusedWindowIsExactlyTenMinutes(t *testing.T) {
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	recs := []RefusedVerb{
		refusedAt(now, RefusedWindow, "queue", "m1", "queue", "old edge"),
		refusedAt(now, RefusedWindow-time.Second, "queue", "m1", "queue", "in window"),
	}
	if got := RefusedIn(recs, now, RefusedWindow); len(got) != 1 || got[0].Why != "in window" {
		t.Fatalf("window = %v", got)
	}
}

// The record is keyed by epoch and bounded by the window: PruneRefused keeps the recent
// window and drops the rest, so the server's record stays bounded as it appends.
func TestRefusedRecordPrunesToTheWindow(t *testing.T) {
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	rec := RefusedRecord{Epoch: 7, Verbs: []RefusedVerb{
		refusedAt(now, time.Minute, RefusedBeatVerb, "batman", "fleet beat batman", "kept"),
		refusedAt(now, RefusedWindow+time.Second, RefusedBeatVerb, "batman", "fleet beat batman", "dropped"),
	}}
	got := PruneRefused(rec.Verbs, now)
	if len(got) != 1 || got[0].Why != "kept" {
		t.Fatalf("pruned record = %v", got)
	}
}
