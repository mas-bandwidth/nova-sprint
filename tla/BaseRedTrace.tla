---------------------------- MODULE BaseRedTrace ----------------------------
(***************************************************************************)
(* Trace validation of BaseRed.tla: BaseRed.tla's next-state relation      *)
(* constrained to a recording of the Go walk                               *)
(* (modules/run/basered_trace_test.go: the run's Ticker on the store fake, *)
(* its BaseWatch reading a fake forge's checks of each new base tip, the   *)
(* live cards, the coordinator's answers and the clock passing deadlines   *)
(* and quiets). The walk records Read's arg as the model's read: running   *)
(* checks, no checks yet and a rate-limited forge are all "pending".       *)
(***************************************************************************)
EXTENDS BaseRed, Sequences, BaseRedTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ fact = TraceLog[k].fact
  /\ live = TraceLog[k].live
  /\ js = TraceLog[k].js
  /\ quiet = TraceLog[k].quiet
  /\ due = TraceLog[k].due
  /\ ticked = TraceLog[k].ticked
  /\ env = TraceLog[k].env

MatchNextState(k) ==
  /\ fact' = TraceLog[k].fact
  /\ live' = TraceLog[k].live
  /\ js' = TraceLog[k].js
  /\ quiet' = TraceLog[k].quiet
  /\ due' = TraceLog[k].due
  /\ ticked' = TraceLog[k].ticked
  /\ env' = TraceLog[k].env

TraceAction(k) ==
  LET a == TraceLog[k].action
  IN CASE a = "Tick"      -> Tick
       [] a = "Read"      -> Read(TraceLog[k].arg)
       [] a = "Cards"     -> Cards
       [] a = "Unwatch"   -> Unwatch
       [] a = "Answer"    -> Answer
       [] a = "QuietEnds" -> QuietEnds
       [] a = "Lapse"     -> Lapse
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
