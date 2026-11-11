------------------------- MODULE BaseRedQuietSwallows -------------------------
\* REVERSED WITNESS of BaseRed.tla: base-red's first answered quiet (#793
\* before its second cold read), keyed by the red set only. Answered, then a
\* green read, then the same checks red again inside the judgement timeout:
\* a new red stretch, left unpushed until the timeout. RedIsPushed must be
\* violated.
EXTENDS BaseRed

QuietOld == Last.st = "answered" /\ Last.set = fact /\ quiet

TickOld ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ due' = FALSE
  /\ IF Red /\ live
       THEN CASE Last.st = "open" /\ Last.set = fact /\ due -> js' = Expire \o Open(fact)
              [] Last.st = "open" /\ Last.set = fact /\ ~due -> UNCHANGED js
              [] Last.st = "open" /\ Last.set # fact -> js' = Expire \o Open(fact)
              [] QuietOld -> UNCHANGED js
              [] ~(Last.st = "open" \/ QuietOld) -> js' = js \o Open(fact)
     ELSE IF Last.st = "open" /\ (fact = "green" \/ due)
       THEN js' = Expire
       ELSE UNCHANGED js
  /\ UNCHANGED <<fact, live, quiet, broke, env>>

NextOld == TickOld \/ (\E r \in Reads : Read(r)) \/ Cards \/ Unwatch \/ Answer \/ QuietEnds \/ Lapse

SpecOld == Init /\ [][NextOld]_vars /\ WF_vars(TickOld)
=============================================================================
