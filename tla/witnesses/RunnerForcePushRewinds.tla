------------------------------ MODULE RunnerForcePushRewinds ------------------------------
(***************************************************************************)
(* Layer 4 runner model: nova-worker on one bench (docs/LAYER-4-EDGES.md,  *)
(* Guarantees 7 to 11). Size: 2 consumers, width 2, 1 slot, 1 Crash.       *)
(*                                                                         *)
(* The deal is the take (defect 52): both consumers are dealt working, in  *)
(* the row's hand, at Init; there is no ready state to take from. Take is  *)
(* the row's acknowledgement (a cursor) and the start of its job, and it   *)
(* moves no store state.                                                   *)
(*                                                                         *)
(* Passes and jobs are separate processes (defect 66, 2026-11-07): pass is *)
(* the wake loop's pass (idle or planning); it acknowledges the cards in   *)
(* its hand and ends whatever its jobs are doing (PassNeverWaitsOnJobs). A *)
(* job is the rest of each consumer's actions. Stop is the worker's own    *)
(* stop (defect 73): each live harness is killed, its job kept at staged   *)
(* (stopped) to be resumed, never a fault (StopIsNeverAFault). Release is  *)
(* the restart's no-blame give-back of a job it cannot resume (a job       *)
(* directory with no receipt): its commits pushed, the consumer withdrawn  *)
(* with no result and no report, the directory gone; the producer's redeal *)
(* is a new consumer, the card machine's. A dealt card with no job         *)
(* directory is no release: the pass acknowledges it and runs it.          *)
(*                                                                         *)
(* A branch is its holder's (defect 78, 2026-11-07): branch[c] is consumer *)
(* c's card branch on origin: who wrote its tip (none, this job, or the    *)
(* card's next holder after a withdraw re-dealt it), whether that holder   *)
(* wrote it, and how many times this job pushed after its consumer was     *)
(* final. A job pushes only while its consumer is working, by lease (the   *)
(* tip is not the holder's); Withdraw is the sprint's and touches no job;  *)
(* Retire is the next pass removing a withdrawn job's directory, pushing   *)
(* nothing; HolderPush is the new holder's push from another row.          *)
(* NoPushAfterFinal and BranchOnlyByHolder; reversed witnesses             *)
(* RunnerWithdrawnPushes and RunnerForcePushRewinds.                       *)
(***************************************************************************)
EXTENDS Integers, FiniteSets

CONSTANTS Width, MaxSlots, MaxCrashes

Consumers == {"c1", "c2"}
StoreStates == {"ready", "working", "done", "fail"}
JobStates == {"none", "taken", "staged", "running", "harvested", "rebased", "reporting", "recorded", "cleaned"}
RetireStates == {"none", "taken", "staged", "harvested", "rebased"}   \* a job with no harness and no report owed

VARIABLES storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass, branch

vars == <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass, branch>>

WorkingSet == {c \in Consumers : storeState[c] = "working"}
AliveSlots == (IF harnessAlive["c1"] > 0 THEN harnessAlive["c1"] ELSE 0) +
              (IF harnessAlive["c2"] > 0 THEN harnessAlive["c2"] ELSE 0)

ResIDFor(c) == IF c = "c1" THEN 1 ELSE 2
PidFor(c) == IF c = "c1" THEN 101 ELSE 102

Init ==
  /\ storeState = [c \in Consumers |-> "working"]
  /\ jobState = [c \in Consumers |-> "none"]
  /\ slotsInUse = 0
  /\ width = Width
  /\ jobDir = [c \in Consumers |-> [exists |-> FALSE, creates |-> 0, pid |-> 0, resId |-> 0, pushed |-> FALSE, outcome |-> "none", bounced |-> FALSE, stopped |-> FALSE]]
  /\ harnessAlive = [c \in Consumers |-> 0]
  /\ appliedOps = {}
  /\ env = [crashes |-> 0, storeDown |-> FALSE, crashed |-> FALSE, released |-> FALSE]
  /\ pass = "idle"
  /\ branch = [c \in Consumers |-> [tip |-> "none", holderWrote |-> FALSE, finalPushes |-> 0]]

\* A job's push moves its branch only while its consumer is working (it
\* holds the card) and by lease: the tip is still none or this job's, never
\* the next holder's (--force-with-lease=refs/heads/<branch>:<last pushed>).
JobPushes(c) ==
  IF branch[c].tip # "holder" THEN [branch EXCEPT ![c].tip = "job"] ELSE branch

Wake ==
  /\ ~env.crashed
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, pass, branch>>

Take(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ pass = "planning"   \* only a pass acknowledges
  /\ storeState[c] = "working"
  /\ jobState[c] = "none"
  /\ jobState' = [jobState EXCEPT ![c] = "taken"]
  /\ UNCHANGED <<storeState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass, branch>>

Stage(c) ==
  /\ ~env.crashed
  /\ storeState[c] = "working"   \* a withdrawn job moves only to Retire
  /\ jobState[c] = "taken"
  /\ ~jobDir[c].exists   \* refuse existing job directory (never reused)
  /\ jobDir[c].creates = 0
  /\ jobDir' = [jobDir EXCEPT ![c].exists = TRUE, ![c].creates = @ + 1]
  /\ jobState' = [jobState EXCEPT ![c] = "staged"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass, branch>>

StageFail(c) ==
  /\ ~env.crashed
  /\ storeState[c] = "working"   \* a withdrawn job moves only to Retire
  /\ jobState[c] = "taken"
  /\ ~jobDir[c].exists
  /\ jobDir[c].creates = 0
  /\ jobDir' = [jobDir EXCEPT ![c].exists = TRUE, ![c].creates = @ + 1, ![c].pushed = TRUE, ![c].outcome = "fault", ![c].resId = ResIDFor(c)]
  /\ jobState' = [jobState EXCEPT ![c] = "reporting"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass, branch>>

Run(c) ==
  /\ ~env.crashed
  /\ storeState[c] = "working"   \* a withdrawn job moves only to Retire
  /\ jobState[c] = "staged"
  /\ harnessAlive[c] = 0
  /\ slotsInUse < MaxSlots
  /\ slotsInUse' = slotsInUse + 1
  /\ harnessAlive' = [harnessAlive EXCEPT ![c] = 1]
  /\ jobDir' = [jobDir EXCEPT ![c].pid = PidFor(c), ![c].stopped = FALSE]   \* record pid in job.json at start; a resumed job runs again
  /\ jobState' = [jobState EXCEPT ![c] = "running"]
  /\ UNCHANGED <<storeState, width, appliedOps, env, pass, branch>>

Die(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "running"
  /\ harnessAlive[c] = 1
  /\ harnessAlive' = [harnessAlive EXCEPT ![c] = 0]
  /\ slotsInUse' = slotsInUse - 1
  /\ jobDir' = [jobDir EXCEPT ![c].pid = 0, ![c].outcome = "fault", ![c].bounced = FALSE]   \* Die is a fault, never a fail that bounces
  /\ jobState' = [jobState EXCEPT ![c] = "harvested"]
  /\ UNCHANGED <<storeState, width, appliedOps, env, pass, branch>>

Harvest(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "running"
  /\ harnessAlive[c] = 1
  /\ harnessAlive' = [harnessAlive EXCEPT ![c] = 0]
  /\ slotsInUse' = slotsInUse - 1   \* slot freed only when harness ends
  /\ jobDir' = [jobDir EXCEPT ![c].pid = 0, ![c].outcome = "ok"]
  /\ jobState' = [jobState EXCEPT ![c] = "harvested"]
  /\ UNCHANGED <<storeState, width, appliedOps, env, pass, branch>>

Rebase(c) ==
  /\ ~env.crashed
  /\ storeState[c] = "working"   \* a withdrawn job moves only to Retire
  /\ jobState[c] = "harvested"
  /\ jobDir[c].outcome = "ok"
  /\ jobState' = [jobState EXCEPT ![c] = "rebased"]
  /\ UNCHANGED <<storeState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass, branch>>

Conflict(c) ==
  /\ ~env.crashed
  /\ storeState[c] = "working"   \* a withdrawn job moves only to Retire
  /\ jobState[c] = "harvested"
  /\ jobDir[c].outcome = "ok"
  /\ jobDir' = [jobDir EXCEPT ![c].outcome = "fail", ![c].bounced = TRUE]
  /\ jobState' = [jobState EXCEPT ![c] = "rebased"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass, branch>>

Push(c) ==
  /\ ~env.crashed
  /\ storeState[c] = "working"   \* only the holder touches the branch
  /\ jobState[c] \in {"harvested", "rebased"}
  /\ (jobState[c] = "harvested" => jobDir[c].outcome # "ok")
  /\ jobDir' = [jobDir EXCEPT ![c].pushed = TRUE, ![c].resId = IF @ = 0 THEN ResIDFor(c) ELSE @]
  /\ jobState' = [jobState EXCEPT ![c] = "reporting"]
  /\ branch' = JobPushes(c)
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass>>

Report(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ jobState[c] = "reporting"
  /\ jobDir[c].pushed
  /\ LET id == jobDir[c].resId IN
     /\ appliedOps' = IF \E op \in appliedOps : op.id = id
                      THEN appliedOps
                      ELSE appliedOps \cup {[id |-> id, c |-> c, res |-> jobDir[c].outcome]}
     /\ storeState' = [storeState EXCEPT ![c] = IF @ = "working" THEN "done" ELSE @]
     /\ jobState' = [jobState EXCEPT ![c] = "recorded"]
  /\ UNCHANGED <<slotsInUse, width, jobDir, harnessAlive, env, pass, branch>>

ReplyLost(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ env.crashes < MaxCrashes
  /\ jobState[c] = "reporting"
  /\ jobDir[c].pushed
  /\ LET id == jobDir[c].resId IN
     /\ appliedOps' = IF \E op \in appliedOps : op.id = id
                      THEN appliedOps
                      ELSE appliedOps \cup {[id |-> id, c |-> c, res |-> jobDir[c].outcome]}
     /\ storeState' = [storeState EXCEPT ![c] = IF @ = "working" THEN "done" ELSE @]
     /\ jobDir' = [jobDir EXCEPT ![c].resId = id]   \* keep same result id on retry
     /\ env' = [env EXCEPT !.crashes = @ + 1]
  /\ UNCHANGED <<jobState, slotsInUse, width, harnessAlive, pass, branch>>

Clean(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "recorded"   \* clean only after report is recorded and head pushed
  /\ storeState[c] \in {"done", "fail"}
  /\ jobDir[c].pushed
  /\ jobDir' = [jobDir EXCEPT ![c].exists = FALSE]
  /\ jobState' = [jobState EXCEPT ![c] = "cleaned"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass, branch>>

\* The sprint withdraws the consumer (a preempt, a silence) and re-deals its
\* card to another row: the store moves; the job and its directory do not.
Withdraw(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ storeState[c] \in {"ready", "working"}
  /\ jobState[c] \in RetireStates
  /\ harnessAlive[c] = 0
  /\ storeState' = [storeState EXCEPT ![c] = "fail"]
  /\ UNCHANGED <<jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass, branch>>

\* The first pass that sees a withdrawn consumer retires its job: the
\* directory is removed (made writable first) and nothing is pushed.
Retire(c) ==
  /\ ~env.crashed
  /\ pass = "planning"
  /\ storeState[c] = "fail"
  /\ jobState[c] \in RetireStates
  /\ harnessAlive[c] = 0
  /\ jobDir' = [jobDir EXCEPT ![c].exists = FALSE]
  /\ jobState' = [jobState EXCEPT ![c] = "cleaned"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass, branch>>

\* The card's next holder, on another row, pushes its branch.
HolderPush(c) ==
  /\ storeState[c] = "fail"
  /\ ~branch[c].holderWrote
  /\ branch' = [branch EXCEPT ![c].tip = "holder", ![c].holderWrote = TRUE]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass>>

Crash ==
  /\ ~env.crashed
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1, !.crashed = TRUE]
  /\ pass' = "idle"   \* the pass dies with the process
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, branch>>

\* The worker's own stop (ctx cancelled): every live harness is killed and
\* its slot freed; its job is kept at staged, marked stopped, with no pid,
\* no outcome and no report, to be run again after Restart.
Stop ==
  /\ ~env.crashed
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1, !.crashed = TRUE]
  /\ pass' = "idle"
  /\ harnessAlive' = [c \in Consumers |-> 0]
  /\ slotsInUse' = 0
  /\ jobState' = [c \in Consumers |-> IF jobState[c] = "running" THEN "staged" ELSE jobState[c]]
  /\ jobDir' = [c \in Consumers |-> IF jobState[c] = "running" THEN [jobDir[c] EXCEPT !.pid = 0, !.stopped = TRUE] ELSE jobDir[c]]
  /\ UNCHANGED <<storeState, width, appliedOps, branch>>

\* A pass: a wake (the hook's or the periodic one) starts it when none runs;
\* it ends whatever its jobs are doing.
PassStart ==
  /\ ~env.crashed
  /\ pass = "idle"
  /\ pass' = "planning"
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, branch>>

PassEnd ==
  /\ ~env.crashed
  /\ pass = "planning"
  /\ pass' = "idle"
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, branch>>

\* After a restart, a job the worker cannot resume (its directory holds no
\* receipt) is given back with no blame: its commits pushed once, by lease,
\* while it still holds the card, then the consumer withdrawn with no result
\* (no outcome, no bounce, no report), and its directory retired once the
\* release is recorded.
Release(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ env.crashes > 0
  /\ ~env.released
  /\ storeState[c] = "working"
  /\ jobState[c] \in {"staged", "harvested"}
  /\ jobDir[c].exists
  /\ harnessAlive[c] = 0
  /\ storeState' = [storeState EXCEPT ![c] = "fail"]
  /\ jobDir' = [jobDir EXCEPT ![c].pushed = TRUE, ![c].exists = FALSE]
  /\ jobState' = [jobState EXCEPT ![c] = "cleaned"]
  /\ env' = [env EXCEPT !.released = TRUE]
  /\ branch' = JobPushes(c)
  /\ UNCHANGED <<slotsInUse, width, harnessAlive, appliedOps, pass>>

Restart ==
  /\ env.crashed
  /\ env' = [env EXCEPT !.crashed = FALSE, !.storeDown = FALSE]
  /\ harnessAlive' = harnessAlive   \* adopt live harnesses by pid and nonce, never spawn a second
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, appliedOps, pass, branch>>

StoreDown ==
  /\ ~env.storeDown
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1, !.storeDown = TRUE]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, pass, branch>>

StoreUp ==
  /\ env.storeDown
  /\ env' = [env EXCEPT !.storeDown = FALSE]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, pass, branch>>

Done ==
  /\ \A c \in Consumers : jobState[c] = "cleaned"
  /\ UNCHANGED vars

\* REVERSED WITNESS (defect 78): the live bug: a withdrawn job's directory is never removed (a dirty tree kept, a read-only module cache), and every recovery pass pushes its stale head with a plain --force, rewinding the new holder's branch.
StalePush(c) ==
  /\ ~env.crashed
  /\ pass = "planning"
  /\ storeState[c] = "fail"
  /\ jobDir[c].exists
  /\ branch[c].finalPushes < 2
  /\ branch' = [branch EXCEPT ![c].tip = "job", ![c].finalPushes = @ + 1]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass>>

Next ==
  \/ Wake
  \/ PassStart
  \/ PassEnd
  \/ Stop
  \/ \E c \in Consumers :
       \/ Take(c)
       \/ Stage(c)
       \/ StageFail(c)
       \/ Run(c)
       \/ Die(c)
       \/ Harvest(c)
       \/ Rebase(c)
       \/ Conflict(c)
       \/ Push(c)
       \/ Report(c)
       \/ ReplyLost(c)
       \/ Clean(c)
       \/ Withdraw(c)
       \/ StalePush(c)
       \/ HolderPush(c)
       \/ Release(c)
  \/ Crash
  \/ Restart
  \/ StoreDown
  \/ StoreUp
  \/ Done

\* Take is strongly fair: it is enabled only while a pass plans, and every
\* pass acknowledges the cards in its hand with no job yet, so a card that
\* stays in hand through infinitely many passes is acknowledged. Retire is
\* strongly fair for the same reason: only a pass retires.
FairConsumer(c) ==
  /\ SF_vars(Take(c))
  /\ WF_vars(Stage(c))
  /\ WF_vars(Run(c))
  /\ WF_vars(Harvest(c))
  /\ WF_vars(Rebase(c))
  /\ WF_vars(Push(c))
  /\ WF_vars(Report(c))
  /\ WF_vars(Clean(c))

Spec == Init /\ [][Next]_vars /\ WF_vars(Restart) /\ WF_vars(StoreUp) /\ WF_vars(PassStart) /\ WF_vars(PassEnd)
        /\ FairConsumer("c1") /\ FairConsumer("c2")

TypeOK ==
  /\ storeState \in [Consumers -> StoreStates]
  /\ jobState \in [Consumers -> JobStates]
  /\ slotsInUse \in 0..2
  /\ width = Width
  /\ pass \in {"idle", "planning"}
  /\ branch \in [Consumers -> [tip : {"none", "job", "holder"}, holderWrote : BOOLEAN, finalPushes : 0..2]]

OneLiveHarnessPerJobDir ==
  \A c \in Consumers : harnessAlive[c] <= 1 /\ (harnessAlive[c] > 0 => jobDir[c].exists)

PidRecordedAtStart ==
  \A c \in Consumers : (jobState[c] = "running" \/ harnessAlive[c] > 0) => jobDir[c].pid = PidFor(c)

JobDirNeverReused ==
  \A c \in Consumers : jobDir[c].creates <= 1

HarnessInsideSlot ==
  slotsInUse = AliveSlots /\ slotsInUse <= MaxSlots

\* A job is cleaned only once its consumer is final, and one that ended done
\* was pushed first (a withdrawn job is retired with nothing pushed).
CleanOnlyAfterRecordedAndPushed ==
  \A c \in Consumers :
    jobState[c] = "cleaned" => (storeState[c] \in {"done", "fail"} /\ (storeState[c] = "done" => jobDir[c].pushed))

ReportRecordedAtMostOnce ==
  \A op1, op2 \in appliedOps : op1.c = op2.c => op1 = op2

FaultSpendsNoAttemptOrBounce ==
  \A c \in Consumers : jobDir[c].outcome = "fault" => ~jobDir[c].bounced

WorkingWithinWidth ==
  Cardinality(WorkingSet) <= width

SlotsWithinCapacity ==
  slotsInUse <= MaxSlots

\* Defect 66: a pass can always end, whatever its jobs are doing; so a wake
\* that arrives while a job runs gets its own pass, and a card dealt into
\* the hand is acknowledged without waiting for that job.
PassNeverWaitsOnJobs ==
  (pass = "planning" /\ ~env.crashed) => ENABLED PassEnd

\* Defect 73: a job the worker's stop cut short carries no outcome and no
\* report until it runs again.
StopIsNeverAFault ==
  \A c \in Consumers : jobDir[c].stopped =>
    /\ jobDir[c].outcome = "none"
    /\ ~(\E op \in appliedOps : op.c = c)

\* Defect 78: a job never pushes once its consumer is final (withdrawn,
\* released, done): the branch is the card's next holder's.
NoPushAfterFinal ==
  \A c \in Consumers : branch[c].finalPushes = 0

\* Defect 78: once the card's next holder wrote the branch, the tip stays
\* the holder's; no job of this worker rewinds it.
BranchOnlyByHolder ==
  \A c \in Consumers : branch[c].holderWrote => branch[c].tip = "holder"

\* Reach checks (each must be violated: the state is reachable).
NeverTakesWhileAJobRuns ==
  ~(\E c, d \in Consumers : c # d /\ harnessAlive[c] = 1 /\ jobState[d] = "taken")
NeverStopped == \A c \in Consumers : ~jobDir[c].stopped
NeverResumedAfterStop == ~(\E c \in Consumers : jobDir[c].stopped /\ ~env.crashed /\ jobState[c] = "staged" /\ pass = "planning")
NeverReleased == ~env.released
NeverRetired == ~(\E c \in Consumers : storeState[c] = "fail" /\ jobDir[c].creates > 0 /\ ~jobDir[c].exists /\ ~jobDir[c].pushed)
NeverHolderPushedOverJob == ~(\E c \in Consumers : branch[c].holderWrote /\ jobDir[c].pushed /\ jobDir[c].creates > 0)

Liveness ==
  \A c \in Consumers : [](storeState[c] = "working" => <>(jobState[c] \in {"recorded", "cleaned"}))

\* Defect 78: a withdrawn job's directory is removed, not kept for ever.
FinalDirRemoved ==
  \A c \in Consumers : [](storeState[c] = "fail" /\ jobDir[c].exists => <>(~jobDir[c].exists))
=============================================================================
