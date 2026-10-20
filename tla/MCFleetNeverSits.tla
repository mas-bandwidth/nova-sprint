----------------------- MODULE MCFleetNeverSits -----------------------
\* The instances of FleetNeverSits.tla (tla/CASES.tsv, group fleetneversits):
\* one card, two members of width two, two routes of one tier, two readers,
\* T = 2, N = 2, D = 1 and the clock's bound 4. The tags are one gate every
\* member meets, so the gate cause is a value the model can name but this
\* instance does not force; the owner's evidence is a route that makes no
\* progress, a dealt card not started, and a reader that does not take.
EXTENDS FleetNeverSits

MCCards == {"c1"}
MCMembers == {"m1", "m2"}
MCReaders == {"rd1", "rd2"}
MCRoutes == {"r1", "r2"}
MCT == 2
MCN == 2
MCD == 1
MCMaxTime == 4
MCMaxWidth == 2
MCCardTags == [c \in MCCards |-> {"go"}]
MCMemberTags == [m \in MCMembers |-> {"go"}]
====
