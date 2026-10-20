package sprint

import "time"

// Every judgment kind has an exit at its deadline: either the safe server default the
// machine answers itself, or the owner delivery that hands the decision to the sprint's
// owner. No kind is left with a silent "none" past twice its deadline; a person who is away
// is not a reason for a stream to wait forever. Every waiting entity has progress under
// fairness or a server-computed deadline that hands it to a coordinator judgment, which
// itself has a deadline (the owner, 2026-10-19: "Any class of deadlock, that's a thing we
// must fix.").
//
// tla/WaitFor.tla Overdue writes the overdue line and ServerDefault/OwnerDeliver clear the
// judgment; tla/DecisionsOwed.tla DefaultOnlyPastTwice and DefaultIsSafe bound what a
// default may do, and OwnerWindow is the owner lane's cadence. docs/SPEC-SPRINT.md section
// 8, "Decisions owed: deadlines, owner escalation and safe defaults", carries the table.
const (
	// ExitHoldWithCause parks or holds the card with its cause; it never drops, accepts
	// or edits a brief. Today's NBound and NBriefWrong default.
	// tla/DecisionsOwed.tla DefaultIsSafe (a default may "hold").
	ExitHoldWithCause = "hold with cause"
	// ExitWaitReAsk waits thirty minutes and re-asks; it never drops, accepts or edits a
	// brief. Today's NCannotAsk default.
	// tla/DecisionsOwed.tla DefaultIsSafe (a default may "wait").
	ExitWaitReAsk = "wait 30m and re-ask"
	// ExitOwner hands the open decision to the owner through the existing decision
	// escalation lane: delivered at twice its deadline and again each OwnerWindow while
	// it stays open. The tick never answers it and no card is moved.
	// tla/WaitFor.tla OwnerDeliver; tla/DecisionsOwed.tla OwnerDue, Observe.
	ExitOwner = "owner"
)

// OwnerWindow is how often the owner lane delivers an open decision again while it stays
// open (cmd/nova-sprint decisionPass's owner cadence).
// tla/DecisionsOwed.tla OwnerWindow.
const OwnerWindow = 30 * time.Minute

// NServerDefaultHold is the type of the note DefaultHeldDecisions writes for a card held
// by a server default (seat_answers.go).
const NServerDefaultHold = "server-default hold"

// JudgmentExits is the one table: every judgment kind's exit at its deadline when no one
// answers. Three kinds keep today's safe server default; every other kind is handed to
// the owner by the existing owner delivery. A kind with no entry is a test failure
// (TestEveryJudgmentKindHasADeadlineExit), never a silent "none".
// tla/WaitFor.tla ServerDefault, OwnerDeliver; docs/SPEC-SPRINT.md section 8.
var JudgmentExits = map[string]string{
	// Today's safe server defaults, unchanged.
	NBound:      ExitHoldWithCause,
	NBriefWrong: ExitHoldWithCause,
	NCannotAsk:  ExitWaitReAsk,
	// Every other kind: the owner, at twice its deadline and again each owner window.
	NAdoptFailed:          ExitOwner,
	NAlarmFleet:           ExitOwner,
	NAlarmMerging:         ExitOwner,
	NAlarmReady:           ExitOwner,
	NAlarmReview:          ExitOwner,
	NAllOutOfCredit:       ExitOwner,
	NBaseCIRed:            ExitOwner,
	NBaseRed:              ExitOwner,
	NBlocked:              ExitOwner,
	NBriefDefect:          ExitOwner,
	NBrokenReadsOutrun:    ExitOwner,
	NCIRed:                ExitOwner,
	NConflict:             ExitOwner,
	NCoordinatorBehind:    ExitOwner,
	NCostGap:              ExitOwner,
	NCross:                ExitOwner,
	NDeadBase:             ExitOwner,
	NDevBehind:            ExitOwner,
	NDriftAhead:           ExitOwner,
	NDriftBaseRed:         ExitOwner,
	NDriftCardBase:        ExitOwner,
	NDriftServer:          ExitOwner,
	NFewReaders:           ExitOwner,
	NFilesAlarm:           ExitOwner,
	NFinishRefused:        ExitOwner,
	NFirstTimeUnderTarget: ExitOwner,
	NFleetFailure:         ExitOwner,
	NFriendEmpty:          ExitOwner,
	NFriendIdle:           ExitOwner,
	NFriendSyncFailing:    ExitOwner,
	NGateNeeds:            ExitOwner,
	NHarnessFault:         ExitOwner,
	NHoldNoCause:          ExitOwner,
	NInvariant:            ExitOwner,
	NLanderFails:          ExitOwner,
	NLanderRefuses:        ExitOwner,
	NLandingStall:         ExitOwner,
	NMemberEnvFault:       ExitOwner,
	NMemberIdleReaps:      ExitOwner,
	NMemberPegged:         ExitOwner,
	NMergeLate:            ExitOwner,
	NMirrorNeed:           ExitOwner,
	NMissingBase:          ExitOwner,
	NMissingNeed:          ExitOwner,
	NNeedBlocked:          ExitOwner,
	NNeedsGone:            ExitOwner,
	NNoFrontierRoom:       ExitOwner,
	NNoMember:             ExitOwner,
	NNoRoute:              ExitOwner,
	NOpStuck:              ExitOwner,
	NOverdue:              ExitOwner,
	NOverloaded:           ExitOwner,
	NPinIgnored:           ExitOwner,
	NProviderFunds:        ExitOwner,
	NProviderKey:          ExitOwner,
	NProviderLow:          ExitOwner,
	NRaiseReadTier:        ExitOwner,
	NReadBranchMissing:    ExitOwner,
	NReadBroken:           ExitOwner,
	NReadLate:             ExitOwner,
	NReaderBreaks:         ExitOwner,
	NReadersBehind:        ExitOwner,
	NReadsExhausted:       ExitOwner,
	NReadyOver:            ExitOwner,
	NReadyToAccept:        ExitOwner,
	NRed:                  ExitOwner,
	NRejected:             ExitOwner,
	NRemindFailed:         ExitOwner,
	NRepairSkipped:        ExitOwner,
	NReportJudgment:       ExitOwner,
	NReturned:             ExitOwner,
	NReviewNoReader:       ExitOwner,
	NScoredLow:            ExitOwner,
	NSentinelReached:      ExitOwner,
	NSprintDone:           ExitOwner,
	NStalled:              ExitOwner,
	NStarving:             ExitOwner,
	NStatus:               ExitOwner,
	NStopDebtBeat:         ExitOwner,
	NStopMemberDown:       ExitOwner,
	NStopPinWaits:         ExitOwner,
	NStranded:             ExitOwner,
	NStreamStale:          ExitOwner,
	NTimer:                ExitOwner,
	NWorkFailed:           ExitOwner,
	NWorkLate:             ExitOwner,
}

// MissingJudgmentExits names the kinds with no JudgmentExits entry, in the order given.
// hiddenForTest is the test-only seam: a non-nil predicate hides that kind's entry, the
// reversed witness TestEveryJudgmentKindHasADeadlineExit turns on to prove a missing kind
// fails the check by name. nil is production's read.
func MissingJudgmentExits(kinds []string, hiddenForTest func(string) bool) []string {
	var out []string
	for _, k := range kinds {
		if hiddenForTest != nil && hiddenForTest(k) {
			out = append(out, k)
			continue
		}
		if _, ok := JudgmentExits[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

// OwnerExitDue says an open note is due for the existing owner delivery now: a kind whose
// exit is ExitOwner at twice its deadline, and again each OwnerWindow while it stays open
// (the owner lane's own cadence repeats it); a server-default hold once its second re-raise
// has gone unanswered. A safe server default is not an owner delivery.
// tla/WaitFor.tla OwnerDeliver; tla/DecisionsOwed.tla OwnerDue, DefaultOnlyPastTwice.
func OwnerExitDue(now time.Time, n Note, stopped func(time.Time, time.Time) time.Duration) bool {
	age, limit := DecisionAge(now, n, stopped)
	if limit == 0 {
		return false // sprint done: no decision deadline
	}
	if n.Type == NServerDefaultHold {
		// The hold stands after the judgment's safe default; its second re-raise is two
		// deadlines of running time after the hold was written (reraiseHeld's clock).
		return age >= 2*DeadlineJudgment
	}
	exit, ok := JudgmentExits[n.Type]
	return ok && exit == ExitOwner && age >= 2*limit
}

// ExitIsSafe says the exit is one tla/DecisionsOwed.tla DefaultIsSafe permits: it parks
// (hold with cause), re-asks (wait 30m and re-ask) or delivers (owner). No exit drops,
// accepts, merges, lands or edits a brief.
func ExitIsSafe(exit string) bool {
	switch exit {
	case ExitHoldWithCause, ExitWaitReAsk, ExitOwner:
		return true
	}
	return false
}
