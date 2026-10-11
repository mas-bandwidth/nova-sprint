------------------------------ MODULE MCLandBound ------------------------------
\* The bound's instance (Land.tla, THE BOUND): one card, the bound on, a brief cap
\* of three attempts, one widening, two files a regression may need outside the
\* first PATHS, no clear of the epoch, two outside events (the accept that queues
\* the card, and one more of any kind). MaxAttempts is MaxWidens * (Cap - 1) +
\* Cap = 5: every failure counts toward it.
EXTENDS Land, TLC

MCWaysOn == TRUE
MCCap == 3
MCMaxWidens == 1
MCFiles == {"f1", "f2"}
=============================================================================
