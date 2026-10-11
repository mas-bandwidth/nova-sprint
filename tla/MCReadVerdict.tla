------------------------------ MODULE MCReadVerdict ------------------------------
\* The instance of ReadVerdict.tla (tla/CASES.tsv, group readverdict): one primary, its
\* worker's finished card, two readers on either table, Need from the cfg (1 or 2).
\* BugReadersOnly = FALSE holds every invariant and AcceptedWorkCountsOk; TRUE is the
\* reversed witness (MCReadVerdictBrokenReadersOnly.cfg) that breaks AcceptedWorkCountsOk.
EXTENDS ReadVerdict
=============================================================================
