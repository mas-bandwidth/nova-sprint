package sprint

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRelease is the release check's snapshot as a test builds it: a clock, a log, and tables.
type fakeRelease struct {
	now      time.Time
	lines    []Line
	dealtMax time.Duration
	snap     *Snapshot
}

func (f fakeRelease) Now() time.Time          { return f.now }
func (f fakeRelease) Log() []Line             { return f.lines }
func (f fakeRelease) DealtMax() time.Duration { return f.dealtMax }
func (f fakeRelease) Snapshot() *Snapshot     { return f.snap }

func fleetMove(at time.Time, card, from, to string, set map[string]string) Line {
	return Line{Kind: LineMove, At: at, Table: Fleet, Card: card, Stream: "s1", From: from, To: to, Set: set}
}

// hr is the clock t0 plus h hours.
func hr(h float64) time.Time { return t0.Add(time.Duration(h * float64(time.Hour))) }

func relFacts(now time.Time, lines ...Line) fakeRelease {
	return fakeRelease{now: now, lines: lines, dealtMax: DealtMaxDefault}
}

func TestReleaseCheckFailsWhileAFriendWasStuckInTheLastFourHours(t *testing.T) {
	t.Parallel()
	amy := FriendRow("amy")

	t.Run("a friend's working card past its deadline fails, naming her and the time", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(3), fleetMove(hr(0), "s1-1.w1", "", amy+":working", map[string]string{"first_taken": stamp(hr(0))}))
		r := NoStuckFriend(f)
		assert.False(t, r.OK)
		assert.Equal(t, CheckNoStuckFriend, r.Name)
		assert.Contains(t, r.Evidence, "friend amy")
		assert.Contains(t, r.Evidence, "s1-1.w1")
		assert.Contains(t, r.Evidence, stamp(hr(2)))
	})

	t.Run("a spell ended before the window opened passes", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(8),
			fleetMove(hr(0), "s1-1.w1", "", amy+":working", nil),
			fleetMove(hr(3), "s1-1.w1", amy+":working", amy+":done_ok", nil))
		r := NoStuckFriend(f)
		assert.True(t, r.OK, r.Evidence)
		assert.Equal(t, "RELEASE CHECK no-stuck-friend ok "+r.Evidence, r.Line())
	})

	t.Run("a spell that ended inside the window fails: stuck at any moment", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(6),
			fleetMove(hr(0), "s1-1.w1", "", amy+":working", nil),
			fleetMove(hr(3), "s1-1.w1", amy+":working", amy+":done_ok", nil))
		r := NoStuckFriend(f)
		assert.False(t, r.OK)
		assert.Contains(t, r.Evidence, stamp(hr(2)), "the spell began at hour 2, inside the window opened at hour 2")
	})

	t.Run("a card finished inside its deadline is never stuck", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(3),
			fleetMove(hr(0), "s1-1.w1", "", amy+":working", nil),
			fleetMove(hr(1), "s1-1.w1", amy+":working", amy+":done_ok", nil))
		assert.True(t, NoStuckFriend(f).OK)
	})

	t.Run("her own longer deadline holds", func(t *testing.T) {
		t.Parallel()
		set := map[string]string{"friend_deadline": strconv.Itoa(5 * 3600)}
		f := relFacts(hr(4), fleetMove(hr(0), "s1-1.w1", "", amy+":working", set))
		assert.True(t, NoStuckFriend(f).OK, "late only from hour 5")
		f.now = hr(6)
		assert.False(t, NoStuckFriend(f).OK)
	})

	t.Run("a card dealt and never taken past the dealt bound fails", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(7), fleetMove(hr(0), "s1-2.w1", "", amy+":ready", nil))
		r := NoStuckFriend(f)
		assert.False(t, r.OK)
		assert.Contains(t, r.Evidence, "dealt, never taken")
		assert.Contains(t, r.Evidence, stamp(hr(6)), "dealt at 0 and the dealt bound is 6h")
	})

	t.Run("a card taken back from her stops the clock", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(9),
			fleetMove(hr(0), "s1-2.w1", "", amy+":ready", nil),
			fleetMove(hr(1), "s1-2.w1", amy+":ready", "m1:ready", nil))
		assert.True(t, NoStuckFriend(f).OK, "it left her row")
	})

	t.Run("a card taken back inside its deadline resets to untaken bound and does not fail", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(3),
			fleetMove(hr(0), "s1-1.w1", "", amy+":working", map[string]string{"first_taken": stamp(hr(0))}),
			fleetMove(hr(1), "s1-1.w1", amy+":working", amy+":withdrawn", map[string]string{FieldTakenBack: "taken back", "untaken_since": stamp(hr(1))}),
		)
		r := NoStuckFriend(f)
		assert.True(t, r.OK, r.Evidence)
		assert.Equal(t, "RELEASE CHECK no-stuck-friend ok "+r.Evidence, r.Line())

		f.now = hr(8)
		r8 := NoStuckFriend(f)
		assert.False(t, r8.OK)
		assert.Contains(t, r8.Evidence, "dealt, never taken")
		assert.Contains(t, r8.Evidence, stamp(hr(7)))
	})

	t.Run("a card taken back after its deadline was stuck and fails inside the window", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(4),
			fleetMove(hr(0), "s1-1.w1", "", amy+":working", map[string]string{"first_taken": stamp(hr(0))}),
			fleetMove(hr(3), "s1-1.w1", amy+":working", amy+":withdrawn", map[string]string{FieldTakenBack: "taken back", "untaken_since": stamp(hr(3))}),
		)
		r := NoStuckFriend(f)
		assert.False(t, r.OK)
		assert.Contains(t, r.Evidence, "friend amy")
		assert.Contains(t, r.Evidence, stamp(hr(2)))
	})

	t.Run("a card whose friend deadline is increased historically does not fail", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(4),
			fleetMove(hr(0), "s1-1.w1", "", amy+":working", map[string]string{"first_taken": stamp(hr(0))}),
			fleetMove(hr(1), "s1-1.w1", amy+":working", amy+":working", map[string]string{FieldFriendDeadline: strconv.Itoa(5 * 3600)}),
		)
		r := NoStuckFriend(f)
		assert.True(t, r.OK, r.Evidence)

		f.now = hr(6)
		r6 := NoStuckFriend(f)
		assert.False(t, r6.OK)
		assert.Contains(t, r6.Evidence, stamp(hr(5)))
	})

	t.Run("a card stuck before a clear inside the window fails", func(t *testing.T) {
		t.Parallel()
		clearLine := Line{Kind: LineMove, At: hr(2.5), Verb: "clear"}
		f := relFacts(hr(3),
			fleetMove(hr(0), "s1-1.w1", "", amy+":working", map[string]string{"first_taken": stamp(hr(0))}),
			clearLine,
		)
		r := NoStuckFriend(f)
		assert.False(t, r.OK)
		assert.Contains(t, r.Evidence, "friend amy")
		assert.Contains(t, r.Evidence, stamp(hr(2)))
	})

	t.Run("a card not yet stuck before a clear does not fail", func(t *testing.T) {
		t.Parallel()
		clearLine := Line{Kind: LineMove, At: hr(2.5), Verb: "clear"}
		f := relFacts(hr(3),
			fleetMove(hr(2.2), "s1-1.w1", "", amy+":working", map[string]string{"first_taken": stamp(hr(2.2))}),
			clearLine,
		)
		r := NoStuckFriend(f)
		assert.True(t, r.OK, r.Evidence)
	})

	t.Run("a machine's late card is not a friend's", func(t *testing.T) {
		t.Parallel()
		f := relFacts(hr(9), fleetMove(hr(0), "s1-1.w1", "", "m1:working", nil))
		assert.True(t, NoStuckFriend(f).OK)
	})

	t.Run("no log is no stuck friend", func(t *testing.T) {
		t.Parallel()
		assert.True(t, NoStuckFriend(relFacts(hr(9))).OK)
	})

	t.Run("consecutive late periods for a card merge across intervening moves of other cards", func(t *testing.T) {
		t.Parallel()
		bob := FriendRow("bob")
		f := relFacts(hr(4),
			fleetMove(hr(0), "s1-1.w1", "", amy+":working", map[string]string{"first_taken": stamp(hr(0))}),
			fleetMove(hr(1), "s1-2.w1", "", bob+":working", map[string]string{"first_taken": stamp(hr(1))}),
			fleetMove(hr(2.5), "s1-1.w1", amy+":working", amy+":working", map[string]string{"first_taken": stamp(hr(0)), "note": "update"}),
			fleetMove(hr(3.5), "s1-2.w1", bob+":working", bob+":working", map[string]string{"first_taken": stamp(hr(1)), "note": "update"}),
		)
		r := NoStuckFriend(f)
		assert.False(t, r.OK)
		spans, _ := friendLateSpans(f.Log(), f.Now(), f.DealtMax())
		assert.Len(t, spans, 2, "one merged span per card, not fragmented")
	})
}

func TestTheReleaseReportSaysOKOrNotReadyByTheChecksRun(t *testing.T) {
	t.Parallel()
	good := relFacts(hr(1))
	rep, err := RunReleaseChecks(good, []string{CheckNoStuckFriend})
	require.NoError(t, err)
	assert.Equal(t, "RELEASE OK checks=1", rep.Summary)
	assert.Equal(t, 0, rep.ExitCode())
	assert.Equal(t, 1, len(rep.Results))

	bad := relFacts(hr(3), fleetMove(hr(0), "s1-1.w1", "", FriendRow("amy")+":working", nil))
	rep, err = RunReleaseChecks(bad, []string{CheckNoStuckFriend})
	require.NoError(t, err)
	assert.Equal(t, "RELEASE NOT READY failed=1", rep.Summary)
	assert.Equal(t, 1, rep.ExitCode())
	assert.False(t, rep.Ready)

	_, err = RunReleaseChecks(good, []string{"no-such-check"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-check")
	assert.Contains(t, err.Error(), CheckNoStuckFriend, "the refusal names the checks there are")
}

func TestEveryReleaseCheckStatesItsBar(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, c := range ReleaseChecks {
		assert.NotEmpty(t, c.Name)
		assert.NotEmpty(t, c.Bar, c.Name)
		assert.False(t, seen[c.Name], "%s twice", c.Name)
		seen[c.Name] = true
	}
}

func TestReleaseStreamsFilterKeepsTheStreamsTheGlobNames(t *testing.T) {
	t.Parallel()
	a := fleetMove(hr(0), "a-1.w1", "", FriendRow("amy")+":working", nil)
	a.Stream = "alpha"
	b := fleetMove(hr(0), "b-1.w1", "", FriendRow("bob")+":working", nil)
	b.Stream = "beta"
	got, err := ReleaseStreamLines([]Line{a, b}, "al*")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "a-1.w1", got[0].Card)
	all, err := ReleaseStreamLines([]Line{a, b}, "")
	require.NoError(t, err)
	assert.Len(t, all, 2)
	_, err = ReleaseStreamLines(nil, "[")
	require.Error(t, err)
}

// TestReleaseCheckColdAuditSamplesTwentyLandedCardsAndNeedsEveryReadOk tests:
//  1. PlanColdAudit samples exactly 20 cards landed since the last release with a seed.
//  2. The sample is reproducible when redrawn with the same seed.
//  3. Readers who never saw each card are selected.
//  4. Cold audit check fails when reads are unanswered.
//  5. Cold audit check fails naming broken reads and their findings.
//  6. Cold audit check passes with OK only when all 20 reads are ok and under 48h old.
//  7. Cold audit check fails when the audit is over 48 hours old.
func TestReleaseCheckColdAuditSamplesTwentyLandedCardsAndNeedsEveryReadOk(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	lastReleaseTime := now.Add(-24 * time.Hour) // release was 24 hours ago

	// Create snapshot with work table, readers table, fleet
	snap := &Snapshot{
		Now:     now,
		Work:    NewTable(Work),
		Readers: NewTable(Readers),
		Fleet:   NewTable(Fleet),
	}
	snap.Work.SetRows([]string{"s1"})

	// Add 3 readers to Readers table
	readers := []string{"reader-a", "reader-b", "reader-c"}
	snap.Readers.SetRows(readers)
	snap.ReaderStates = map[string]string{
		"reader-a": ReaderUp,
		"reader-b": ReaderUp,
		"reader-c": ReaderUp,
	}

	// Add 5 old landed cards before the release (should not be sampled)
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("old-%d", i)
		snap.Work.Put(&Card{
			ID:     id,
			Row:    "s1",
			Col:    Landed,
			Score:  float64(i),
			Fields: map[string]string{"landed": stamp(lastReleaseTime.Add(-time.Duration(i) * time.Hour))},
		})
	}

	// Add 30 landed cards after the release
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("card-%02d", i)
		snap.Work.Put(&Card{
			ID:    id,
			Row:   "s1",
			Col:   Landed,
			Score: float64(10 + i),
			Fields: map[string]string{
				"landed":      stamp(lastReleaseTime.Add(time.Duration(i) * time.Minute)),
				"landed_head": fmt.Sprintf("sha%02d", i),
				"base":        "main",
				"asked":       "reader-a", // reader-a saw the card during normal review!
			},
		})
		// Record that reader-a had a read card during review
		snap.Readers.Put(&Card{
			ID:     ReadCardID(id, 1, "reader-a"),
			Row:    "reader-a",
			Col:    OK,
			Score:  float64(10 + i),
			Fields: map[string]string{"primary": id, "attempt": "1"},
		})
	}

	// 1. PlanColdAudit with seed 42
	seed := int64(42)
	req := ColdAuditReq{
		Seed:      seed,
		SinceTag:  "v1.0.0",
		SinceTime: lastReleaseTime,
		Who:       "coordinator",
	}

	plan, rec, err := PlanColdAudit(snap, req)
	require.NoError(t, err)

	// Exactly 20 cards sampled
	assert.Len(t, rec.IDs, 20, "audit samples exactly 20 cards")
	assert.Equal(t, seed, rec.Seed)
	assert.Equal(t, now, rec.Asked)

	// None of the old cards before lastReleaseTime was sampled
	for _, id := range rec.IDs {
		assert.False(t, strings.HasPrefix(id, "old-"), "must not sample cards before the release: %s", id)
	}

	// 2. Redraw with the same seed: identical sample
	_, rec2, err2 := PlanColdAudit(snap, req)
	require.NoError(t, err2)
	assert.Equal(t, rec.IDs, rec2.IDs, "same seed produces identical sample")

	// Redraw with different seed: different sample
	reqDiff := req
	reqDiff.Seed = 9999
	_, recDiff, _ := PlanColdAudit(snap, reqDiff)
	assert.NotEqual(t, rec.IDs, recDiff.IDs, "different seed draws different sample")

	// 3. Every chosen reader never saw the card (reader-a was excluded because reader-a saw all of them)
	for _, u := range plan.Units {
		require.Len(t, u.Changes, 1)
		ch := u.Changes[0]
		assert.Equal(t, Readers, ch.Table)
		// Reader chosen should be reader-b or reader-c, NEVER reader-a
		entry := ch.Entry
		require.NotNil(t, entry.Create)
		assert.Contains(t, []string{"reader-b", "reader-c"}, entry.Create.Row, "reader-a saw the card and must not be chosen")
		assert.Equal(t, Asked, entry.Create.Col)
		assert.Equal(t, "cold-audit", entry.Set["audit"])
	}

	// Apply plan to snapshot:
	// Set property on Work table
	for _, pw := range plan.Props {
		snap.Work.SetProp(pw.Name, pw.Value)
	}
	// Add read cards to Readers table
	for _, u := range plan.Units {
		for _, ch := range u.Changes {
			entry := ch.Entry
			snap.Readers.Put(&Card{
				ID:     entry.ID,
				Row:    entry.Create.Row,
				Col:    entry.Create.Col,
				Score:  entry.Create.Score,
				Fields: entry.Set,
			})
		}
	}

	facts := fakeRelease{
		now:      now,
		snap:     snap,
		dealtMax: DealtMaxDefault,
	}

	// 4. Plain check fails when reads are unanswered
	res := ColdAudit(facts)
	assert.False(t, res.OK, "fails when reads are still in Asked")
	assert.Contains(t, res.Evidence, "still unanswered")
	assert.Contains(t, res.Evidence, "look at: nova-sprint queue")

	// 5. Plain check fails naming broken read with finding
	// Mark card 0 as broken
	firstID := rec.IDs[0]
	var firstReadCard *Card
	for _, rc := range snap.Readers.Of(firstID) {
		if rc.F("audit") == "cold-audit" {
			firstReadCard = rc
			break
		}
	}
	require.NotNil(t, firstReadCard)
	firstReadCard.Col = Broken
	firstReadCard.Fields["finding"] = "docs/SPEC.md:12: missing comma"

	// Mark remaining 19 as OK
	for i := 1; i < 20; i++ {
		id := rec.IDs[i]
		for _, rc := range snap.Readers.Of(id) {
			if rc.F("audit") == "cold-audit" {
				rc.Col = OK
			}
		}
	}

	res = ColdAudit(facts)
	assert.False(t, res.OK)
	assert.Contains(t, res.Evidence, "1 broken read(s)")
	assert.Contains(t, res.Evidence, firstID)
	assert.Contains(t, res.Evidence, "docs/SPEC.md:12: missing comma")
	assert.Contains(t, res.Evidence, fmt.Sprintf("look at: nova-sprint card %s, nova-sprint read", firstID))

	// 6. Plain check passes when all 20 reads are OK
	firstReadCard.Col = OK
	res = ColdAudit(facts)
	assert.True(t, res.OK, "passes when all 20 reads are ok: %s", res.Evidence)
	assert.Equal(t, CheckColdAudit, res.Name)
	assert.Contains(t, res.Evidence, "all 20 cold reads ok")
	assert.Contains(t, res.Evidence, "seed 42")
	assert.Equal(t, "RELEASE CHECK cold-audit ok "+res.Evidence, res.Line())

	// 7. Plain check fails when audit is over 48 hours old
	staleFacts := fakeRelease{
		now:      now.Add(49 * time.Hour), // 49 hours after audit asked
		snap:     snap,
		dealtMax: DealtMaxDefault,
	}
	res = ColdAudit(staleFacts)
	assert.False(t, res.OK, "fails when audit is over 48 hours old")
	assert.Contains(t, res.Evidence, "cold audit expired")
	assert.Contains(t, res.Evidence, "limit 48h")
	assert.Contains(t, res.Evidence, "run: nova-sprint release check --audit")

	// 8. Plain check fails when no audit has been asked
	emptySnap := &Snapshot{
		Now:  now,
		Work: NewTable(Work),
	}
	noAuditFacts := fakeRelease{now: now, snap: emptySnap, dealtMax: DealtMaxDefault}
	res = ColdAudit(noAuditFacts)
	assert.False(t, res.OK)
	assert.Contains(t, res.Evidence, "no cold audit has been asked")
}
