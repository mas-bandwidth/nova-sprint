------------------------------ MODULE RunnerPassWaitsOnJobs ------------------------------
(***************************************************************************)
(* Layer 4 runner model: nova-worker on one bench (docs/LAYER-4-EDGES.md,  *)
(* Guarantees 7 to 11). Size: 2 consumers, width 2, 1 slot, 1 Crash.       *)
(*                                                                         *)
(* Passes and jobs are separate processes (defect 66, 2026-11-07): pass is *)
(* the wake loop's pass (idle or planning); it takes dealt cards and ends  *)
(* whatever its jobs are doing (PassNeverWaitsOnJobs). A job is the rest   *)
(* of each consumer's actions. Stop is the worker's own stop (defect 73):  *)
(* each live harness is killed, its job kept at staged (stopped) to be     *)
(* resumed, never a fault (StopIsNeverAFault). Release is the restart's    *)
(* no-blame give-back of a card it holds no job directory for.             *)
(***************************************************************************)
EXTENDS Integers, FiniteSets

CONSTANTS Width, MaxSlots, MaxCrashes

Consumers == {"c1", "c2"}
StoreStates == {"ready", "working", "done", "fail"}
JobStates == {"none", "taken", "staged", "running", "harvested", "rebased", "reporting", "recorded", "cleaned"}

VARIABLES storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass

vars == <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass>>

WorkingSet == {c \in Consumers : storeState[c] = "working"}
AliveSlots == (IF harnessAlive["c1"] > 0 THEN harnessAlive["c1"] ELSE 0) +
              (IF harnessAlive["c2"] > 0 THEN harnessAlive["c2"] ELSE 0)

ResIDFor(c) == IF c = "c1" THEN 1 ELSE 2
PidFor(c) == IF c = "c1" THEN 101 ELSE 102

Init ==
  /\ storeState = [c \in Consumers |-> "ready"]
  /\ jobState = [c \in Consumers |-> "none"]
  /\ slotsInUse = 0
  /\ width = Width
  /\ jobDir = [c \in Consumers |-> [exists |-> FALSE, creates |-> 0, pid |-> 0, resId |-> 0, pushed |-> FALSE, outcome |-> "none", bounced |-> FALSE, stopped |-> FALSE]]
  /\ harnessAlive = [c \in Consumers |-> 0]
  /\ appliedOps = {}
  /\ env = [crashes |-> 0, storeDown |-> FALSE, crashed |-> FALSE, released |-> FALSE]
  /\ pass = "idle"

Wake ==
  /\ ~env.crashed
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, pass>>

Take(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ pass = "planning"   \* only a pass takes
  /\ storeState[c] = "ready"
  /\ jobState[c] = "none"
  /\ Cardinality(WorkingSet) < width
  /\ storeState' = [storeState EXCEPT ![c] = "working"]
  /\ jobState' = [jobState EXCEPT ![c] = "taken"]
  /\ UNCHANGED <<slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass>>

Stage(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "taken"
  /\ ~jobDir[c].exists   \* refuse existing job directory (never reused)
  /\ jobDir[c].creates = 0
  /\ jobDir' = [jobDir EXCEPT ![c].exists = TRUE, ![c].creates = @ + 1]
  /\ jobState' = [jobState EXCEPT ![c] = "staged"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass>>

StageFail(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "taken"
  /\ ~jobDir[c].exists
  /\ jobDir[c].creates = 0
  /\ jobDir' = [jobDir EXCEPT ![c].exists = TRUE, ![c].creates = @ + 1, ![c].pushed = TRUE, ![c].outcome = "fault", ![c].resId = ResIDFor(c)]
  /\ jobState' = [jobState EXCEPT ![c] = "reporting"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass>>

Run(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "staged"
  /\ harnessAlive[c] = 0
  /\ slotsInUse < MaxSlots
  /\ slotsInUse' = slotsInUse + 1
  /\ harnessAlive' = [harnessAlive EXCEPT ![c] = 1]
  /\ jobDir' = [jobDir EXCEPT ![c].pid = PidFor(c), ![c].stopped = FALSE]   \* record pid in job.json at start; a resumed job runs again
  /\ jobState' = [jobState EXCEPT ![c] = "running"]
  /\ UNCHANGED <<storeState, width, appliedOps, env, pass>>

Die(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "running"
  /\ harnessAlive[c] = 1
  /\ harnessAlive' = [harnessAlive EXCEPT ![c] = 0]
  /\ slotsInUse' = slotsInUse - 1
  /\ jobDir' = [jobDir EXCEPT ![c].pid = 0, ![c].outcome = "fault", ![c].bounced = FALSE]   \* Die is a fault, never a fail that bounces
  /\ jobState' = [jobState EXCEPT ![c] = "harvested"]
  /\ UNCHANGED <<storeState, width, appliedOps, env, pass>>

Harvest(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "running"
  /\ harnessAlive[c] = 1
  /\ harnessAlive' = [harnessAlive EXCEPT ![c] = 0]
  /\ slotsInUse' = slotsInUse - 1   \* slot freed only when harness ends
  /\ jobDir' = [jobDir EXCEPT ![c].pid = 0, ![c].outcome = "ok"]
  /\ jobState' = [jobState EXCEPT ![c] = "harvested"]
  /\ UNCHANGED <<storeState, width, appliedOps, env, pass>>

Rebase(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "harvested"
  /\ jobDir[c].outcome = "ok"
  /\ jobState' = [jobState EXCEPT ![c] = "rebased"]
  /\ UNCHANGED <<storeState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env, pass>>

Conflict(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "harvested"
  /\ jobDir[c].outcome = "ok"
  /\ jobDir' = [jobDir EXCEPT ![c].outcome = "fail", ![c].bounced = TRUE]
  /\ jobState' = [jobState EXCEPT ![c] = "rebased"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass>>

Push(c) ==
  /\ ~env.crashed
  /\ jobState[c] \in {"harvested", "rebased"}
  /\ (jobState[c] = "harvested" => jobDir[c].outcome # "ok")
  /\ jobDir' = [jobDir EXCEPT ![c].pushed = TRUE, ![c].resId = IF @ = 0 THEN ResIDFor(c) ELSE @]
  /\ jobState' = [jobState EXCEPT ![c] = "reporting"]
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
  /\ UNCHANGED <<slotsInUse, width, jobDir, harnessAlive, env, pass>>

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
  /\ UNCHANGED <<jobState, slotsInUse, width, harnessAlive, pass>>

Clean(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "recorded"   \* clean only after report is recorded and head pushed
  /\ storeState[c] \in {"done", "fail"}
  /\ jobDir[c].pushed
  /\ jobDir' = [jobDir EXCEPT ![c].exists = FALSE]
  /\ jobState' = [jobState EXCEPT ![c] = "cleaned"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env, pass>>

Withdraw(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ storeState[c] \in {"ready", "working"}
  /\ jobState[c] \in {"none", "taken", "staged", "harvested", "rebased"}
  /\ harnessAlive[c] = 0
  /\ storeState' = [storeState EXCEPT ![c] = "fail"]
  /\ jobDir' = [jobDir EXCEPT ![c].pushed = IF jobDir[c].exists THEN TRUE ELSE jobDir[c].pushed, ![c].exists = FALSE]
  /\ jobState' = [jobState EXCEPT ![c] = "cleaned"]
  /\ UNCHANGED <<slotsInUse, width, harnessAlive, appliedOps, env, pass>>

Crash ==
  /\ ~env.crashed
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1, !.crashed = TRUE]
  /\ pass' = "idle"   \* the pass dies with the process
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps>>

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
  /\ UNCHANGED <<storeState, width, appliedOps>>

\* A pass: a wake (the hook's or the periodic one) starts it when none runs;
\* it ends whatever its jobs are doing.
PassStart ==
  /\ ~env.crashed
  /\ pass = "idle"
  /\ pass' = "planning"
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env>>

PassEnd ==
  /\ ~env.crashed
  /\ pass = "planning"
  /\ \A c \in Consumers : harnessAlive[c] = 0   \* FAULT: the pass waits on its jobs (the wake loop before 2026-11-07)
  /\ pass' = "idle"
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env>>

\* After a restart, a working card the worker holds no job directory for is
\* given back with no blame (no outcome, no bounce, no report): ready again.
Release(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ env.crashes > 0
  /\ ~env.released
  /\ storeState[c] = "working"
  /\ jobState[c] = "taken"
  /\ ~jobDir[c].exists
  /\ storeState' = [storeState EXCEPT ![c] = "ready"]
  /\ jobState' = [jobState EXCEPT ![c] = "none"]
  /\ env' = [env EXCEPT !.released = TRUE]
  /\ UNCHANGED <<slotsInUse, width, jobDir, harnessAlive, appliedOps, pass>>

Restart ==
  /\ env.crashed
  /\ env' = [env EXCEPT !.crashed = FALSE, !.storeDown = FALSE]
  /\ harnessAlive' = harnessAlive   \* adopt live harnesses by pid and nonce, never spawn a second
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, appliedOps, pass>>

StoreDown ==
  /\ ~env.storeDown
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1, !.storeDown = TRUE]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, pass>>

StoreUp ==
  /\ env.storeDown
  /\ env' = [env EXCEPT !.storeDown = FALSE]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, pass>>

Done ==
  /\ \A c \in Consumers : jobState[c] = "cleaned"
  /\ UNCHANGED vars

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
       \/ Release(c)
  \/ Crash
  \/ Restart
  \/ StoreDown
  \/ StoreUp
  \/ Done

\* Take is strongly fair: it is enabled only while a pass plans, and every
\* pass takes the ready cards it has room for, so a card that stays ready
\* with room through infinitely many passes is taken (a released card is).
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

OneLiveHarnessPerJobDir ==
  \A c \in Consumers : harnessAlive[c] <= 1 /\ (harnessAlive[c] > 0 => jobDir[c].exists)

PidRecordedAtStart ==
  \A c \in Consumers : (jobState[c] = "running" \/ harnessAlive[c] > 0) => jobDir[c].pid = PidFor(c)

JobDirNeverReused ==
  \A c \in Consumers : jobDir[c].creates <= 1

HarnessInsideSlot ==
  slotsInUse = AliveSlots /\ slotsInUse <= MaxSlots

CleanOnlyAfterRecordedAndPushed ==
  \A c \in Consumers :
    jobState[c] = "cleaned" => (storeState[c] \in {"done", "fail"} /\ (jobDir[c].creates > 0 => jobDir[c].pushed))

ReportRecordedAtMostOnce ==
  \A op1, op2 \in appliedOps : op1.c = op2.c => op1 = op2

FaultSpendsNoAttemptOrBounce ==
  \A c \in Consumers : jobDir[c].outcome = "fault" => ~jobDir[c].bounced

WorkingWithinWidth ==
  Cardinality(WorkingSet) <= width

SlotsWithinCapacity ==
  slotsInUse <= MaxSlots

\* Defect 66: a pass can always end, whatever its jobs are doing; so a wake
\* that arrives while a job runs gets its own pass, and a ready card with
\* room is taken without waiting for that job.
PassNeverWaitsOnJobs ==
  (pass = "planning" /\ ~env.crashed) => ENABLED PassEnd

\* Defect 73: a job the worker's stop cut short carries no outcome and no
\* report until it runs again.
StopIsNeverAFault ==
  \A c \in Consumers : jobDir[c].stopped =>
    /\ jobDir[c].outcome = "none"
    /\ ~(\E op \in appliedOps : op.c = c)

\* Reach checks (each must be violated: the state is reachable).
NeverTakesWhileAJobRuns ==
  ~(\E c, d \in Consumers : c # d /\ harnessAlive[c] = 1 /\ jobState[d] = "taken")
NeverStopped == \A c \in Consumers : ~jobDir[c].stopped
NeverResumedAfterStop == ~(\E c \in Consumers : jobDir[c].stopped /\ ~env.crashed /\ jobState[c] = "staged" /\ pass = "planning")
NeverReleased == ~env.released

Liveness ==
  \A c \in Consumers : [](storeState[c] = "working" => <>(jobState[c] \in {"recorded", "cleaned"}))
=============================================================================
