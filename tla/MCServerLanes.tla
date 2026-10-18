---------------------------- MODULE MCServerLanes ----------------------------
EXTENDS ServerLanes
\* safety: a beat, a read, a dashboard read, a write, a begin and a queue (the begin
\* and the queue take the line), one reader card
MCCallers == {"b1", "r1", "w1", "b2", "q1", "d1"}
\* liveness: a beat, a read and a write, a tick (the dashboard read is safety only: with
\* it the liveness run outgrows its 110 s budget)
MCCallersLive == {"b1", "r1", "w1"}
MCCards == {"c1"}
MCCardsLive == {"c1"}
MCKindOf == [r \in MCCallers |->
               IF r = "b1" THEN "beat"
               ELSE IF r = "r1" THEN "read"
               ELSE IF r = "b2" THEN "begin"
               ELSE IF r = "q1" THEN "queue"
               ELSE IF r = "d1" THEN "dash"
               ELSE "write"]
MCKindOfLive == [r \in MCCallersLive |-> MCKindOf[r]]
\* no fleet beat among the callers above: Changes is FALSE for each
MCChanges == [r \in MCCallers |-> FALSE]
MCChangesLive == [r \in MCCallersLive |-> FALSE]
\* the fleet beat: a member's beat that changes nothing (f1), one that changes the
\* member's word (f2), a write and a friend's beat, one reader card
MCCallersFleet == {"f1", "f2", "w1", "b1"}
MCKindOfFleet == [r \in MCCallersFleet |->
                    IF r \in {"f1", "f2"} THEN "fbeat"
                    ELSE IF r = "b1" THEN "beat"
                    ELSE "write"]
MCChangesFleet == [r \in MCCallersFleet |-> r = "f2"]
\* the fleet beat alone, no friend's beat: the "beatonline" witness over these callers
\* fails for the changeless fleet beat f1, not for a friend's
MCCallersFBeat == {"f1", "f2", "w1"}
MCKindOfFBeat == [r \in MCCallersFBeat |-> MCKindOfFleet[r]]
MCChangesFBeat == [r \in MCCallersFBeat |-> MCChangesFleet[r]]
=============================================================================
