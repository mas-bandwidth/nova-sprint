---------------------------- MODULE MCDealFill ----------------------------
\* The 2026-10-10 incident, small: D is a friend that is up and never takes (her lanes
\* exit 126), first in the deal's order; R is a machine reader that is down holding asked
\* reads; L is a live machine that works and reads. Two cards, each needing one read.
EXTENDS DealFill

MCMembers == {"D", "R", "L"}
MCFriends == {"D"}
MCPref == <<"D", "R", "L">>
MCWorkers == {"D", "L"}
MCReaders == {"D", "R", "L"}
MCUp == {"D", "L"}
MCAble == {"L"}
=============================================================================
