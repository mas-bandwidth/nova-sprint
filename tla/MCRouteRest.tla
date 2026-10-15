------------------------------ MODULE MCRouteRest ------------------------------
EXTENDS RouteRest
\* Replay the v4-direct storm alone, at the production 3-in-10 threshold.
\* Fair silent ends prevent stuttering from manufacturing a failure.
SilentNext == \E r \in Routes : TakeEnds(r, "silent")
SilentSpec == Init /\ [][SilentNext]_vars /\ WF_vars(SilentNext)
SilentProviderEventuallyRests == <>(\A r \in Routes : own[r] = "provider")
=============================================================================
