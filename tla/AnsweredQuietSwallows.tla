------------------------ MODULE AnsweredQuietSwallows ------------------------
\* REVERSED WITNESS of AnsweredQuiet.tla: the first answered quiet (#793
\* before its second cold read), keyed by kind and about only. A cause that
\* cleared after the answer and came back inside the judgement timeout stayed
\* quiet, unpushed, until the timeout. CauseIsPushed must be violated.
EXTENDS AnsweredQuiet

TickOld ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ IF cause
       THEN IF Last = "open" \/ (Last = "answered" /\ quiet)
              THEN UNCHANGED js
              ELSE js' = Append(js, "open")
       ELSE IF Last = "open" THEN js' = [js EXCEPT ![Len(js)] = "expired"] ELSE UNCHANGED js
  /\ See
  /\ UNCHANGED <<cause, quiet, env>>

NextOld == TickOld \/ Cause \/ Answer \/ QuietEnds

SpecOld == Init /\ [][NextOld]_vars /\ WF_vars(TickOld)
=============================================================================
