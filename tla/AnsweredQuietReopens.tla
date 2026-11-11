------------------------ MODULE AnsweredQuietReopens ------------------------
\* REVERSED WITNESS of AnsweredQuiet.tla: judge before 2026-11-10. An
\* answered judgement whose cause still holds opened its next episode at the
\* next tick. NoReopenInQuiet must be violated.
EXTENDS AnsweredQuiet

TickOld ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ IF cause
       THEN IF Last = "open" THEN UNCHANGED js ELSE js' = Append(js, "open")
       ELSE IF Last = "open" THEN js' = [js EXCEPT ![Len(js)] = "expired"] ELSE UNCHANGED js
  /\ See
  /\ UNCHANGED <<cause, quiet, env>>

NextOld == TickOld \/ Cause \/ Answer \/ QuietEnds

SpecOld == Init /\ [][NextOld]_vars /\ WF_vars(TickOld)
=============================================================================
