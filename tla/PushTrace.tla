------------------------------ MODULE PushTrace ------------------------------
(***************************************************************************)
(* Trace validation of Push.tla: Push.tla's next-state relation            *)
(* constrained to a recording of the Go bus edge (edge/bus/trace_test.go). *)
(***************************************************************************)
EXTENDS Push, Sequences, PushTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ events = TraceLog[k].events
  /\ cursor = TraceLog[k].cursor
  /\ pending = TraceLog[k].pending
  /\ delivered = TraceLog[k].delivered
  /\ confirmed = TraceLog[k].confirmed
  /\ crashes = TraceLog[k].crashes

MatchNextState(k) ==
  /\ events' = TraceLog[k].events
  /\ cursor' = TraceLog[k].cursor
  /\ pending' = TraceLog[k].pending
  /\ delivered' = TraceLog[k].delivered
  /\ confirmed' = TraceLog[k].confirmed
  /\ crashes' = TraceLog[k].crashes

TraceAction(k) ==
  LET a == TraceLog[k].action
      r == TraceLog[k].arg
  IN CASE a = "CommitEvent"    -> CommitEvent
       [] a = "SkipIrrelevant" -> SkipIrrelevant(r)
       [] a = "StartSend"      -> StartSend(r)
       [] a = "SendConfirmed"  -> SendConfirmed(r)
       [] a = "SendFailed"     -> SendFailed(r)
       [] a = "ReplyLost"      -> ReplyLost(r)
       [] a = "Crash"          -> Crash
       [] a = "Idle"           -> Idle
       [] OTHER                -> FALSE

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
