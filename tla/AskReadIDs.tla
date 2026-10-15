----------------------------- MODULE AskReadIDs -----------------------------
\* The identity allocated when AskStep re-asks a read (internal/sprint/store/steps.go).
\* The first fleet read ended without a verdict. ReadCardExtras must load that
\* ended Fleet record before sprint.Ask calls the read-card allocator, so the
\* allocator advances from r1 to r2 instead of reusing r1. This small bounded
\* model isolates that visibility rule; the reversed witness models AskStep
\* omitting Fleet ReadCardExtras.

EXTENDS Naturals, FiniteSets, Sequences

CONSTANT Broken

VARIABLES cards, loaded, prepared, asked
vars == <<cards, loaded, prepared, asked>>

Init ==
  /\ cards = <<[id |-> "r1", ended |-> TRUE]>>
  /\ loaded = {}
  /\ prepared = FALSE
  /\ asked = FALSE

\* AskStep loads ended fleet read cards with sprint.ReadCardExtras(s).
Load ==
  /\ loaded' = IF Broken THEN {} ELSE {cards[i].id : i \in 1..Len(cards)}
  /\ prepared' = TRUE
  /\ UNCHANGED <<cards, asked>>

\* The bounded allocator chooses the base id when it cannot see r1, or the
\* next id when the ended Fleet record is in the snapshot.
Ask ==
  /\ prepared /\ ~asked
  /\ LET id == IF "r1" \in loaded THEN "r2" ELSE "r1"
     IN cards' = Append(cards, [id |-> id, ended |-> FALSE])
  /\ asked' = TRUE
  /\ UNCHANGED <<loaded, prepared>>

Next == Load \/ Ask
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ cards \in Seq([id : {"r1", "r2"}, ended : BOOLEAN])
  /\ loaded \subseteq {"r1", "r2"}
  /\ prepared \in BOOLEAN
  /\ asked \in BOOLEAN

\* Each read-card identity is unique even when its earlier record has ended.
NoReusedReadID ==
  Cardinality({cards[i].id : i \in 1..Len(cards)}) = Len(cards)

=============================================================================
