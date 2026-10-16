------------------------------- MODULE MCSprintTables ---------------------------
\* The instance: three cards, c2 a sentinel, c3 needing c1 and in line behind
\* the sentinel, two members m1 and m2 both up at the start, and three readers
\* r1, r2, r3 (r1 on m1, r2 on m2, r3 a friend). c1 is a flash card (one read)
\* and c3 a pro card (two reads). c1 starts ready (nothing to wait for) and is
\* dealt to m1, taken, finished, read by a reader other than its worker r1, and
\* accepted on one read; c2 the sentinel starts waiting and lands only by
\* release; c3 starts waiting for its need c1 and for the sentinel c2 to land
\* first, then is dealt, taken, finished, read by two readers other than its
\* worker, and accepted on two reads.
EXTENDS SprintTables, TLC

CONSTANTS c1, c2, c3, r1, r2, r3

MCCard == {c1, c2, c3}
MCSentinels == {c2}
MCNeedsOf == [c \in MCCard |-> IF c = c3 THEN {c1} ELSE {}]
MCScore == [c \in MCCard |-> IF c = c1 THEN 1 ELSE IF c = c2 THEN 2 ELSE 3]
MCInitOf == [c \in MCCard |-> IF c = c1 THEN "ready" ELSE "waiting"]

MCMember == {"m1", "m2"}
MCInitUp == {"m1", "m2"}
MCStreamOf == [c \in MCCard |-> "s1"]

MCReader == {"r1", "r2", "r3"}
MCFlash == {c1}
MCMemberReader == [m \in MCMember |-> IF m = "m1" THEN "r1" ELSE "r2"]

\* The needs rewrites (needs set, a brief's DEPENDS-ON): c3 drops its need on
\* c1, and c1 gains a need on c3 (a cycle while c3 still needs c1; a need not
\* landed while c3 has not landed; landed once it has).
MCRewrites == {<<c3, {}>>, <<c1, {c3}>>}
\* The same rewrites with the clear of c1's needs added (needs set --clear): a
\* working card gains a need, then has its needs cleared (the go-table case).
MCRewritesClear == {<<c3, {}>>, <<c1, {c3}>>, <<c1, {}>>}
MCOneCard == {c1}
MCNoSentinels == {}
MCOneNeeds == [c \in {c1} |-> {}]
MCOneScore == [c \in {c1} |-> 1]
MCOneInit == [c \in {c1} |-> "ready"]
MCOneStream == [c \in {c1} |-> "s1"]
MCNoRewrites == {}
=============================================================================
