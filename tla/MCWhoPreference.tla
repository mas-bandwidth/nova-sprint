------------------------ MODULE MCWhoPreference ------------------------
EXTENDS WhoPreference
\* the lanes part's cards: three work (one a fix) and three reads, and the
\* smaller set its liveness case runs on
MCLCards6 == {"w1", "w2", "x1", "r1", "r2", "r3"}
MCLCardsLive == {"r1"}
=============================================================================
