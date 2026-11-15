----------------------------- MODULE MergeNagOffBus -----------------------------
\* REVERSED WITNESS of MergeNag.tla: the nag ignores whether the
\* coordinator is on the bus, so the bus queues a line a second for nobody.
\* NoneOffBus must be violated.
EXTENDS MergeNag

TickBlind ==
  /\ pushed' = IF Old # {} THEN 1 ELSE 0
  /\ ticked' = TRUE
  /\ UNCHANGED <<st, coord>>

NextBlind ==
  \/ \E c \in Cards : Enter(c) \/ Leave(c)
  \/ \E S \in SUBSET Cards : Ages(S)
  \/ Hook
  \/ TickBlind

SpecBlind == Init /\ [][NextBlind]_vars /\ WF_vars(TickBlind)
=============================================================================
