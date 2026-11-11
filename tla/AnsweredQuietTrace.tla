------------------------- MODULE AnsweredQuietTrace -------------------------
(***************************************************************************)
(* Trace validation of AnsweredQuiet.tla: its next-state relation          *)
(* constrained to a recording of the Go walk                               *)
(* (modules/run/answeredquiet_trace_test.go: the run's Ticker on the store *)
(* fake, an admission cause turned on and off from outside, the            *)
(* coordinator's answers and the clock passing the quiet). broke is not    *)
(* recorded: it is what the model says a tick has seen, so the trace       *)
(* matches only if the code's quiet ends where the model's does.           *)
(***************************************************************************)
EXTENDS AnsweredQuiet, Sequences, AnsweredQuietTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ cause = TraceLog[k].cause
  /\ js = TraceLog[k].js
  /\ quiet = TraceLog[k].quiet
  /\ ticked = TraceLog[k].ticked
  /\ env = TraceLog[k].env

MatchNextState(k) ==
  /\ cause' = TraceLog[k].cause
  /\ js' = TraceLog[k].js
  /\ quiet' = TraceLog[k].quiet
  /\ ticked' = TraceLog[k].ticked
  /\ env' = TraceLog[k].env

TraceAction(k) ==
  LET a == TraceLog[k].action
  IN CASE a = "Tick"      -> Tick
       [] a = "Cause"     -> Cause
       [] a = "Answer"    -> Answer
       [] a = "QuietEnds" -> QuietEnds
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
