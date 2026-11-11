--------------------------- MODULE VerbFenceTrace ---------------------------
(***************************************************************************)
(* Trace validation of VerbFence.tla: its next-state relation constrained  *)
(* to a recording of the Go walk (modules/verbs/fence_trace_test.go: the   *)
(* replace verb on the Fake, the run's deals, landings and holds and other *)
(* writes landing between its reads and its commits).                      *)
(***************************************************************************)
EXTENDS VerbFence, Sequences, VerbFenceTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ old = TraceLog[k].old
  /\ new = TraceLog[k].new
  /\ dep = TraceLog[k].dep
  /\ x = TraceLog[k].x
  /\ pc = TraceLog[k].pc
  /\ snap = TraceLog[k].snap
  /\ first = TraceLog[k].first
  /\ tries = TraceLog[k].tries
  /\ env = TraceLog[k].env
  /\ at = TraceLog[k].at

MatchNextState(k) ==
  /\ old' = TraceLog[k].old
  /\ new' = TraceLog[k].new
  /\ dep' = TraceLog[k].dep
  /\ x' = TraceLog[k].x
  /\ pc' = TraceLog[k].pc
  /\ snap' = TraceLog[k].snap
  /\ first' = TraceLog[k].first
  /\ tries' = TraceLog[k].tries
  /\ env' = TraceLog[k].env
  /\ at' = TraceLog[k].at

TraceAction(k) ==
  LET a == TraceLog[k].action
  IN CASE a = "Read"  -> Read
       [] a = "Apply" -> Apply
       [] a = "Deal"  -> Deal /\ UNCHANGED <<dep, pc, snap, first, tries, at>>
       [] a = "Land"  -> Land /\ UNCHANGED <<dep, pc, snap, first, tries, at>>
       [] a = "Hold"  -> Hold /\ UNCHANGED <<dep, pc, snap, first, tries, at>>
       [] a = "Moved" -> Moved /\ UNCHANGED <<dep, pc, snap, first, tries, at>>
       [] a = "Touch" -> Touch /\ UNCHANGED <<dep, pc, snap, first, tries, at>>
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
