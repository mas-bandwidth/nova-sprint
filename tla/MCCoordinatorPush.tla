------------------------- MODULE MCCoordinatorPush -------------------------
\* Minute steps check the exact 30m reminder boundary alongside push safety.
EXTENDS CoordinatorPush
\* The stream witnesses fix the unrelated legacy state to isolate the event
\* channel. The full LiveSpec above still checks the legacy liveness property.
StreamNext == CommitTransition \/ LogPush \/ LoseWake \/ Look
StreamSpec == Init /\ [][StreamNext]_vars /\ WF_vars(Look) /\ WF_vars(LogPush)
=============================================================================
