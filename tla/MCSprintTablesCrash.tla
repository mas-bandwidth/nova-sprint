------------------------------- MODULE MCSprintTablesCrash -----------------------
\* The crash instance: one flash primary c1 (ready at the start), two members
\* m1 and m2 both up, two readers r1 and r2 (r1 on m1, r2 on m2). c1 is dealt,
\* taken, finished, read by a reader other than its worker and accepted, so a
\* crash can cut each step that writes more than one table: Start (the deal),
\* Finish, FleetDown and Drop. No needs, no sentinels. It runs CrashSpec, which
\* adds the outside events and the repair to the ordinary steps.
EXTENDS SprintTables, TLC

CONSTANTS c1

MCCard == {c1}
MCSentinels == {}
MCNeedsOf == [c \in MCCard |-> {}]
MCScore == [c \in MCCard |-> 1]
MCInitOf == [c \in MCCard |-> "ready"]

MCMember == {"m1", "m2"}
MCInitUp == {"m1", "m2"}
MCStreamOf == [c \in MCCard |-> "s1"]

MCReader == {"r1", "r2"}
MCFlash == {c1}
MCMemberReader == [m \in MCMember |-> IF m = "m1" THEN "r1" ELSE "r2"]
MCRewrites == {}
=============================================================================
