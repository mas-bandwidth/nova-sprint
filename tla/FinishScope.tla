------------------------------ MODULE FinishScope ------------------------------
EXTENDS Naturals
CONSTANT BugReadsFirst
VARIABLES phase, outside, judged, read, returned, machine
vars == <<phase, outside, judged, read, returned, machine>>
\* Scope boundary: machine members always supply evidence and fail closed on collection
\* errors. Legacy/manual/friend finishes may omit it and remain lander-checked.
\* Git evidence integrity and the LandScope parser are pinned by Go tests.
\* outside is the result of the shared E12 rule, after always-in-PATHS and amendments.
Init == /\ phase = "working" /\ outside \in BOOLEAN /\ machine \in BOOLEAN
        /\ judged = FALSE /\ read = FALSE /\ returned = FALSE
Finish == /\ phase = "working" /\ phase' = "review"
          /\ judged' = (machine /\ ~BugReadsFirst)
          /\ returned' = (machine /\ ~BugReadsFirst /\ outside)
          /\ UNCHANGED <<outside, read, machine>>
Judge == /\ phase = "review" /\ ~judged /\ judged' = TRUE
         /\ returned' = outside /\ UNCHANGED <<phase, outside, read, machine>>
Ask == /\ phase = "review" /\ ~returned
       /\ (~machine \/ BugReadsFirst \/ (judged /\ ~outside))
       /\ read' = TRUE /\ UNCHANGED <<phase, outside, judged, returned, machine>>
Widen == /\ returned /\ ~read /\ outside' = FALSE
         /\ returned' = FALSE /\ UNCHANGED <<phase, judged, read, machine>>
Conflict == /\ returned /\ ~read /\ phase' = "working"
            /\ returned' = FALSE /\ judged' = FALSE
            /\ UNCHANGED <<outside, read, machine>>
Next == Finish \/ Judge \/ Ask \/ Widen \/ Conflict
Spec == Init /\ [][Next]_vars
TypeOK == /\ phase \in {"working", "review"}
          /\ outside \in BOOLEAN /\ judged \in BOOLEAN
          /\ read \in BOOLEAN /\ returned \in BOOLEAN
NoOutsideReads == (machine /\ read) => ~outside
JudgedBeforeRead == (machine /\ read) => judged
=============================================================================
