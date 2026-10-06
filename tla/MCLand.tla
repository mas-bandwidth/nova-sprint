------------------------------- MODULE MCLand -------------------------------
\* The instance: three cards as model values of up to two attempts, one clear (epochs 0 and 1), at
\* most four outside events (accepts, returns, reworks, another lander, a clear, the
\* base moving, a crash) in any order; the order of the cards matters (the
\* queue), so there is no symmetry. The eject's needs: c2 needs c1 (MCNeeds),
\* and c3 needs c2 as well in the chain (MCNeedsChain); Bad names the cards whose
\* heads never merge.
EXTENDS Land, TLC

CONSTANTS c1, c2, c3

MCNeeds == [c \in Cards |-> IF c = c2 THEN {c1} ELSE {}]
MCNeedsChain == [c \in Cards |-> CASE c = c2 -> {c1} [] c = c3 -> {c2} [] OTHER -> {}]
=============================================================================
