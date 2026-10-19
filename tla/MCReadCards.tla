------------------------------ MODULE MCReadCards ------------------------------
\* The instance of ReadCards.tla (tla/CASES.tsv, group readcards): one primary
\* in review, the worker and two readers, MaxGen 2 and Need from the cfg (0, 1 or
\* 2). BugIgnoreSpent = FALSE holds every invariant; TRUE is the reversed witness
\* (MCReadCardsBrokenIgnoreSpent.cfg) that breaks NeverTwiceAfterSpent;
\* BugNamesHeld and BugNoLiveReturn each break NoGhostRead (v1.2.14), under LiveSpec.
\* Findings = TRUE (MCReadCardsFindings.cfg) checks the findings of a reworked attempt:
\* NoFindingLost, which BugDropLate and BugReworkAtFirstBroken each break
\* (MCReadCardsFindingsBrokenDropLate.cfg, MCReadCardsFindingsBrokenReworkAtFirstBroken.cfg).
\* BugWidenAtFirstBroken (a PATHS widen) and BugLateMissesRunning (a late read while the next
\* attempt runs) break it too (MCReadCardsFindingsBrokenWidenAtFirstBroken.cfg,
\* MCReadCardsFindingsBrokenLateMissesRunning.cfg).
EXTENDS ReadCards

\* the live reads' cfgs (Live = TRUE) check one attempt at the cfg's need: the beats and the
\* children of the cards multiply the deal's space past the bench budget otherwise
LiveBound == attempt = 1 /\ need = Need
=============================================================================
