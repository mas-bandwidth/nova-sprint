---------------------------- MODULE MCLiveRuns ----------------------------
EXTENDS LiveRuns
\* two cards on two rows: one row's take can die while the other's finishes
MCCards == {"c1", "c2"}
MCRows == {"r1", "r2"}
MCHome == [c \in MCCards |-> IF c = "c1" THEN "r1" ELSE "r2"]
\* r2 is a legacy row: its beat carries no live set (a worker before v1.2.6)
MCLegacy == {"r2"}
=============================================================================
