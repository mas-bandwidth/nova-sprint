----------------------------- MODULE HookTrace -----------------------------
(***************************************************************************)
(* Trace validation of Hook.tla: Hook.tla's next-state relation            *)
(* constrained to a recording of the Go verbs                              *)
(* (modules/verbs/hook_trace_test.go). Each record names a step (the bus   *)
(* hook started or stopped, a hook, an unhook, an acting verb), says       *)
(* whether the code accepted it (ok), and carries the state after: live    *)
(* (the fake bus's id for each name), rec and role (the store's rows) and  *)
(* hand (whether the row holds live cards). A step whose answer or state   *)
(* the model does not give has no successor: TraceMatched is violated and  *)
(* l is the last record matched.                                           *)
(***************************************************************************)
EXTENDS Hook, Sequences, HookTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ live = TraceLog[k].live
  /\ rec = TraceLog[k].rec
  /\ role = TraceLog[k].role
  /\ hand = TraceLog[k].hand

MatchNextState(k) ==
  /\ live' = TraceLog[k].live
  /\ rec' = TraceLog[k].rec
  /\ role' = TraceLog[k].role
  /\ hand' = TraceLog[k].hand

TraceAction(k) ==
  LET a  == TraceLog[k].action
      n  == TraceLog[k].name
      r  == TraceLog[k].arg
      ok == TraceLog[k].ok
  IN CASE a = "BusStart" -> BusStart(n)
       [] a = "BusStop"  -> BusStop(n)
       [] a = "Hook"     -> Hook(n, r, ok)
       [] a = "Unhook"   -> Unhook(n, ok)
       [] a = "Verb"     -> Verb(n, r, ok)
       [] OTHER          -> FALSE

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
