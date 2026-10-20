----------------------- MODULE MCFleetNeverSits -----------------------
\* The instances of FleetNeverSits.tla (tla/CASES.tsv, group fleetneversits):
\* one card for the control and most reversed witnesses, two members of width
\* one, two routes of one tier, two readers, T = 1, N = 1, D = 1 and the
\* clock's bound 2. The noprogressdeadline witness uses two cards and one
\* member (MCCards, MCMembersOne): its second card stays read-less while a
\* reader is set away, so a later Deal is checked against NoAwayReaderHolds
\* there. The one-card, two-member cases keep each counterexample small. The
\* tags are one gate every member meets, so the gate cause is a value the model
\* can name but this instance does not force; the owner's evidence is a route
\* that makes no progress, a dealt card not started, and a reader that does not
\* take.
EXTENDS FleetNeverSits

MCCards == {"c1", "c2"}
MCSmallCards == {"c1"}
MCMembers == {"m1", "m2"}
MCMembersOne == {"m1"}
MCReaders == {"rd1", "rd2"}
MCRoutes == {"r1", "r2"}
MCRoutesOne == {"r1"}
MCT == 1
MCN == 1
MCD == 1
MCMaxTime == 2
MCMaxWidth == 1
MCCardTags == [c \in MCCards |-> {"go"}]
MCMemberTags == [m \in MCMembers |-> {"go"}]
====
