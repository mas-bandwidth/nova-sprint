--------------------------- MODULE MergeNagNeverStops ---------------------------
\* REVERSED WITNESS of MergeNag.tla: the nag keeps its last line when the
\* queue clears, so a coordinator is pested about cards already landed.
\* StopsWhenClear must be violated.
EXTENDS MergeNag

TickSticky ==
  /\ pushed' = IF Past THEN 1 ELSE pushed
  /\ ticked' = TRUE
  /\ UNCHANGED <<st, coord>>

NextSticky ==
  \/ \E c \in Cards : Enter(c) \/ Leave(c)
  \/ \E S \in SUBSET Cards : Ages(S)
  \/ Hook
  \/ TickSticky

SpecSticky == Init /\ [][NextSticky]_vars /\ WF_vars(TickSticky)
=============================================================================
