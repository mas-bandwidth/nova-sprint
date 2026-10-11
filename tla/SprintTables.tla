--------------------------- MODULE SprintTables ------------------------------
\* The sprint's four tables and the lifecycle of a primary (docs/SPEC-SPRINT.md
\* section 3), written from the reference model internal/sprint/refmodel
\* (actions.go) and the code (internal/sprint/readers.go ReadsNeededIn,
\* internal/sprint/lifecycle.go Moves). This cut holds the read actions and the
\* accept: a card in review asks its reads of readers other than its worker, a
\* read begins and returns ok or broken, and accept moves a primary to merging
\* when it has the reads its tier needs (one for a flash card, two for a pro
\* card; ReadsNeededIn).
\*
\* A primary's state is the work-table column it sits in, the six states of
\* lifecycle.go: waiting, ready, working, review, merging, landed. A read card
\* sits in the readers table, one cell of a primary x reader pair: none
\* (retired or not yet cut), asked, reading, ok or broken. The worker of a
\* primary is the reader of the machine that worked its card (reader-<m>, the
\* one reader on fleet machine m); a primary's read is never asked of its own
\* worker (NeverReadByItsWorker).
\*
\* Actions (refmodel actions.go):
\*   Ask(p)       the primary in review asks one more read of a reader other
\*                than its worker (ReadsWanted > 0, no read broken yet).
\*   AskAnother(p) one more reader for a primary already asked, answering a
\*                broken read or reads exhausted.
\*   ReadStart(r) the reader moves its own asked read to reading.
\*   Read(r)      the reader records ok or broken on a read asked or reading.
\*   Accept(p)    review -> merging when the primary has the reads its tier
\*                needs (ReadsNeededIn). Broken = "shortaccept" drops the
\*                guard: it accepts with one read fewer (AcceptHeld).
\*
\* Drift kept out (v1.2.13 cleanup, not here): refmodel's Accept demands two
\* readers where lifecycle.go:57 and ReadsNeededIn allow one for a flash card,
\* so this module models the code (ReadsNeededIn). docs/SPEC-SPRINT.md section 3
\* calls accept "mechanical (the tick)"; lifecycle.go:57 names it Coordinator,
\* and this module models the code (Accept is a named verb, all or nothing).
EXTENDS Integers, FiniteSets

CONSTANTS Primary, Reader, Flash, Broken

VARIABLES work, read, worker

\* work[p] is the state of primary p; read[p][r] the cell of p's read on
\* reader r; worker[p] the reader of the machine that worked p.
vars == <<work, read, worker>>

States == {"waiting", "ready", "working", "review", "merging", "landed"}
Place  == {"none", "asked", "reading", "ok", "broken"}

\* ReadsNeeded (readers.go ReadsNeeded): one reader for a flash card, two for
\* a pro card. ReadsNeededIn's setting override and the landed card's pinned
\* count are outside this cut (they are read in review and merging alike; the
\* accept here is review -> merging under the tier's rule).
Needs(p) == IF p \in Flash THEN 1 ELSE 2

\* The primary's live read cards (any cell but none) and the readers whose read
\* of it is ok at its head.
LiveReads(p) == {r \in Reader : read[p][r] # "none"}
OkReaders(p) == {r \in Reader : read[p][r] = "ok"}
BrokenRead(p) == \E r \in Reader : read[p][r] = "broken"

\* A primary is acceptable when as many different readers as it needs have an
\* ok read at its head (SPEC-SPRINT.md section 6).
Acceptable(p) == Cardinality(OkReaders(p)) >= Needs(p)

TypeOK ==
  /\ work \in [Primary -> States]
  /\ read \in [Primary -> [Reader -> Place]]
  /\ worker \in [Primary -> Reader]

Init ==
  /\ work = [p \in Primary |-> "review"]
  /\ read = [p \in Primary |-> [r \in Reader |-> "none"]]
  /\ worker = [p \in Primary |-> IF p \in Flash THEN "r1" ELSE "r2"]

\* A reader the primary can still be asked of: another reader, not its worker,
\* with no read card of the primary cut yet.
FreshReader(p) == Reader \ ({worker[p]} \cup LiveReads(p))

\* The ask: a primary in review wanting a read asks one of a reader other than
\* its worker, distinct from the readers already standing (refmodel Ask, line
\* 420). Refused with fewer than two readers.
Ask(p) ==
  /\ work[p] = "review"
  /\ ~ BrokenRead(p)
  /\ Cardinality(Reader) >= 2
  /\ Cardinality(LiveReads(p)) < Needs(p)
  /\ \E r \in FreshReader(p) :
       /\ read' = [read EXCEPT ![p][r] = "asked"]
       /\ UNCHANGED <<work, worker>>

\* One more reader for a primary already asked, answering a broken read or
\* reads exhausted (refmodel AskAnother, line 480).
AskAnother(p) ==
  /\ work[p] = "review"
  /\ LiveReads(p) # {}
  /\ (BrokenRead(p) \/ Cardinality(LiveReads(p)) >= Needs(p))
  /\ \E r \in FreshReader(p) :
       /\ read' = [read EXCEPT ![p][r] = "asked"]
       /\ UNCHANGED <<work, worker>>

\* The reader begins its own asked read (refmodel ReadStart, line 514).
ReadStart(r) ==
  /\ \E p \in Primary : read[p][r] = "asked"
  /\ \E p \in Primary :
       /\ read[p][r] = "asked"
       /\ read' = [read EXCEPT ![p][r] = "reading"]
       /\ UNCHANGED <<work, worker>>

\* The reader records ok or broken (refmodel Read, line 536). A report on a
\* read still asked is the begin and the report in one step.
Read(r) ==
  /\ \E p \in Primary : read[p][r] \in {"asked", "reading"}
  /\ \E p \in Primary, v \in {"ok", "broken"} :
       /\ read[p][r] \in {"asked", "reading"}
       /\ read' = [read EXCEPT ![p][r] = v]
       /\ UNCHANGED <<work, worker>>

\* Accept: review -> merging when the primary has the reads its tier needs
\* (refmodel Accept, line 593, held to ReadsNeededIn). Named, one primary at a
\* time here; broken "shortaccept" drops the read guard and accepts with one
\* read fewer.
Accept(p) ==
  /\ work[p] = "review"
  /\ Acceptable(p) \/ (Broken = "shortaccept" /\ Cardinality(OkReaders(p)) >= Needs(p) - 1)
  /\ work' = [work EXCEPT ![p] = "merging"]
  /\ UNCHANGED <<read, worker>>

Next ==
  \/ \E p \in Primary : Ask(p)
  \/ \E p \in Primary : AskAnother(p)
  \/ \E r \in Reader : ReadStart(r)
  \/ \E r \in Reader : Read(r)
  \/ \E p \in Primary : Accept(p)

Spec == Init /\ [][Next]_vars

\* A primary moves only by a row of the lifecycle table: every state is one of
\* the six (lifecycle.go Moves). The accept is the only move modelled here and
\* it is review -> merging, a row of the table.
MovesOnlyByTable == \A p \in Primary : work[p] \in States

\* Accept holds to the reads: no primary enters merging without ok read cards
\* from as many different readers at its head as its tier needs
\* (SPEC-SPRINT.md section 6; ReadsNeededIn).
AcceptHeld == \A p \in Primary : work[p] = "merging" => Acceptable(p)

\* A primary's read is never asked of its own worker (readers.go: the reader
\* ran the program itself; a read is asked of readers other than its worker).
NeverReadByItsWorker == \A p \in Primary, r \in Reader : read[p][r] # "none" => r # worker[p]

=============================================================================
