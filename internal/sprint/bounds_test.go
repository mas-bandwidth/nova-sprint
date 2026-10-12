package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The lane-broken bound (docs/SPEC-SPRINT.md, the bounds): an attempt that ended
// without a worker report (an idle reap, a harness fault, a lane refused, a push
// refused, a friend lane that never started) says nothing about the brief. Four such
// attempts raise "the lane is broken, not the brief" instead, naming the member or
// friend and the fault. Since 2026-10-12 every attempt counts toward the attempt cap
// whatever ended it (brief_bound.go, AttemptsSince; the owner: the bound is on the
// machine's retries between two coordinator decisions), so the lane-broken bound is
// checked first and the cap is the outer one.

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

// TestAttemptsSinceCountsEveryAttemptSinceTheDecision pins the attempt cap's count
// (brief_bound.go, AttemptsSince): every attempt since the coordinator last decided on
// the card, whatever ended it; a card never decided on counts from its first attempt;
// a decision at attempt n leaves the attempts after it.
func TestAttemptsSinceCountsEveryAttemptSinceTheDecision(t *testing.T) {
	c := &Card{Fields: map[string]string{"attempt": "4"}}
	c.Fields[FieldRanAttempts] = "attempt 1: yes\nattempt 2: yes\nattempt 3: no: idle reap\nattempt 4: no: harness fault"
	require.Equal(t, 4, AttemptsSince(c), "an idle reap and a harness fault count as attempts")
	c.Fields[FieldDecidedAttempt] = "3"
	require.Equal(t, 1, AttemptsSince(c), "one attempt since the decision at attempt 3")
	c.Fields[FieldBriefAttempt] = "4"
	require.Equal(t, 1, AttemptsSince(c), "a replaced brief alone is no decision")
	require.Equal(t, 0, AttemptsSince(&Card{Fields: map[string]string{}}), "a card never dealt")
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

// TestAtBriefBoundCountsEveryAttemptSinceTheDecision pins the attempt cap's count
// (brief_bound.go, AtBriefBound): four attempts of which two ended without a worker
// report reach a cap of four (the owner, 2026-10-12: every attempt kind counts), a
// replaced brief does not reset it (a rule's widen, the cap's old friend deal and stream
// set --base all replace the brief), and the coordinator's decision does.
func TestAtBriefBoundCountsEveryAttemptSinceTheDecision(t *testing.T) {
	c := &Card{ID: "x", Fields: map[string]string{"attempt": "4",
		FieldRanAttempts: "attempt 1: yes\nattempt 2: yes\nattempt 3: no: idle reap\nattempt 4: no: harness fault"}}
	bb, atBound := AtBriefBound(c, "", 4)
	require.True(t, atBound, "four attempts reach the cap, two of them harness faults")
	require.Equal(t, 4, bb.Since, "the bound counts every attempt")
	require.Equal(t, 4, bb.Cap)
	require.Contains(t, bb.String(), "brief defect after 4 attempts (max_attempts 4) without a coordinator decision")
	require.Contains(t, bb.Remedy(), "rework x --fix <text> --tier <a higher tier>", "the re-tier is offered")

	widened := withField(c, FieldBriefAttempt, "3")
	_, atBound = AtBriefBound(widened, "", 4)
	require.True(t, atBound, "a replaced brief alone is no decision: still at the cap")

	decided := withField(c, FieldDecidedAttempt, "3")
	_, atBound = AtBriefBound(decided, "", 4)
	require.False(t, atBound, "one attempt since the decision at attempt 3: under the cap")
	_, atBound = AtBriefBound(withField(decided, "attempt", "7"), "", 4)
	require.True(t, atBound, "four attempts since the decision reach it again")
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
