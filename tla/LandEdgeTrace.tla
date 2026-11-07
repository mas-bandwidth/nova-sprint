---------------------------- MODULE LandEdgeTrace ----------------------------
(***************************************************************************)
(* Trace validation of LandEdge.tla: LandEdge.tla's next-state relation    *)
(* constrained to a recording of the Go git/forge landing walk             *)
(* (edge/git/trace_test.go).                                               *)
(***************************************************************************)
EXTENDS LandEdge, Sequences, LandEdgeTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

MatchState(k) ==
  /\ base = TraceLog[k].base
  /\ stream = TraceLog[k].stream
  /\ group = TraceLog[k].group
  /\ step = TraceLog[k].step
  /\ gatedAt = TraceLog[k].gatedAt
  /\ landed = TraceLog[k].landed

MatchNextState(k) ==
  /\ base' = TraceLog[k].base
  /\ stream' = TraceLog[k].stream
  /\ group' = TraceLog[k].group
  /\ step' = TraceLog[k].step
  /\ gatedAt' = TraceLog[k].gatedAt
  /\ landed' = TraceLog[k].landed

TraceAction(k) ==
  LET a == TraceLog[k].action
      s == TraceLog[k].arg
  IN CASE a = "Fetch"         -> Fetch(s)
       [] a = "Merge"         -> Merge(s)
       [] a = "MergeConflict" -> MergeConflict(s)
       [] a = "PushStream"    -> PushStream(s)
       [] a = "Gate"          -> Gate(s)
       [] a = "CIGreen"       -> CIGreen(s)
       [] a = "CIRed"         -> CIRed(s)
       [] a = "CIFault"       -> CIFault(s)
       [] a = "BaseMoves"     -> BaseMoves
       [] a = "Land"          -> Land(s)
       [] a = "Remerge"       -> Remerge(s)
       [] a = "Refuse"        -> Refuse(s)
       [] a = "Done"          -> Done
       [] OTHER               -> FALSE

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
