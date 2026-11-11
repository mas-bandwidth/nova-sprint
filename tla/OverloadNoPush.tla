---------------------------- MODULE OverloadNoPush ----------------------------
\* REVERSED WITNESS of Overload.tla: the behaviour before 2026-11-10. The
\* tick samples the load, but no step opens an overload judgement, so a bench
\* over its bound for hours is seen on the board at most. OverloadIsPushed
\* must be violated.
EXTENDS Overload

TickOld ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ overT' = [r \in Rows |-> O2(r)]
  /\ underT' = [r \in Rows |-> U2(r)]
  /\ fact' = [r \in Rows |-> Fact(r)]
  /\ sustained' = [r \in Rows |-> IF O2(r) >= Span THEN TRUE ELSE IF U2(r) >= Span THEN FALSE ELSE sustained[r]]
  /\ UNCHANGED <<cur, up, js, quiet, env>>

NextOld == TickOld \/ Again \/ \E r \in Rows : Beat(r) \/ UpDown(r) \/ Answer(r) \/ QuietEnds(r) \/ Lapse(r)

SpecOld == Init /\ [][NextOld]_vars /\ WF_vars(TickOld)
=============================================================================
