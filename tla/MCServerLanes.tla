---------------------------- MODULE MCServerLanes ----------------------------
EXTENDS ServerLanes
\* safety: a beat, two reads (one waits for the other), a write, a begin and a
\* queue (the begin and the queue take the line), one reader card
MCCallers == {"b1", "r1", "r2", "w1", "b2", "q1"}
\* liveness: a beat, a read, a write, a begin and a queue, a tick
MCCallersLive == {"b1", "r1", "w1", "b2", "q1"}
MCCards == {"c1"}
MCCardsLive == {"c1"}
MCKindOf == [r \in MCCallers |->
               IF r = "b1" THEN "beat"
               ELSE IF r \in {"r1", "r2"} THEN "read"
               ELSE IF r = "b2" THEN "begin"
               ELSE IF r = "q1" THEN "queue"
               ELSE "write"]
MCKindOfLive == [r \in MCCallersLive |-> MCKindOf[r]]
=============================================================================
