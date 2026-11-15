---------------------------- MODULE MergeNagPerCard ----------------------------
\* REVERSED WITNESS of MergeNag.tla: one nudge per card past the threshold,
\* so a queue of 130 cards is 130 lines a second. OneLinePerTick must be
\* violated.
EXTENDS MergeNag

TickPerCard ==
  /\ pushed' = IF coord THEN Cardinality(Old) ELSE 0
  /\ ticked' = TRUE
  /\ UNCHANGED <<st, coord>>

NextPerCard ==
  \/ \E c \in Cards : Enter(c) \/ Leave(c)
  \/ \E S \in SUBSET Cards : Ages(S)
  \/ Hook
  \/ TickPerCard

SpecPerCard == Init /\ [][NextPerCard]_vars /\ WF_vars(TickPerCard)
=============================================================================
