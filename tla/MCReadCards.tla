------------------------------ MODULE MCReadCards ------------------------------
\* The instance of ReadCards.tla (tla/CASES.tsv, group readcards): one primary
\* in review, the worker and two readers, MaxGen 2 and Need from the cfg (0, 1 or
\* 2). BugIgnoreSpent = FALSE holds every invariant; TRUE is the reversed witness
\* (MCReadCardsBrokenIgnoreSpent.cfg) that breaks NeverTwiceAfterSpent;
\* BugNamesHeld breaks NoGhostRead and BugNoLiveReturn NamedOrWithinGrace (v1.2.14).
EXTENDS ReadCards

\* the live reads' cfgs (Live = TRUE) check one attempt at the cfg's need: the beats and the
\* children of the cards multiply the deal's space past the bench budget otherwise
LiveBound == attempt = 1 /\ need = Need
=============================================================================
