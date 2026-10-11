----------------------------- MODULE ReaderTiers ------------------------------
\* The ask of nova-sprint by tier (docs/SPEC-SPRINT.md section 6; internal/sprint
\* readers.go freeReaders, ReadsWanted; read_route.go readerServesTier): a read
\* is asked only of a reader that serves the card's tier, and a card's reads are
\* asked together, the reads it needs at once.
\*
\* A fleet reader whose tiers cell is empty reads flash alone while the store
\* holds routes (read_route.go fleetReadsFlashOnly), and every tier with none
\* (tierRouted: no route means every reader runs its own model). A friend's
\* reader brings its own model (ownModelReader) and reads its own model's tiers:
\* an empty cell reads every tier, route or none (readerReadsTier). A reader
\* whose cell names tiers reads those, and a held friend's reader reads none
\* (docs/SPEC-SPRINT.md section 11, hold). The invariant: no read is ever asked
\* of a reader outside the card's tier, read as the code reads it.
\*
\* BugIgnoreHeld = TRUE is the reversed witness, a hold that is ignored: the held
\* reader reads its tiers as if up, is asked a read, and NoReadToAHeldReader
\* fails. BugEmptyReadsAll = TRUE is the other: an empty cell reads every tier
\* while routes exist, and NoReadOutsideTier fails.
\*
\* The instance is the module. It is run as the cases MCReaderTiers.cfg,
\* MCReaderTiersBrokenHeld.cfg and MCReaderTiersBrokenEmptyReadsAll.cfg
\* (tla/CASES.tsv, group readertiers).

EXTENDS Naturals, FiniteSets

\* BugIgnoreHeld: TRUE ignores a held reader's hold (the reversed witness).
\* BugEmptyReadsAll: TRUE has an empty cell read every tier while routes exist.
CONSTANTS BugIgnoreHeld, BugEmptyReadsAll, RoutesExist, Need

Readers == {"rf", "ra1", "ra2", "hra"}
Cards == {"cf", "cp"}
\* Four tiers, the code's routes (reader_tiers.go readerTierOrder).
AllTiers == {"flash", "pro", "heavy", "frontier"}

tierOf == [cf |-> "flash", cp |-> "pro"]
\* rf reads flash only; an empty cell is flash alone while the store holds
\* routes, every tier with none (read_route.go readerServesTier).
readerTiers == [rf |-> {"flash"}, ra1 |-> {}, ra2 |-> {}, hra |-> {}]

\* hra is a held friend's reader row (reader-<friend>): its tiers cell is empty,
\* so unheld it reads every tier (its own model); held, it reads none.
heldReaders == {"hra"}

\* hra brings its own model (ownModelReader), so unheld its empty cell reads
\* every tier (readerReadsTier); the fleet's readers run their read's route, and
\* the fleet empty-cell rule below is theirs alone.
OwnModelReaders == {"hra"}

Held(r) == r \in heldReaders /\ ~BugIgnoreHeld

\* What the ask reads off the cell. BugEmptyReadsAll is the reversed witness:
\* a fleet empty cell that reads every tier even while the store holds routes.
Reads(r) ==
  IF Held(r) THEN {}
  ELSE IF r \in OwnModelReaders THEN
    IF readerTiers[r] = {} THEN AllTiers
    ELSE readerTiers[r]
  ELSE IF readerTiers[r] = {} THEN
    IF BugEmptyReadsAll THEN AllTiers
    ELSE IF RoutesExist THEN {"flash"}
    ELSE AllTiers
  ELSE readerTiers[r]

VARIABLE asked

Init == asked = [r \in Readers |-> {}]

\* Eligible is the readers the ask may place the card on, those that have not
\* read it. It no longer filters on the tier; FreeFor adds the tier the reader
\* serves, so NoReadOutsideTier is the check that the ask reads the cell right.
Eligible(c) == {r \in Readers : c \notin asked[r]}
FreeFor(c) == {r \in Eligible(c) : tierOf[c] \in Reads(r)}

\* Ask places the reads the card needs together, Need[c] of them, one each on a
\* different free reader that serves the card's tier (ReadsWanted).
Ask(c) ==
  /\ Cardinality(FreeFor(c)) >= Need[c]
  /\ \E S \in SUBSET FreeFor(c) :
       /\ Cardinality(S) = Need[c]
       /\ asked' = [r \in Readers |-> IF r \in S THEN asked[r] \cup {c} ELSE asked[r]]

\* Too few readers of the card's tier: the few-readers refusal. Nothing is asked.
Refuse(c) ==
  /\ Cardinality(FreeFor(c)) < Need[c]
  /\ UNCHANGED asked

Next == \E c \in Cards : Ask(c) \/ Refuse(c)

Spec == Init /\ [][Next]_asked

\* The code's rule the invariant checks (read_route.go readerServesTier): a
\* fleet reader whose cell is empty serves flash alone while the store holds
\* routes, every tier with none; a friend's reader serves its own model's tiers,
\* an empty cell every tier; a reader whose cell names tiers serves those alone;
\* a held reader serves nothing.
ServesTier(r, c) ==
  /\ ~Held(r)
  /\ IF r \in OwnModelReaders THEN
       IF readerTiers[r] = {} THEN TRUE
       ELSE tierOf[c] \in readerTiers[r]
     ELSE IF readerTiers[r] = {} THEN RoutesExist => (tierOf[c] = "flash")
       ELSE tierOf[c] \in readerTiers[r]

\* The ask never places a read of a card on a reader that does not serve the
\* card's tier as the code reads the cell. It no longer holds by construction:
\* Eligible does not filter on the tier, so this is the check.
NoReadOutsideTier ==
  \A r \in Readers : \A c \in asked[r] : ServesTier(r, c)

\* The tightened rule: a held friend's reader is asked nothing (docs/SPEC-SPRINT.md
\* section 11, hold).
NoReadToAHeldReader ==
  \A r \in heldReaders : asked[r] = {}

=============================================================================
