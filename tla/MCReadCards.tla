------------------------------ MODULE MCReadCards ------------------------------
\* The instance of ReadCards.tla (tla/CASES.tsv, group readcards): one primary
\* in review, the worker and two readers, MaxGen 2 and Need from the cfg (0, 1 or
\* 2). BugIgnoreSpent = FALSE holds every invariant; TRUE is the reversed witness
\* (MCReadCardsBrokenIgnoreSpent.cfg) that breaks NeverTwiceAfterSpent.
EXTENDS ReadCards
=============================================================================
