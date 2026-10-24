---------------------------- MODULE MCFinishVerdict ----------------------------
\* The instance of FinishVerdict.tla (tla/CASES.tsv, group finishverdict): two cards, each
\* report's verdict LAND, HOLD or none, at most two refusals a card. Broken = "none" holds
\* every invariant and HoldStillFinishes; Broken = "acceptshold" is the reversed witness
\* (MCFinishVerdictBrokenAcceptsHold.cfg) that breaks NoHoldFinishedOk.
EXTENDS FinishVerdict
=============================================================================
