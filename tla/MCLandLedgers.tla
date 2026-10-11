---------------------------- MODULE MCLandLedgers ----------------------------
\* The instance: two cards, three paths (two shared ledgers and one code file), every
\* conflict set and every check outcome from Init, so each card's merge is clean,
\* ledger-only, code-only or mixed, with the checks passing or not.
EXTENDS LandLedgers, TLC

MCPaths == {"ledger1", "ledger2", "code"}
MCLedgers == {"ledger1", "ledger2"}
=============================================================================
