---------------------------- MODULE MCCapDeal ----------------------------
\* The small instances of CapDeal: three cards, two friends (one frontier, one
\* heavy), one machine of width one, a cap of two attempts and a redeal bound of
\* three redeals, with the broken value each configuration turns on ("none" for the
\* design). The friends' classes and widths the plan needs are the operators
\* below, so a configuration names the same instance whichever witness it runs.
EXTENDS CapDeal

MCClass == [f \in Friends |-> IF f = "f1" THEN "frontier" ELSE "heavy"]
MCWidth == [f \in Friends |-> 1]
=============================================================================
