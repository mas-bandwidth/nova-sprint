--------------------------- MODULE CIReceiptTell ---------------------------
EXTENDS Naturals, Sequences, FiniteSets
CONSTANT Broken
VARIABLES rows, cursor, told, inflight, acknowledged, clock, counts, lost
vars == <<rows, cursor, told, inflight, acknowledged, clock, counts, lost>>

\* Rows 1 and 2 share a once-key; row 3 is another run. The bus atomically
\* publishes to both recipients. Told means accepted by the bus, not read by a
\* human. The cursor is durable; in-flight arguments survive restart in Go.
Key(i) == IF i <= 2 THEN 1 ELSE 2
Init == /\ rows = <<>> /\ cursor = 0 /\ told = {}
        /\ inflight = 0 /\ acknowledged = FALSE /\ clock = 0
        /\ counts = [k \in 1..2 |-> 0] /\ lost = FALSE
Receipt == /\ Len(rows) < 3
           /\ rows' = Append(rows, Key(Len(rows) + 1))
           /\ UNCHANGED <<cursor, told, inflight, acknowledged, clock, counts, lost>>
Read == /\ inflight = 0 /\ cursor < Len(rows)
        /\ inflight' = cursor + 1 /\ acknowledged' = FALSE
        /\ UNCHANGED <<rows, cursor, told, clock, counts, lost>>
Send == /\ inflight # 0 /\ ~acknowledged
        /\ told' = told \cup {rows[inflight]}
        /\ counts' = IF rows[inflight] \in told THEN counts
                      ELSE [counts EXCEPT ![rows[inflight]] = @ + 1]
        /\ acknowledged' = TRUE
        /\ UNCHANGED <<rows, cursor, inflight, clock, lost>>
\* Lost replies may occur repeatedly, with or without a bus commit. The
\* correct consumer retains its durable pending send, so Send stays enabled
\* and weak fairness ensures eventual success. The reversed implementation
\* forgets that pending work after its premature cursor advance.
SendLost == /\ inflight # 0 /\ ~acknowledged
            /\ inflight' = IF Broken = "cursorfirst" THEN 0 ELSE inflight
            /\ lost' = ~lost
            /\ \/ /\ UNCHANGED <<told, counts>>
                   \/ /\ told' = told \cup {rows[inflight]}
                      /\ counts' = IF rows[inflight] \in told THEN counts
                                    ELSE [counts EXCEPT ![rows[inflight]] = @ + 1]
            /\ UNCHANGED <<rows, cursor, acknowledged, clock>>
AdvanceCursor == /\ inflight # 0
                 /\ (acknowledged \/ Broken = "cursorfirst")
                 /\ cursor' = inflight
                 /\ IF acknowledged THEN inflight' = 0 ELSE UNCHANGED inflight
                 /\ acknowledged' = FALSE
                 /\ UNCHANGED <<rows, told, clock, counts, lost>>
Tick == /\ clock < 2 /\ clock' = clock + 1
        /\ UNCHANGED <<rows, cursor, told, inflight, acknowledged, counts, lost>>
Sweep == /\ Broken # "cursorfirst" /\ clock = 2
         /\ inflight = 0 /\ cursor < Len(rows)
         /\ inflight' = cursor + 1 /\ acknowledged' = FALSE /\ clock' = 0
         /\ UNCHANGED <<rows, cursor, told, counts, lost>>
Next == Receipt \/ Read \/ Send \/ SendLost \/ AdvanceCursor \/ Tick \/ Sweep
Spec == Init /\ [][Next]_vars /\ WF_vars(Read) /\ WF_vars(Send)
        /\ WF_vars(Sweep) /\ WF_vars(AdvanceCursor) /\ WF_vars(Tick)
TypeOK == /\ rows \in Seq(1..2) /\ Len(rows) <= 3
          /\ cursor \in 0..Len(rows) /\ inflight \in 0..Len(rows)
          /\ told \subseteq 1..2 /\ acknowledged \in BOOLEAN
          /\ clock \in 0..2 /\ counts \in [1..2 -> 0..3] /\ lost \in BOOLEAN
NeverToldTwice == \A k \in 1..2 : counts[k] <= 1
CursorNeverPassesUntold == \A i \in 1..cursor : rows[i] \in told
EveryReceiptIsPushed == \A i \in 1..3 : (i <= Len(rows)) ~> (Key(i) \in told)
=============================================================================
