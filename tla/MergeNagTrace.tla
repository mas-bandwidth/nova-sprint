---------------------------- MODULE MergeNagTrace ----------------------------
(***************************************************************************)
(* Trace validation of MergeNag.tla: its next-state relation constrained  *)
(* to a recording of the Go nag walk (modules/steps/nagtrace_test.go:     *)
(* mergingNag on a View the walk moves, its clock an argument).          *)
(***************************************************************************)
EXTENDS MergeNag, Sequences, MergeNagTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ st = TraceLog[k].st
  /\ coord = TraceLog[k].coord
  /\ pushed = TraceLog[k].pushed
  /\ ticked = TraceLog[k].ticked

MatchNextState(k) ==
  /\ st' = TraceLog[k].st
  /\ coord' = TraceLog[k].coord
  /\ pushed' = TraceLog[k].pushed
  /\ ticked' = TraceLog[k].ticked

TraceAction(k) ==
  LET a == TraceLog[k].action
      x == TraceLog[k].arg
  IN CASE a = "Tick"  -> Tick
       [] a = "Enter" -> Enter(x)
       [] a = "Leave" -> Leave(x)
       [] a = "Ages"  -> \E S \in SUBSET Cards : Ages(S)
       [] a = "Hook"  -> Hook
       [] OTHER       -> FALSE

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
