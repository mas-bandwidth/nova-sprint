------------------------------ MODULE MCLandPass ------------------------------
\* The instance: three streams in priority order, two files; the first two streams touch
\* the same file (they meet when both land in one pass), the third touches the other
\* (disjoint from both); the oracle over every tree is enumerated at Init, so every way
\* a tree can be green alone and red combined is a behaviour.
EXTENDS LandPass, TLC

CONSTANTS s1, s2, s3

MCStreams == {s1, s2, s3}
MCOrder == <<s1, s2, s3>>
MCFiles == {"f1", "f2"}
MCTouches == [s \in MCStreams |-> IF s = s3 THEN {"f2"} ELSE {"f1"}]
MCHeads == {1, 2}
\* The blame the bisection finds when a batch's tree is red: s1 blames head 2 (the prefix
\* before it lands, head 2 reworked), s2 is refused with no head blamed (Unblamed), s3's
\* tree tests read none of its files so it lands whole (Whole).
MCBlame == [s \in MCStreams |-> IF s = s1 THEN 2 ELSE IF s = s2 THEN Unblamed ELSE Whole]
\* Routing bound: two streams, three singleton batches (two in s1). Both add-before-
\* migration and add-after-migration, every lifecycle column and both cert results.
MCFlowStreams == {s1, s2}
MCFlowOrder == <<s1, s2>>
MCFlowTouches == [s \in MCFlowStreams |-> {"f1"}]
MCFlowHeads == {1, 2}
MCFlowBlame == [s \in MCFlowStreams |-> Whole]
=============================================================================
