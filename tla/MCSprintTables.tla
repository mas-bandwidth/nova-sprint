------------------------------- MODULE MCSprintTables ---------------------------
\* The instance: three cards, c2 a sentinel, c3 needing c1 and in line behind
\* the sentinel. c1 starts ready (nothing to wait for); c2 the sentinel starts
\* waiting and lands only by release; c3 starts waiting for its need c1 and for
\* the sentinel c2 to land first.
EXTENDS SprintTables, TLC

CONSTANTS c1, c2, c3

MCCard == {c1, c2, c3}
MCSentinels == {c2}
MCNeedsOf == [c \in MCCard |-> IF c = c3 THEN {c1} ELSE {}]
MCScore == [c \in MCCard |-> IF c = c1 THEN 1 ELSE IF c = c2 THEN 2 ELSE 3]
MCInitOf == [c \in MCCard |-> IF c = c1 THEN "ready" ELSE "waiting"]
=============================================================================
