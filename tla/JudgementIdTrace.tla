-------------------------- MODULE JudgementIdTrace --------------------------
(***************************************************************************)
(* Trace validation of JudgementId.tla: its next-state relation            *)
(* constrained to a recording of the Go walk                               *)
(* (modules/steps/episodes_trace_test.go: the steps' own Tick on the store *)
(* fake, applied op by op through the store's check; one row's flap        *)
(* judgements for ByCount, one stream's admission judgements for not; the  *)
(* flaps, the cause, the coordinator's answers and the deadline moved from *)
(* outside). js is read back from the store after every move: the episode  *)
(* from the id, the state, and the count the question reports. last is     *)
(* not recorded: it is what the model says the tick's op was, so a trace   *)
(* matches only if the code's tick wrote what the model's valid op writes. *)
(*                                                                         *)
(* What this module adds to its model: the walk starts on a store of its   *)
(* own (any ids, any states, as a store written by an older planner holds  *)
(* them), so TraceInit takes js and count from the first record instead of *)
(* Init's Stores; MaxCount and MaxEnv are lifted in the cfg.               *)
(***************************************************************************)
EXTENDS JudgementId, Sequences, JudgementIdTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ js = TraceLog[k].js
  /\ count = TraceLog[k].count
  /\ cause = TraceLog[k].cause
  /\ due = TraceLog[k].due
  /\ ticked = TraceLog[k].ticked
  /\ env = TraceLog[k].env

MatchNextState(k) ==
  /\ js' = TraceLog[k].js
  /\ count' = TraceLog[k].count
  /\ cause' = TraceLog[k].cause
  /\ due' = TraceLog[k].due
  /\ ticked' = TraceLog[k].ticked
  /\ env' = TraceLog[k].env

TraceAction(k) ==
  LET a == TraceLog[k].action
  IN CASE a = "Tick"     -> Tick
       [] a = "Flap"     -> Flap
       [] a = "Cause"    -> Cause
       [] a = "Answer"   -> \E j \in js : Answer(j)
       [] a = "Deadline" -> Deadline
       [] OTHER          -> FALSE

TraceInit ==
  /\ l = 1
  /\ last = Clean
  /\ MatchState(1)
  /\ ~ticked /\ ~due /\ env = 0

TraceNext ==
  /\ l < Len(TraceLog)
  /\ l' = l + 1
  /\ TraceAction(l + 1)
  /\ MatchNextState(l + 1)

TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)

TraceMatched == <>(l = Len(TraceLog))
=============================================================================
