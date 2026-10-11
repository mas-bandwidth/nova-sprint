----------------------------- MODULE ReaderTiers ------------------------------
\* No existing model covers this ask. ReadsByRoom places a read on a reader
\* with room and has no tiers. DirtyTick models a reader up, away and a
\* returned read asked again in place, and has no tiers either. This module is
\* the small model of the rule those two do not state: which tiers a reader
\* may be asked of (internal/sprint/read_route.go readerServesTier,
\* fleetReadsFlashOnly; internal/sprint/readers.go freeReaders, ReadsWanted,
\* enoughReadersUp), read as the drift of 2026-10-11.
\*
\* A fleet reader with an empty tiers cell reads flash only while the store
\* holds routes (fleetReadsFlashOnly) and every tier with none (tierRouted); a
\* friend's reader (its own model) with an empty cell reads every tier; a held
\* friend's reader serves nothing. The ask places a card's reads together
\* (ReadsWanted), as many as the card needs (ReadsNeededIn), each of a
\* different reader that serves its tier, and asks nothing when too few do (the
\* few-readers refusal). NoReadOutsideTier: no read is ever asked of a reader
\* outside the tiers its row names (the correct rule, not the ask's own).
\*
\* BugIgnoreHeld = TRUE is a reversed witness, a hold that is ignored: the held
\* reader reads its tiers as if up, is asked a read, and NoReadToAHeldReader
\* fails. BugEmptyReadsAll = TRUE is the other: a fleet reader's empty cell
\* reads every tier while routes exist (the ask before the fleetReadsFlashOnly
\* rule), so a read above flash is asked of it and NoReadOutsideTier fails.
\*
\* The instance is the module. It is run as the cases MCReaderTiers.cfg,
\* MCReaderTiersBrokenHeld.cfg and MCReaderTiersBrokenEmptyReadsAll.cfg
\* (tla/CASES.tsv, group readertiers).

EXTENDS Naturals, FiniteSets

\* BugIgnoreHeld: TRUE ignores a held reader's hold (the reversed witness).
\* BugEmptyReadsAll: TRUE makes a fleet reader's empty cell read every tier
\* while the store holds routes (the reversed witness).
CONSTANT BugIgnoreHeld, BugEmptyReadsAll
\* RoutesExist: whether the store holds routes (fleetReadsFlashOnly).
\* ReadsNeeded: the ask's ReadsNeededIn, 0, 1 or 2.
CONSTANT RoutesExist, ReadsNeeded

AllTiers == {"flash", "pro", "heavy", "frontier"}

Readers == {"rf", "re", "ra", "hra"}
Cards == {"cf", "cp", "ch", "cfz"}

tierOf == [cf |-> "flash", cp |-> "pro", ch |-> "heavy", cfz |-> "frontier"]

\* rf is a fleet reader whose cell names flash; re a fleet reader with an empty
\* cell; ra a friend's reader (its own model) with an empty cell; hra a held
\* friend's reader row. A held friend and her reader row are one member to the
\* server, and a held member serves nothing (docs/SPEC-SPRINT.md section 11,
\* hold).
heldReaders == {"hra"}
ownModelReaders == {"ra", "hra"}

\* The tiers each reader's cell names; {} is the empty cell.
readerTiers == [rf |-> {"flash"}, re |-> {}, ra |-> {}, hra |-> {}]

Held(r) == r \in heldReaders /\ ~BugIgnoreHeld

\* readerReadsTier: a friend's reader reads its cell, an empty cell every tier.
OwnReads(r) == IF readerTiers[r] = {} THEN AllTiers ELSE readerTiers[r]

\* A fleet reader reads its cell; an empty cell is flash while the store holds
\* routes (fleetReadsFlashOnly) and every tier with none (tierRouted).
FleetReads(r) ==
  IF readerTiers[r] = {} THEN (IF RoutesExist THEN {"flash"} ELSE AllTiers)
  ELSE readerTiers[r]

\* readerServesTier: the tiers a reader may be asked of. NoReadOutsideTier
\* checks this rule.
Reads(r) ==
  IF Held(r) THEN {}
  ELSE IF r \in ownModelReaders THEN OwnReads(r)
  ELSE FleetReads(r)

VARIABLE asked

Init == asked = [r \in Readers |-> {}]

\* The ask's own eligibility, the reader serving the card's tier by its own
\* rule. It does not consult Reads: BugEmptyReadsAll removes the fleet-empty-cell
\* guard, so the ask places a read the correct rule refuses, and
\* NoReadOutsideTier (which checks Reads) fails.
Serves(r, c) ==
  IF Held(r) THEN FALSE
  ELSE IF r \in ownModelReaders THEN tierOf[c] \in OwnReads(r)
  ELSE IF readerTiers[r] = {} THEN (BugEmptyReadsAll \/ tierOf[c] \in FleetReads(r))
  ELSE tierOf[c] \in readerTiers[r]

\* The readers the ask may place the card's reads on: they serve its tier and
\* do not hold it already.
Eligible(c) == {r \in Readers : Serves(r, c) /\ c \notin asked[r]}

\* The ask places every read the card needs together (ReadsWanted), each of a
\* different reader that serves its tier.
Ask(c) ==
  /\ \E S \in SUBSET Eligible(c) :
       /\ Cardinality(S) = ReadsNeeded
       /\ asked' = [r \in Readers |-> IF r \in S THEN asked[r] \cup {c} ELSE asked[r]]

\* Too few readers of the card's tier: the few-readers refusal. Nothing is asked.
Refuse(c) ==
  /\ Cardinality(Eligible(c)) < ReadsNeeded
  /\ UNCHANGED asked

Next == \E c \in Cards : Ask(c) \/ Refuse(c)

Spec == Init /\ [][Next]_asked

NoReadOutsideTier ==
  \A r \in Readers : \A c \in asked[r] : tierOf[c] \in Reads(r)

\* The tightened rule: a held friend's reader is asked nothing (docs/SPEC-SPRINT.md
\* section 11, hold).
NoReadToAHeldReader ==
  \A r \in heldReaders : asked[r] = {}

=============================================================================
