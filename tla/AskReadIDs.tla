----------------------------- MODULE AskReadIDs -----------------------------
\* Ask keeps ended records and allocates beyond them. Load uses the candidate
\* reader set, including a friend without a fleet row, and loads the complete
\* ended generation chain. A first create refusal reloads and re-plans once.
\* NoReusedReadID forbids a terminal collision; the store always refuses an
\* existing identity, so even the broken witness never overwrites its history.
EXTENDS Naturals, FiniteSets, Sequences
CONSTANT Broken

Readers == {"member", "friend"}
Rows == {"member"}
MaxEnded == 5
FilterRows == FALSE
RetryOn == ~Broken

VARIABLES records, loaded, phase, reader, proposed, retries, raced, created
vars == <<records, loaded, phase, reader, proposed, retries, raced, created>>

Init ==
  /\ records = Readers \X (0..(MaxEnded - 1))
  /\ loaded = {}
  /\ phase = "load"
  /\ reader \in Readers
  /\ proposed = 0
  /\ retries = 0
  /\ raced = FALSE
  /\ created = {}

Visible == IF Broken THEN {} ELSE
  {id \in records : ~FilterRows \/ id[1] \in Rows}
NextID == CHOOSE n \in 0..(MaxEnded + 1) :
  /\ <<reader, n>> \notin loaded
  /\ \A m \in 0..(n - 1) : <<reader, m>> \in loaded

Load ==
  /\ phase = "load"
  /\ loaded' = Visible
  /\ phase' = "plan"
  /\ UNCHANGED <<records, reader, proposed, retries, raced, created>>
Plan ==
  /\ phase = "plan"
  /\ proposed' = NextID
  /\ phase' = "create"
  /\ UNCHANGED <<records, loaded, reader, retries, raced, created>>
\* An ended record appears after the snapshot: the first refusal is curable.
Race ==
  /\ phase = "create" /\ ~raced /\ retries = 0
  /\ records' = records \cup {<<reader, proposed>>}
  /\ raced' = TRUE
  /\ UNCHANGED <<loaded, phase, reader, proposed, retries, created>>
Create ==
  /\ phase = "create"
  /\ IF <<reader, proposed>> \in records
     THEN /\ phase' = IF RetryOn /\ retries = 0 THEN "refused" ELSE "failed"
          /\ UNCHANGED created
     ELSE /\ phase' = "done"
          /\ created' = {<<reader, proposed>>}
  /\ UNCHANGED <<records, loaded, reader, proposed, retries, raced>>
Retry ==
  /\ phase = "refused" /\ retries = 0
  /\ loaded' = records
  /\ retries' = 1
  /\ phase' = "plan"
  /\ UNCHANGED <<records, reader, proposed, raced, created>>

Next == Load \/ Plan \/ Race \/ Create \/ Retry
Spec == Init /\ [][Next]_vars
TypeOK ==
  /\ records \subseteq Readers \X (0..MaxEnded)
  /\ loaded \subseteq records
  /\ reader \in Readers
  /\ phase \in {"load", "plan", "create", "refused", "done", "failed"}
  /\ proposed \in 0..(MaxEnded + 1)
  /\ retries \in 0..1
  /\ raced \in BOOLEAN
  /\ created \subseteq Readers \X (0..(MaxEnded + 1))
NoReusedReadID == phase # "failed" /\ created \intersect records = {}
HistoryKept == Readers \X (0..(MaxEnded - 1)) \subseteq records
=============================================================================
