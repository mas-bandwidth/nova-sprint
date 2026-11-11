---------------------------- MODULE BaseRedNoPush ----------------------------
\* REVERSED WITNESS of BaseRed.tla: the behaviour before 2026-11-10. The tick
\* runs, but no step ever opens a base-red judgement, so a base red for days
\* is met card by card and never pushed. RedIsPushed must be violated.
EXTENDS BaseRed

TickOld == /\ ~ticked /\ ticked' = TRUE /\ UNCHANGED <<fact, live, js, quiet, broke, due, env>>

NextOld == TickOld \/ (\E r \in Reads : Read(r)) \/ Cards \/ Unwatch \/ Answer \/ QuietEnds \/ Lapse

SpecOld == Init /\ [][NextOld]_vars /\ WF_vars(TickOld)
=============================================================================
