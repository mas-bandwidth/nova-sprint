----------------------------- MODULE RunnerTrace -----------------------------
(***************************************************************************)
(* Trace validation of Runner.tla: Runner.tla's next-state relation        *)
(* constrained to a recording of the Go worker walk                        *)
(* (modules/edge/worker/trace_test.go).                                            *)
(***************************************************************************)
EXTENDS Runner, Sequences, RunnerTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ storeState = TraceLog[k].storeState
  /\ jobState = TraceLog[k].jobState
  /\ slotsInUse = TraceLog[k].slotsInUse
  /\ width = TraceLog[k].width
  /\ jobDir = TraceLog[k].jobDir
  /\ harnessAlive = TraceLog[k].harnessAlive
  /\ appliedOps = TraceLog[k].appliedOps
  /\ env = TraceLog[k].env
  /\ pass = TraceLog[k].pass
  /\ branch = TraceLog[k].branch

MatchNextState(k) ==
  /\ storeState' = TraceLog[k].storeState
  /\ jobState' = TraceLog[k].jobState
  /\ slotsInUse' = TraceLog[k].slotsInUse
  /\ width' = TraceLog[k].width
  /\ jobDir' = TraceLog[k].jobDir
  /\ harnessAlive' = TraceLog[k].harnessAlive
  /\ appliedOps' = TraceLog[k].appliedOps
  /\ env' = TraceLog[k].env
  /\ pass' = TraceLog[k].pass
  /\ branch' = TraceLog[k].branch

TraceAction(k) ==
  LET a == TraceLog[k].action
      c == TraceLog[k].arg
  IN CASE a = "Wake"      -> Wake
       [] a = "Take"      -> Take(c)
       [] a = "Stage"     -> Stage(c)
       [] a = "StageFail" -> StageFail(c)
       [] a = "Run"       -> Run(c)
       [] a = "Spawn"     -> Spawn(c)
       [] a = "Die"       -> Die(c)
       [] a = "Harvest"   -> Harvest(c)
       [] a = "Rebase"    -> Rebase(c)
       [] a = "NothingNew" -> NothingNew(c)
       [] a = "Conflict"  -> Conflict(c)
       [] a = "Push"      -> Push(c)
       [] a = "Report"    -> Report(c)
       [] a = "ReplyLost" -> ReplyLost(c)
       [] a = "Clean"     -> Clean(c)
       [] a = "Withdraw"  -> Withdraw(c)
       [] a = "Retire"    -> Retire(c)
       [] a = "HolderPush" -> HolderPush(c)
       [] a = "HolderTake" -> HolderTake(c)
       [] a = "PassStart" -> PassStart
       [] a = "PassEnd"   -> PassEnd
       [] a = "Stop"      -> Stop
       [] a = "Crash"     -> Crash
       [] a = "KillLeftover" -> KillLeftover(c)
       [] a = "UnhookRefused" -> UnhookRefused
       [] a = "Exit"      -> Exit
       [] a = "Restart"   -> Restart
       [] a = "StoreDown" -> StoreDown
       [] a = "StoreUp"   -> StoreUp
       [] a = "Done"      -> Done
       [] OTHER           -> FALSE

TraceInit ==
  /\ l = 1
  /\ Init
  /\ MatchState(1)

TraceNext ==
  /\ l < Len(TraceLog)
  /\ l' = l + 1
  /\ TraceAction(l + 1)
  /\ MatchNextState(l + 1)

TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)

TraceMatched == <>(l = Len(TraceLog))
=============================================================================
