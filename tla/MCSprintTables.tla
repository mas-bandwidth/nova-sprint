-------------------------- MODULE MCSprintTables ---------------------------
\* The instances of SprintTables.tla (tla/CASES.tsv, group sprinttables): two
\* primaries (p1 flash, p2 pro), three readers (r1, r2, r3). p1's worker is r1
\* and p2's is r2, so a read is never asked of its own worker
\* (NeverReadByItsWorker).
EXTENDS SprintTables

MCPrimary == {"p1", "p2"}
MCReader  == {"r1", "r2", "r3"}
MCFlash   == {"p1"}
=============================================================================
