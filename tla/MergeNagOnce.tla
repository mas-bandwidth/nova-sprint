------------------------------ MODULE MergeNagOnce ------------------------------
\* REVERSED WITNESS of MergeNag.tla: the behaviour before 2026-11-15. A
\* card past the threshold is said once (the stall judgement's push) and
\* then the queue sits quiet. PushedWhilePast must be violated.
EXTENDS MergeNag

TickOnce ==
  /\ pushed' = IF Past /\ pushed = 0 THEN 1 ELSE 0
  /\ ticked' = TRUE
  /\ UNCHANGED <<st, coord>>

NextOnce ==
  \/ \E c \in Cards : Enter(c) \/ Leave(c)
  \/ \E S \in SUBSET Cards : Ages(S)
  \/ Hook
  \/ TickOnce

SpecOnce == Init /\ [][NextOnce]_vars /\ WF_vars(TickOnce)
=============================================================================
