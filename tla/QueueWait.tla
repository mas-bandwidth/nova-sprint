----------------------------- MODULE QueueWait -----------------------------
EXTENDS Naturals, Sequences, FiniteSets, TLC
CONSTANTS Workers, Cards, Owner, Deadline, Broken
VARIABLES log, cursor, lastQueue, phase, cut, sampled, seen, clock, until,
          timedPastEvent
vars == <<log, cursor, lastQueue, phase, cut, sampled, seen, clock, until,
          timedPastEvent>>
LogCards(n, w) == {log[i] : i \in {j \in 1..n : Owner[log[j]] = w}}
Init == /\ log = <<>>
        /\ cursor = [w \in Workers |-> 0]
        /\ lastQueue = [w \in Workers |-> {}]
        /\ phase = [w \in Workers |-> "idle"]
        /\ cut = [w \in Workers |-> 0]
        /\ sampled = [w \in Workers |-> {}]
        /\ seen = [w \in Workers |-> {}]
        /\ clock = 0
        /\ until = [w \in Workers |-> 0]
        /\ timedPastEvent = FALSE
Deal(c) == /\ c \notin {log[i] : i \in 1..Len(log)}
           /\ log' = Append(log, c)
           /\ UNCHANGED <<cursor, lastQueue, phase, cut, sampled, seen,
                          clock, until, timedPastEvent>>
Wait(w) == /\ phase[w] = "idle"
           /\ phase' = [phase EXCEPT ![w] = "waiting"]
           /\ until' = [until EXCEPT ![w] = Deadline]
           /\ UNCHANGED <<log, cursor, lastQueue, cut, sampled, seen, clock,
                          timedPastEvent>>
\* WaitLog returns a cut BEFORE the queue read. Writes committed afterwards
\* remain after the cursor. Broken reverses these independent reads.
Wake(w) == /\ phase[w] = "waiting" /\ Len(log) > cursor[w]
           /\ phase' = [phase EXCEPT ![w] = "reading"]
           /\ cut' = [cut EXCEPT ![w] = Len(log)]
           /\ sampled' = [sampled EXCEPT ![w] = LogCards(Len(log), w)]
           /\ UNCHANGED <<log, cursor, lastQueue, seen, clock, until,
                          timedPastEvent>>
Timeout(w) == /\ phase[w] = "waiting" /\ clock >= until[w]
              /\ Len(log) <= cursor[w]
              /\ phase' = [phase EXCEPT ![w] = "reading"]
              /\ cut' = [cut EXCEPT ![w] = Len(log)]
              /\ sampled' = [sampled EXCEPT ![w] = LogCards(Len(log), w)]
              /\ timedPastEvent' = (Len(log) > cursor[w])
              /\ UNCHANGED <<log, cursor, lastQueue, seen, clock, until>>
Read(w) == /\ phase[w] = "reading"
           /\ cursor' = [cursor EXCEPT ![w] = IF Broken THEN Len(log) ELSE cut[w]]
           /\ lastQueue' = [lastQueue EXCEPT ![w] =
                              IF Broken THEN sampled[w] ELSE LogCards(Len(log), w)]
           /\ seen' = [seen EXCEPT ![w] = @ \cup
                              IF Broken THEN sampled[w] ELSE LogCards(Len(log), w)]
           /\ phase' = [phase EXCEPT ![w] = "idle"]
           /\ UNCHANGED <<log, cut, sampled, clock, until, timedPastEvent>>
\* Finite clock saturates: expired waits remain eligible; there is no real time.
Advance == /\ clock < Deadline
           /\ clock' = clock + 1
           /\ UNCHANGED <<log, cursor, lastQueue, phase, cut, sampled, seen,
                          until, timedPastEvent>>
Next == (\E c \in Cards : Deal(c)) \/ (\E w \in Workers :
        Wait(w) \/ Wake(w) \/ Timeout(w) \/ Read(w)) \/ Advance
Spec == Init /\ [][Next]_vars /\ WF_vars(Advance)
        /\ \A w \in Workers : WF_vars(Wait(w)) /\ WF_vars(Wake(w))
                               /\ WF_vars(Timeout(w)) /\ WF_vars(Read(w))
TypeOK == /\ log \in Seq(Cards) /\ Len(log) <= Cardinality(Cards)
          /\ cursor \in [Workers -> 0..Cardinality(Cards)]
          /\ lastQueue \in [Workers -> SUBSET Cards]
          /\ seen \in [Workers -> SUBSET Cards]
          /\ clock \in 0..Deadline
CursorNeverSkips == \A w \in Workers : LogCards(cursor[w], w) \subseteq lastQueue[w]
PushedWithinOneEvent == ~timedPastEvent
EveryDealIsSeen == \A c \in Cards :
                    (c \in {log[i] : i \in 1..Len(log)}) ~> (c \in seen[Owner[c]])
=============================================================================
