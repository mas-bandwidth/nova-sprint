---------------------------- MODULE MCRouteIndex ----------------------------
\* The instance: two tiers of three entries each (pro names a route twice), seven
\* cards as model values (four flash, two pro, one flash card that pins a model;
\* the control makes one pro card a read, the reader tier pro), one redeal each;
\* the work cards of a tier are interchangeable (the symmetry).
EXTENDS RouteIndex, TLC

CONSTANTS ProCards, BenchedCards, RefusedCards

\* m1 can launch no flash entry, m2 every entry; a benched card's bench is m1 alone,
\* and a refused card was refused at staging by m2, so dealt again its pool is m1.
MCMemberUnlaunch == [m \in Members |-> IF m = "m1" THEN {"a", "b", "c"} ELSE {}]
MCBenchOf == [c \in Cards |-> IF c \in BenchedCards THEN {"m1"} ELSE Members]
MCRefused == [c \in Cards |-> IF c \in RefusedCards THEN {"m2"} ELSE {}]

\* The design's resting routes, a card's avoided routes, and the read tier's
\* collapse: none, none, and identity (a read draws from its primary's tier).
MCRested == {}
MCAvoid == [c \in Cards |-> {}]
MCReadTier == [t \in Tiers |-> t]

\* A case that exercises them (MCRouteIndexRead.cfg): b rests, the read c6 avoids
\* a, and a pro primary's read is drawn on flash (readTierOf's collapse).
MCRestedB == {"b"}
MCAvoidA == [c \in Cards |-> IF c \in Reads THEN {"a"} ELSE {}]
MCReadTierProFlash == [t \in Tiers |-> IF t = "pro" THEN "flash" ELSE t]

MCArr == [t \in {"flash", "pro"} |-> IF t = "flash" THEN <<"a", "b", "c">> ELSE <<"d", "e", "e">>]
MCTierOf == [c \in Cards |-> IF c \in ProCards THEN "pro" ELSE "flash"]

\* The frontier instance (MCRouteIndexFrontier.cfg): a flash tier and a frontier
\* tier of one route, f; ProCards are the frontier cards there.
MCArrF == [t \in {"flash", "frontier"} |-> IF t = "flash" THEN <<"a", "b", "c">> ELSE <<"f">>]
MCTierOfF == [c \in Cards |-> IF c \in ProCards THEN "frontier" ELSE "flash"]
\* a read card is no work card's twin: the reads are left out of the symmetry
MCSym == Permutations(Cards \ (ProCards \cup Pinned \cup Reads \cup BenchedCards \cup RefusedCards)) \cup Permutations(ProCards \ Reads)
=============================================================================
