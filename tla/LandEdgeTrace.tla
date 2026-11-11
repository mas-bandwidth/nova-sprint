---------------------------- MODULE LandEdgeTrace ----------------------------
(***************************************************************************)
(* Trace validation of LandEdge.tla: LandEdge.tla's next-state relation    *)
(* constrained to a recording of the Go landing walk                       *)
(* (modules/edge/git/trace_test.go: LandPass on the git fake and a truthful       *)
(* forge, with the tick, outside commits and the coordinator's answers).  *)
(***************************************************************************)
EXTENDS LandEdge, Sequences, LandEdgeTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ base = TraceLog[k].base
  /\ cs = TraceLog[k].cs
  /\ req = TraceLog[k].req
  /\ js = TraceLog[k].js
  /\ held = TraceLog[k].held
  /\ ticked = TraceLog[k].ticked
  /\ passes = TraceLog[k].passes

MatchNextState(k) ==
  /\ base' = TraceLog[k].base
  /\ cs' = TraceLog[k].cs
  /\ req' = TraceLog[k].req
  /\ js' = TraceLog[k].js
  /\ held' = TraceLog[k].held
  /\ ticked' = TraceLog[k].ticked
  /\ passes' = TraceLog[k].passes

TraceAction(k) ==
  LET a == TraceLog[k].action
      x == TraceLog[k].arg
  IN CASE a = "Tick"      -> Tick
       [] a = "Pass"      -> Pass(x)
       [] a = "CIStep"    -> CIStep(x)
       [] a = "Late"      -> Late(x)
       [] a = "BaseMoves" -> BaseMoves
       [] a = "Answer"    -> Answer(x, TraceLog[k].choice)
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
