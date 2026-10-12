------------------------------- MODULE MCSprintTablesMerge -----------------------
\* The merge instance: two cards c1 (a flash primary) and c2 (a sentinel) in
\* the one stream s1, one member m1 up, two readers r1 and r2 (r1 on m1, r2 a
\* friend). c1 starts ready, is dealt and finished, read by r2 and accepted,
\* then merged, returned, redone, refused, stopped and resumed, ranked or
\* dropped; c2 the sentinel lands by release and lands the stream. It turns the
\* merge side on (MergeSide) and stores no needs.
EXTENDS SprintTables, TLC

CONSTANTS c1, c2

MCCard == {c1, c2}
MCSentinels == {c2}
MCNeedsOf == [c \in MCCard |-> {}]
MCScore == [c \in MCCard |-> IF c = c1 THEN 1 ELSE 2]
MCInitOf == [c \in MCCard |-> IF c = c1 THEN "ready" ELSE "waiting"]

MCMember == {"m1"}
MCInitUp == {"m1"}
MCStreamOf == [c \in MCCard |-> "s1"]

MCReader == {"r1", "r2"}
MCFlash == {c1}
MCMemberReader == [m \in MCMember |-> "r1"]
MCRewrites == {}
=============================================================================
