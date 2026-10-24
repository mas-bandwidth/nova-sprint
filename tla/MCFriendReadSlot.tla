---------------------------- MODULE MCFriendReadSlot ----------------------------
\* The instance of FriendReadSlot.tla (tla/CASES.tsv, group friendreadslot): two reads, the
\* start bound one tick, three friends (c the author) and a fleet member, each of width one.
\* MCFriendReadSlot.cfg holds every property; each Broken cfg turns one rule off and TLC must
\* find the counterexample.
EXTENDS FriendReadSlot
=============================================================================
