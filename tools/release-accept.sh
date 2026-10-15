#!/bin/sh
# release-accept: the release gate of a nova-sprint point release (docs/TESTS.md, "Release
# acceptance"). It replaces the ten-minute live watch after `server switch --rollback
# --window 30m` and the hand canary: each check a release passed by watching the live server
# is a test here, every one on a store of its own, run in parallel, the whole set under one
# minute on a bench (two at most: go test's -timeout 2m fails it past that).
#
#   make release-accept                        # this tree, its binary built once
#   make release-accept NOVA_SPRINT_BIN=<path>  # the canary runs that build
#
# It needs redis-server on PATH (NOVA_CI=1: a missing one fails, never skips). Every name below
# must run and pass: a test renamed or gone fails the gate by the count, so the set never
# shrinks in silence. The name list is the gate; add a check by adding its test here.
set -eu

GO=${GO:-go}
P=${RELEASE_ACCEPT_P:-8}

# the tests, by the check each pins (the numbers are the release checks of 2026-10-11), then
# the stall and stuck classes of 2026-10-11, each class's functional test (TestStall...) and
# its unit tests (docs/TESTS.md, "Stall and stuck coverage")
NAMES='
TestAHookGateRefusalInTheWindowIsNotARollback
TestARealLandFailureInTheWindowStillRollsBack
TestASwitchedServerKeepsLandingOnceTheSeatHooksInAgain
TestServerSwitchVerbSwitchesAndRollsBack
TestTheHookGateRefusalIsATypedError
TestCheckRollbackOnLandFailureExitCodes
TestReleaseAcceptTicksOfABusySprintNeverFail
TestReleaseAcceptNeedsHoldACardOffTheDealOnTheStore
TestTheFriendsDealNeverDealsACardWithUnlandedNeeds
TestADealtThenUnheldFriendDoesNotTakeACardWithUnlandedNeeds
TestAMemberTakeRefusesACardWithUnlandedNeeds
TestAReadyCardWithAnUnlandedNeedIsNeverDealt
TestAStopOnUnlandedNeedsIsNotTheWorkers
TestANoUsageNeedsStopIsStillOneTake
TestACardWithdrawnForItsNeedsIsDealtWhenTheyLand
TestLawfulWorkingToWaitingNeedsTheCardsOwnNeedNotLanded
TestALedgerOnlyConflictLandsWithBothSidesRows
TestASameIdFixConflictIsRefused
TestACodeConflictIsStillRefused
TestEvidenceFormsKeepBoth
TestEvidenceHistoryKeepsEveryRevision
TestADoneIsEveryCardTheRowCompleted
TestDoneCountsEveryKindOfCardFromTheLinesThatCompleteIt
TestTheIdleAlarmIsRaisedOnceAnEpisodeNeverOncePerTick
TestTickIdleAlarmsOnceAnEpisodeAndClears
TestTheIdleAlarmNamesTheRootsOnceAnEpisode
TestTickIdleAlarmTellsTheCoordinatorWhy
TestTheFleetAlarmIgnoresReadsCrossingTheThreshold
TestTheIdleFleetCountsWorkNotReads
TestLifecycleIsTheSpecTable
TestLawfulRefusesAMoveOutsideTheLifecycle
TestTheEngineHoldsEveryPlanToTheLifecycle
TestNeedsSetRefusesAnEditThatClosesACycle
TestNeedsSetSeesAWorkingCardsNeeds
TestSentinelSetSeesAWorkingCardsNeeds
TestRelinkSeesAWorkingCardsNeeds
TestABriefBatchCannotCloseACycleBetweenItsCards
TestAddRefusesACycle
TestReleaseAcceptAClearedSprintLandsWithNoSeatAction
TestReleaseAcceptCanaryOneMemberFinishesOneCardOnTheBuild
TestReleaseAcceptTheBinaryNeverReachesAConfiguredServer
TestStallASwitchedServerNeverRollsBackOnTheHookGate
TestStallANeedCycleThroughAWorkingCardIsRefusedOnTheStore
TestStallALedgerOnlyConflictLandsOnTheStore
TestStallDoneCountsEveryCompletionOnTheStore
TestStallTheIdleAlarmCountsWorkNotReadsOnTheStore
TestStallAnOverBoundMergeNoteNeverPoisonsTheDrain
TestAnOverBoundMergeNoteIsRefusedAndTheTickStillDrains
TestAMergeNoteAtTheBoundIsTaken
TestMergeNoteOverTheBoundIsRefusedAtTheVerb
TestCardTextBoundIs64KiB
TestBoundedChecksKeepsWholeFindingsAndSaysWhatItLeftOut
TestStallAnOverBoundBriefNeverPoisonsTheDrain
TestBriefFileOverTheBoundIsRefused
TestABriefOverTheBoundIsRefusedWhileRunning
TestStallTheBinaryRefusesAStoreMissingItsLockedColumns
TestShadowTickRefusesAStoreMissingTheLockedFleetColumns
TestRunRefusesAStoreMissingTheLockedFleetColumns
TestLockedShapeRefusesTheFleetTableBeforeTheVerdictsMigration
TestDoctorPreflightRefusesAStoreMissingTheLockedColumns
TestStallQueuedReadsMoveToAMemberThatComesUpOnTheStore
TestTheRebalanceMovesQueuedReadsToAnIdleMemberInOneTick
TestTheRebalanceMovesAllTheCardsInOneTickNeverOnePerTick
TestTheLevelVerbRebalancesTheReadsToo
TestAMemberProcessKilledOrRestartedLeavesNoGhostRead
TestAMemberKilledSilentGivesItsReadsBackByTheDownPath
TestAReadTheMemberDoesNotRunIsReturnedAtTheNextBeat
TestAMemberRestartReturnsItsReadsAtOnce
TestAWorkingReadTheBeatNamesIsNeverReturned
TestAReadWhoseStageFailedIsRunAgainNotHeldAsAGhost
TestTickNeedsJudgesAGoneNeedInEveryUnlandedState
TestDropRefusesACardAnUnlandedCardNeeds
TestDropWithWaiveLeavesNoStrandedNeed
TestAcceptRefusesACardWhoseNeedHasNotLanded
TestTickNeedsHoldsAndClears
TestEveryStateAlertsTheCoordinatorOfAGoneNeed
TestAGoneNeedRepeatsAtItsDeadlineAndNeedsSetClearsIt
TestDropRefusesANeededCardAndWaiveLeavesNone
TestDropGroupRefusesANeededCard
TestDropWithRelinkRepointsTheNeed
TestNeedsSetOnAMergingCardWithAGoneNeed
'
PKGS='./cmd/nova-sprint/ ./internal/sprint/ ./internal/sprint/store/ github.com/mas-bandwidth/nova-tools/pkg/member'

want=$(printf '%s\n' $NAMES | sed '/^$/d' | sort -u | wc -l | tr -d ' ')
run="^($(printf '%s\n' $NAMES | sed '/^$/d' | paste -sd '|' -))\$"

# the owner's bound: under one minute ideally, two at most. The clock starts here, before
# the build, and the whole run (the build and the tests) is killed when it passes BOUND.
# bounded() starts the command in a process group of its own (setsid, or job control where
# there is no setsid), so the test binaries go test starts and the redis-servers they start
# are in that group with it; a watchdog sends the group TERM at the deadline and KILL 5 s
# later. Every process in the group was started by this gate. On a kill it says what the
# group held and what is left of it (left=0 is the proof nothing was orphaned).
BOUND=120
# a lower bound may be named, to prove the kill on a known case; never a higher one
if [ -n "${RELEASE_ACCEPT_BOUND:-}" ] && [ "$RELEASE_ACCEPT_BOUND" -lt "$BOUND" ]; then
	BOUND=$RELEASE_ACCEPT_BOUND
fi
began=$(date +%s)
deadline=$((began + BOUND))
exec 3>&1 # the watchdog's line goes to the gate's own output, never the command's
timed_out=0
bounded() {
	# RELEASE_ACCEPT_NO_SETSID=1 takes the job-control path even where setsid exists, to
	# prove the refusal below on a known case
	if command -v setsid >/dev/null 2>&1 && [ -z "${RELEASE_ACCEPT_NO_SETSID:-}" ]; then
		setsid "$@" &
		pid=$!
	else
		set -m 2>/dev/null || true
		"$@" &
		pid=$!
		set +m 2>/dev/null || true
	fi
	pgid=$pid
	# the kill is only sound when the command leads a group of its own: wait for setsid to
	# take effect, then refuse loudly when it never does (dash with no tty turns job
	# control off, and then the command stays in this gate's group)
	own=""
	for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
		own=$(ps -o pgid= -p "$pid" 2>/dev/null | tr -d ' ')
		[ "$own" = "$pid" ] && break
		[ -z "$own" ] && break # already exited
		sleep 0.1
	done
	if [ -n "$own" ] && [ "$own" != "$pid" ]; then
		kill -TERM "$pid" 2>/dev/null
		wait "$pid" 2>/dev/null || true
		echo "RELEASE ACCEPT FAILED the command could not be put in a process group of its own (pid=$pid pgid=$own): no setsid, and job control is off; the ${BOUND}s bound could not kill what it starts" >&3
		exit 1
	fi
	(
		while kill -0 "$pid" 2>/dev/null; do
			if [ "$(date +%s)" -ge "$deadline" ]; then
				held=$(pgrep -g "$pgid" | wc -l | tr -d ' ')
				redis=$(pgrep -g "$pgid" redis-server | wc -l | tr -d ' ')
				kill -TERM "-$pgid" 2>/dev/null
				sleep 5
				kill -KILL "-$pgid" 2>/dev/null
				sleep 1
				left=$(pgrep -g "$pgid" | wc -l | tr -d ' ')
				echo "RELEASE ACCEPT KILLED group=$pgid held=$held redis-servers=$redis left=$left" >&3
				exit 0
			fi
			sleep 1
		done
	) &
	watchdog=$!
	rc=0
	wait "$pid" || rc=$?
	wait "$watchdog" 2>/dev/null || true
	if [ "$(date +%s)" -ge "$deadline" ]; then
		timed_out=1
	fi
	# a normal exit leaves nothing behind: whatever is still in the group is killed, named,
	# and fails the gate
	left=$(pgrep -a -g "$pgid" 2>/dev/null || true)
	if [ -n "$left" ]; then
		kill -TERM "-$pgid" 2>/dev/null
		sleep 2
		kill -KILL "-$pgid" 2>/dev/null
		echo "RELEASE ACCEPT FAILED processes were left in the run's group $pgid after it ended, killed:" >&3
		printf '%s\n' "$left" >&3
		exit 1
	fi
	return "$rc"
}
over() {
	echo "RELEASE ACCEPT FAILED wall=$(($(date +%s) - began))s: killed at the ${BOUND}s bound ($1)"
	exit 1
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if [ -z "${NOVA_SPRINT_BIN:-}" ]; then
	NOVA_SPRINT_BIN=$work/nova-sprint
	bounded $GO build -o "$NOVA_SPRINT_BIN" ./cmd/nova-sprint || { [ "$timed_out" -eq 1 ] && over "the build"; echo "RELEASE ACCEPT FAILED the build"; exit 1; }
fi
export NOVA_SPRINT_BIN NOVA_CI=1
# auto maintenance off for every git the tests start (Makefile, NO_AUTO_MAINTENANCE)
export GIT_CONFIG_PARAMETERS="'maintenance.auto'='false'"

left=$((deadline - $(date +%s)))
[ "$left" -gt 0 ] || over "before the tests"
status=0
# go test's own -timeout is set past the bound: the group kill is the bound. A test binary
# that hits its own -timeout panics without its cleanups, which would leave the redis-servers
# it started running outside any group this gate kills.
# shellcheck disable=SC2086 # PKGS is a list of packages
bounded $GO test -tags functional -count=1 -p "$P" -parallel "$P" -timeout "$((left + 60))s" -v -run "$run" $PKGS >"$work/out" 2>&1 || status=$?
wall=$(($(date +%s) - began))
[ "$timed_out" -eq 0 ] || over "the tests"

grep -E '^(FAIL|ok  )' "$work/out" || true
passed=$(grep -E '^--- PASS: Test' "$work/out" | awk '{print $3}' | sort -u | wc -l | tr -d ' ')
failed=$(grep -cE '^--- FAIL: Test' "$work/out" || true)
missing=$(printf '%s\n' $NAMES | sed '/^$/d' | sort -u | while read -r n; do grep -qE "^--- (PASS|FAIL): $n " "$work/out" || echo "$n"; done)

if [ "$status" -ne 0 ] || [ "$passed" -ne "$want" ]; then
	[ -n "$missing" ] && printf 'RELEASE ACCEPT MISSING %s\n' $missing
	# each failure with what it said: the --- FAIL line and its indented lines after it
	awk '/^[[:space:]]*--- FAIL/ {f=1; print; next} f && /^[[:space:]]+[^=]/ {print; next} {f=0}' "$work/out" | head -200
	grep -E '^panic:' -A20 "$work/out" | head -40 || true
	echo "RELEASE ACCEPT FAILED passed=$passed of $want failed=$failed wall=${wall}s bin=$NOVA_SPRINT_BIN"
	exit 1
fi
echo "RELEASE ACCEPT OK passed=$passed of $want wall=${wall}s bin=$NOVA_SPRINT_BIN"
