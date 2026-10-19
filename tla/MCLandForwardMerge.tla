---------------------- MODULE MCLandForwardMerge ----------------------
\* The forward-merge instance (Land.tla, NoDealAgainstStaleTip): a base takes a
\* recorded forward-merge while cards wait, and the coordinator rechecks each
\* card before it is dealt. ForwardMergeOn is off in every other instance, so
\* their state spaces are unchanged.
EXTENDS Land, TLC

MCFMOn == TRUE
=============================================================================
