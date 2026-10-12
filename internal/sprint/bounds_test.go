package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The lane-broken bound (docs/SPEC-SPRINT.md, the bounds): an attempt that ended
// without a worker report (an idle reap, a harness fault, a lane refused, a push
// refused, a friend lane that never started) says nothing about the brief. It does
// not count toward the brief bound, which counts only attempts whose worker ran and
// reported (a verdict, a pushed head). Four such attempts raise "the lane is broken,
// not the brief" instead, naming the member or friend and the fault.

// brokenLaneWorld is a world with s1-1 a machine's card whose four attempts all ended
// without a worker report: empty run (idle reap) and harness faults, the brief bound
// counting none of them.
func brokenLaneWorld(t *testing.T) *world {
	t.Helper()
	w := friendWorld(t, "c: a machine's card\nREPO: mas-bandwidth/nova-tools\n\nThe task.")
	pr := w.s.Work.Placed("s1-1")
	pr.Fields["attempt"] = "4"
	pr.Fields["ran_attempts"] = "attempt 1: no: idle reap\nattempt 2: no: harness fault\nattempt 3: no: lane refused\nattempt 4: no: push refused"
	w.s.Fleet.Put(pr)
	return w
}

// TestRanSinceCountsOnlyAttemptsThatRan pins that an attempt that ended without a
// worker report does not count toward the brief bound: four attempts, only two of
// which ran, count two toward the bound, and legacy attempts with no record count as
// having run (docs/SPEC-SPRINT.md, the bounds).
func TestRanSinceCountsOnlyAttemptsThatRan(t *testing.T) {
	c := &Card{Fields: map[string]string{"attempt": "4"}}
	c.Fields[FieldRanAttempts] = "attempt 1: yes\nattempt 2: yes\nattempt 3: no: idle reap\nattempt 4: no: harness fault"
	require.Equal(t, 2, ranSince(c, 0), "two attempts that ran count toward the brief bound")
	legacy := &Card{Fields: map[string]string{"attempt": "4"}}
	require.Equal(t, 4, ranSince(legacy, 0), "legacy attempts with no record count as having run")
}

// TestFourNonRunningAttemptsRaiseLaneBroken pins that four attempts that all ended
// without a worker report raise "the lane is broken, not the brief", naming the
// fault, not the brief bound.
func TestFourNonRunningAttemptsRaiseLaneBroken(t *testing.T) {
	w := brokenLaneWorld(t)
	pr := w.s.Work.Placed("s1-1")
	at, ok := AtLaneBroken(w.s, pr, 4)
	require.True(t, ok, "four attempts that never ran are a broken lane, not a broken brief")
	require.Contains(t, at.String(), "the lane is broken", "the lane-broken bound names the breach")
	require.Contains(t, at.String(), "idle reap", "the lane-broken bound names the fault")
	require.NotContains(t, at.String(), "the brief is wrong", "a broken lane is not a broken brief")
}

// TestLaneBrokenJudgmentNamesMemberAndRanRecords pins that the lane-broken judgment
// names the member or friend and carries the attempts' ran record (ran: yes|no and
// its fault class) in the judgment text itself (judgments.go, laneBroken.String;
// docs/SPEC-SPRINT.md, the bounds).
func TestLaneBrokenJudgmentNamesMemberAndRanRecords(t *testing.T) {
	w := brokenLaneWorld(t)
	pr := w.s.Work.Placed("s1-1")
	at, ok := AtLaneBroken(w.s, pr, 4)
	require.True(t, ok, "four attempts that never ran are a broken lane")
	require.NotEmpty(t, at.Lane, "the lane-broken bound names the member or friend")
	require.Contains(t, at.String(), "attempt 1: no: idle reap", "the judgment text carries the ran record with its fault class")
}

// TestLaneBrokenRemedyOffersHoldReworkDrop pins that the lane-broken bound's offered
// actions are hold <member>, rework --tier <other tier>, and drop, never the brief
// bound's brief/drop (judgments.go, laneBroken.Remedy; docs/SPEC-SPRINT.md, the
// bounds).
func TestLaneBrokenRemedyOffersHoldReworkDrop(t *testing.T) {
	w := brokenLaneWorld(t)
	pr := w.s.Work.Placed("s1-1")
	at, ok := AtLaneBroken(w.s, pr, 4)
	require.True(t, ok, "four attempts that never ran are a broken lane")
	remedy := at.Remedy()
	require.Contains(t, remedy, "hold ", "the lane-broken remedy offers hold <member>")
	require.Contains(t, remedy, "rework --tier", "the lane-broken remedy offers rework on another tier")
	require.Contains(t, remedy, "drop", "the lane-broken remedy offers drop")
	require.NotContains(t, remedy, "nova-sprint brief", "a broken lane is not answered by correcting the brief")
}

// TestRecordRanWritesTheAttemptRecord pins the attempt record's shape (judgments.go,
// FieldRanAttempts): one line per attempt, "attempt <n>: yes" or "attempt <n>: no: <fault
// class>", an attempt's record replaced, others kept.
func TestRecordRanWritesTheAttemptRecord(t *testing.T) {
	got := recordRan("", 1, false, "idle reap")
	require.Equal(t, "attempt 1: no: idle reap", got)
	got = recordRan("", 1, true, "")
	require.Equal(t, "attempt 1: yes", got)
	got = recordRan("attempt 1: yes\nattempt 3: no: lane refused", 2, false, "push refused")
	require.Contains(t, got, "attempt 1: yes")
	require.Contains(t, got, "attempt 2: no: push refused")
	require.Contains(t, got, "attempt 3: no: lane refused")
	got = recordRan("attempt 1: yes", 1, false, "harness fault")
	require.Equal(t, "attempt 1: no: harness fault", got, "an attempt's record is replaced, not doubled")
}

// TestAtBriefBoundCountsOnlyAttemptsThatRan pins the brief bound's count (brief_bound.go,
// AtBriefBound): the attempt cap counts only attempts whose worker ran and reported, so
// four attempts of which only two ran are under a cap of four. Today they all counted as
// brief evidence: this is the failing line.
func TestAtBriefBoundCountsOnlyAttemptsThatRan(t *testing.T) {
	c := &Card{ID: "x", Fields: map[string]string{"attempt": "4",
		FieldRanAttempts: "attempt 1: yes\nattempt 2: yes\nattempt 3: no: idle reap\nattempt 4: no: harness fault"}}
	_, atBound := AtBriefBound(c, "", 4)
	require.False(t, atBound, "two attempts that ran do not reach the four-attempt cap: the brief bound counts only attempts whose worker ran and reported")

	ran4 := &Card{ID: "y", Fields: map[string]string{"attempt": "4",
		FieldRanAttempts: "attempt 1: yes\nattempt 2: yes\nattempt 3: yes\nattempt 4: yes"}}
	bb, atBound := AtBriefBound(ran4, "", 4)
	require.True(t, atBound, "four attempts that ran reach the cap")
	require.Equal(t, 4, bb.Since, "the bound counts the four attempts that ran")
}

// TestBriefStopAtRaisesLaneBrokenNotBrief pins the wiring of the lane-broken bound into
// the brief's stop (steps_review.go, briefStopAt): four attempts that all ended without a
// worker report stop the card on the lane, not on the brief.
func TestBriefStopAtRaisesLaneBrokenNotBrief(t *testing.T) {
	w := brokenLaneWorld(t)
	pr := w.s.Work.Placed("s1-1")
	stop, ok := briefStopAt(w.s, pr, "", "")
	require.True(t, ok, "four attempts that never ran stop the card on the lane, not the brief")
	require.Contains(t, stop.String(), "the lane is broken", "the stop names the lane, not the brief")
	require.NotContains(t, stop.String(), "the brief is wrong", "a broken lane is not a broken brief")
	require.Contains(t, stop.Why(), "hold ", "the stop offers hold <member>")
	require.Contains(t, stop.Why(), "rework --tier", "the stop offers rework on another tier")
	require.Contains(t, stop.Why(), "drop", "the stop offers drop")
}

// TestAttemptLineShowsRan pins that the work card's attempt line carries its ran record
// (route.go, AttemptLine): "ran=yes" when its worker ran and reported, "ran=no: <fault
// class>" when it ended without a worker report, "-" before any record.
func TestAttemptLineShowsRan(t *testing.T) {
	ran := &Card{Fields: map[string]string{"attempt": "3", FieldRanAttempts: "yes"}}
	require.Contains(t, AttemptLine(ran), " ran=yes", "the attempt line shows the worker ran")
	notRan := &Card{Fields: map[string]string{"attempt": "3", FieldRanAttempts: "no: push refused"}}
	require.Contains(t, AttemptLine(notRan), " ran=no: push refused", "the attempt line shows the fault class")
	unrecorded := &Card{Fields: map[string]string{"attempt": "3"}}
	require.Contains(t, AttemptLine(unrecorded), " ran=-", "an attempt with no record shows ran=-")
}
