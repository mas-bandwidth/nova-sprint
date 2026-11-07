------------------------ MODULE RunnerSlotFreedEarly ------------------------
(***************************************************************************)
(* Layer 4 runner model: nova-worker on one bench (AGENTS.md, Edges and worker). *)
(* Size: 2 consumers, width 2, 1 slot, 1 Crash.                            *)
(***************************************************************************)
EXTENDS Integers, FiniteSets

CONSTANTS Width, MaxSlots, MaxCrashes

Consumers == {"c1", "c2"}
StoreStates == {"ready", "working", "done", "fail"}
JobStates == {"none", "taken", "staged", "running", "harvested", "rebased", "ciwait", "reporting", "recorded", "cleaned"}

VARIABLES storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env

vars == <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env>>

WorkingSet == {c \in Consumers : storeState[c] = "working"}
AliveSlots == (IF harnessAlive["c1"] > 0 THEN harnessAlive["c1"] ELSE 0) +
              (IF harnessAlive["c2"] > 0 THEN harnessAlive["c2"] ELSE 0)

ResIDFor(c) == IF c = "c1" THEN 1 ELSE 2

Init ==
  /\ storeState = [c \in Consumers |-> "working"]   \* dealt in hand
  /\ jobState = [c \in Consumers |-> "none"]
  /\ slotsInUse = 0
  /\ width = Width
  /\ jobDir = [c \in Consumers |-> [exists |-> FALSE, creates |-> 0, resId |-> 0, pushed |-> FALSE, outcome |-> "none", bounced |-> FALSE]]
  /\ harnessAlive = [c \in Consumers |-> 0]
  /\ appliedOps = {}
  /\ env = [crashes |-> 0, storeDown |-> FALSE, crashed |-> FALSE]

Wake ==
  /\ ~env.crashed
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps>>

Take(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ storeState[c] = "working"
  /\ jobState[c] = "none"
  /\ jobState' = [jobState EXCEPT ![c] = "taken"]
  /\ UNCHANGED <<storeState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env>>

Stage(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "taken"
  /\ ~jobDir[c].exists   \* refuse existing job directory (never reused)
  /\ jobDir[c].creates = 0
  /\ jobDir' = [jobDir EXCEPT ![c].exists = TRUE, ![c].creates = @ + 1]
  /\ jobState' = [jobState EXCEPT ![c] = "staged"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env>>

StageFail(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "taken"
  /\ ~jobDir[c].exists
  /\ jobDir[c].creates = 0
  /\ jobDir' = [jobDir EXCEPT ![c].exists = TRUE, ![c].creates = @ + 1, ![c].pushed = TRUE, ![c].outcome = "fault", ![c].resId = ResIDFor(c)]
  /\ jobState' = [jobState EXCEPT ![c] = "reporting"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env>>

Run(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "staged"
  /\ harnessAlive[c] = 0
  /\ slotsInUse < MaxSlots
  /\ slotsInUse' = slotsInUse   \* FAULT: slot freed while harness lives
  /\ harnessAlive' = [harnessAlive EXCEPT ![c] = 1]
  /\ jobState' = [jobState EXCEPT ![c] = "running"]
  /\ UNCHANGED <<storeState, width, jobDir, appliedOps, env>>

Die(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "running"
  /\ harnessAlive[c] = 1
  /\ harnessAlive' = [harnessAlive EXCEPT ![c] = 0]
  /\ slotsInUse' = slotsInUse - 1
  /\ jobDir' = [jobDir EXCEPT ![c].outcome = "fault", ![c].bounced = FALSE]   \* Die is a fault, never a fail that bounces
  /\ jobState' = [jobState EXCEPT ![c] = "harvested"]
  /\ UNCHANGED <<storeState, width, appliedOps, env>>

Harvest(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "running"
  /\ harnessAlive[c] = 1
  /\ harnessAlive' = [harnessAlive EXCEPT ![c] = 0]
  /\ slotsInUse' = slotsInUse - 1   \* slot freed only when harness ends
  /\ jobDir' = [jobDir EXCEPT ![c].outcome = "ok"]
  /\ jobState' = [jobState EXCEPT ![c] = "harvested"]
  /\ UNCHANGED <<storeState, width, appliedOps, env>>

Rebase(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "harvested"
  /\ jobDir[c].outcome = "ok"
  /\ jobState' = [jobState EXCEPT ![c] = "rebased"]
  /\ UNCHANGED <<storeState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env>>

Conflict(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "harvested"
  /\ jobDir[c].outcome = "ok"
  /\ jobDir' = [jobDir EXCEPT ![c].outcome = "fail", ![c].bounced = TRUE]
  /\ jobState' = [jobState EXCEPT ![c] = "rebased"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env>>

Push(c) ==
  /\ ~env.crashed
  /\ jobState[c] \in {"harvested", "rebased"}
  /\ (jobState[c] = "harvested" => jobDir[c].outcome # "ok")
  /\ jobDir' = [jobDir EXCEPT ![c].pushed = TRUE, ![c].resId = IF @ = 0 THEN ResIDFor(c) ELSE @]
  /\ jobState' = [jobState EXCEPT ![c] = IF jobDir[c].outcome = "ok" THEN "ciwait" ELSE "reporting"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env>>

CIReady(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "ciwait"
  /\ jobState' = [jobState EXCEPT ![c] = "reporting"]
  /\ UNCHANGED <<storeState, slotsInUse, width, jobDir, harnessAlive, appliedOps, env>>

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
  /\ UNCHANGED <<slotsInUse, width, jobDir, harnessAlive, env>>

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
  /\ UNCHANGED <<jobState, slotsInUse, width, harnessAlive>>

Clean(c) ==
  /\ ~env.crashed
  /\ jobState[c] = "recorded"   \* clean only after report is recorded and head pushed
  /\ storeState[c] \in {"done", "fail"}
  /\ jobDir[c].pushed
  /\ jobDir' = [jobDir EXCEPT ![c].exists = FALSE]
  /\ jobState' = [jobState EXCEPT ![c] = "cleaned"]
  /\ UNCHANGED <<storeState, slotsInUse, width, harnessAlive, appliedOps, env>>

Withdraw(c) ==
  /\ ~env.crashed
  /\ ~env.storeDown
  /\ storeState[c] \in {"ready", "working"}
  /\ jobState[c] \in {"none", "taken", "staged", "harvested", "rebased", "ciwait"}
  /\ harnessAlive[c] = 0
  /\ storeState' = [storeState EXCEPT ![c] = "fail"]
  /\ jobDir' = [jobDir EXCEPT ![c].pushed = IF jobDir[c].exists THEN TRUE ELSE jobDir[c].pushed, ![c].exists = FALSE]
  /\ jobState' = [jobState EXCEPT ![c] = "cleaned"]
  /\ UNCHANGED <<slotsInUse, width, harnessAlive, appliedOps, env>>

Crash ==
  /\ ~env.crashed
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1, !.crashed = TRUE]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps>>

Restart ==
  /\ env.crashed
  /\ env' = [env EXCEPT !.crashed = FALSE, !.storeDown = FALSE]
  /\ harnessAlive' = harnessAlive   \* adopt live harnesses by pid and nonce, never spawn a second
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, appliedOps>>

StoreDown ==
  /\ ~env.storeDown
  /\ env.crashes < MaxCrashes
  /\ env' = [env EXCEPT !.crashes = @ + 1, !.storeDown = TRUE]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps>>

StoreUp ==
  /\ env.storeDown
  /\ env' = [env EXCEPT !.storeDown = FALSE]
  /\ UNCHANGED <<storeState, jobState, slotsInUse, width, jobDir, harnessAlive, appliedOps>>

Done ==
  /\ \A c \in Consumers : jobState[c] = "cleaned"
  /\ UNCHANGED vars

Next ==
  \/ Wake
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
       \/ CIReady(c)
       \/ Report(c)
       \/ ReplyLost(c)
       \/ Clean(c)
       \/ Withdraw(c)
  \/ Crash
  \/ Restart
  \/ StoreDown
  \/ StoreUp
  \/ Done

FairConsumer(c) ==
  /\ WF_vars(Take(c))
  /\ WF_vars(Stage(c))
  /\ WF_vars(Run(c))
  /\ WF_vars(Harvest(c))
  /\ WF_vars(Rebase(c))
  /\ WF_vars(Push(c))
  /\ WF_vars(CIReady(c))
  /\ WF_vars(Report(c))
  /\ WF_vars(Clean(c))

Spec == Init /\ [][Next]_vars /\ WF_vars(Restart) /\ WF_vars(StoreUp) /\ FairConsumer("c1") /\ FairConsumer("c2")

TypeOK ==
  /\ storeState \in [Consumers -> StoreStates]
  /\ jobState \in [Consumers -> JobStates]
  /\ slotsInUse \in 0..2
  /\ width = Width

OneLiveHarnessPerJobDir ==
  \A c \in Consumers : harnessAlive[c] <= 1 /\ (harnessAlive[c] > 0 => jobDir[c].exists)

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

Liveness ==
  \A c \in Consumers : [](storeState[c] = "working" => <>(jobState[c] \in {"recorded", "cleaned"}))
=============================================================================
