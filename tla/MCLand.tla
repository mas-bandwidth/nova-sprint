------------------------------- MODULE MCLand -------------------------------
\* The instance: three cards as model values of up to two attempts, one clear (epochs 0 and 1), at
\* most four outside events (accepts, returns, reworks, another lander, a clear, the
\* base moving, a crash) in any order; the order of the cards matters (the
\* queue), so there is no symmetry.
\* The eject (land-ejects-a-bad-card-and-its-dependents): the instances before it
\* have no needs and no bad head (NoNeeds, NoBad); the eject's have c1's first
\* attempt that cannot be merged and c2 that needs c1 (EjectNeeds, EjectBad), so a
\* batch of c1, c2, c3 ejects c1 and c2 and lands c3, and a rework of c1 mends it.
EXTENDS Land, TLC

CONSTANTS c1, c2, c3

NoNeeds == [c \in Cards |-> {}]
NoBad == {}

EjectNeeds == [c \in Cards |-> IF c = c2 THEN {c1} ELSE {}]
EjectBad == {<<c1, 1>>}
=============================================================================
