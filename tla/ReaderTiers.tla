----------------------------- MODULE ReaderTiers ------------------------------
\* The ask of nova-sprint by tier (docs/SPEC-SPRINT.md section 6; internal/sprint
\* readers.go freeReaders, ReadsWanted; read_route.go readerServesTier): a read
\* is asked only of a reader that serves the card's tier, and a card's reads are
\* asked together, the reads it needs at once.
\*
\* A fleet reader whose tiers cell is empty reads every tier a route serves
\* (read_route.go readerServesTier: the empty cell falls to tierRouted, and the
\* flash-only rule is gone; a-cleared-sprint-reads-with-no-seat-action). A
\* friend's reader brings its own model (ownModelReader) and reads its own
\* model's tiers: an empty cell reads every tier, route or none
\* (readerReadsTier). A reader whose cell names tiers reads those, and a held
\* friend's reader reads none (docs/SPEC-SPRINT.md section 11, hold).
\*
\* Every up fleet member has a member reader row reader-<member>, created by
\* init, by a member's join and by a clear when missing, and up while its
\* member is up and not held (read_cards.go memberReads, steps_clear.go
\* ShapeOf). A clear keeps member readers and every working default
\* (defaults.go SetDefaults): after Clear, every up member is a reader of
\* every served tier and read-cards stays on.
\*
\* Reversed witnesses, each red: BugIgnoreHeld ignores a hold and fails
\* NoReadToAHeldReader; BugEmptyFlashOnly has an empty cell read flash only
\* and fails EmptyCellReadsEveryServedTier; BugClearDropsReaders has Clear
\* drop member reader rows and fails EveryUpMemberIsAReader;
\* BugClearTurnsOff has Clear write read-cards off and fails
\* ReadCardsStaysOn.
\*
\* The instance is the module. It is run as the cases MCReaderTiers.cfg,
\* MCReaderTiersBrokenHeld.cfg, MCReaderTiersBrokenEmptyFlashOnly.cfg,
\* MCReaderTiersBrokenClearDrops.cfg and MCReaderTiersBrokenClearOff.cfg
\* (tla/CASES.tsv, group readertiers).

EXTENDS Naturals, FiniteSets

\* BugIgnoreHeld: TRUE ignores a held reader's hold (the reversed witness).
\* BugEmptyFlashOnly: TRUE has a fleet empty cell read flash alone.
\* BugClearDropsReaders: TRUE has Clear drop the member reader rows.
\* BugClearTurnsOff: TRUE has Clear write read-cards off.
CONSTANTS BugIgnoreHeld, BugEmptyFlashOnly, BugClearDropsReaders,
          BugClearTurnsOff, RoutesExist, Need

Readers == {"rf", "ra1", "ra2", "hra"}
Cards == {"cf", "cp"}
Members == {"m1", "m2"}
\* Four tiers, the code's routes (reader_tiers.go readerTierOrder).
AllTiers == {"flash", "pro", "heavy", "frontier"}
\* The tiers a route serves in this instance: every tier (with no route at
\* all every reader runs its own model, tierRouted, so the set is the same).
ServedTiers == AllTiers

tierOf == [cf |-> "flash", cp |-> "pro"]
\* rf reads flash only; an empty cell is every served tier (read_route.go
\* readerServesTier after the flash-only rule went).
readerTiers == [rf |-> {"flash"}, ra1 |-> {}, ra2 |-> {}, hra |-> {}]

\* hra is a held friend's reader row (reader-<friend>): its tiers cell is empty,
\* so unheld it reads every tier (its own model); held, it reads none.
heldReaders == {"hra"}

\* hra brings its own model (ownModelReader), so unheld its empty cell reads
\* every tier (readerReadsTier); the fleet's readers run their read's route, and
\* the fleet empty-cell rule below is theirs alone.
OwnModelReaders == {"hra"}

\* Every member of this instance is up; its reader row is reader-<member>,
\* modelled here as the memberReader flag per member.
memberUp == [m \in Members |-> TRUE]

Held(r) == r \in heldReaders /\ ~BugIgnoreHeld

\* What the ask reads off the cell. BugEmptyFlashOnly is the reversed witness:
\* a fleet empty cell that reads flash alone.
Reads(r) ==
  IF Held(r) THEN {}
  ELSE IF r \in OwnModelReaders THEN
    IF readerTiers[r] = {} THEN AllTiers
    ELSE readerTiers[r]
  ELSE IF readerTiers[r] = {} THEN
    IF BugEmptyFlashOnly THEN {"flash"}
    ELSE ServedTiers
  ELSE readerTiers[r]

VARIABLES asked, memberReader, readCardsOn

vars == <<asked, memberReader, readCardsOn>>

Init ==
  /\ asked = [r \in Readers |-> {}]
  /\ memberReader = [m \in Members |-> TRUE]
  /\ readCardsOn = TRUE

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
  /\ UNCHANGED <<memberReader, readCardsOn>>

\* Too few readers of the card's tier: the few-readers refusal. Nothing is asked.
Refuse(c) ==
  /\ Cardinality(FreeFor(c)) < Need[c]
  /\ UNCHANGED vars

\* Clear advances the epoch: the work goes, the shape stays. Member reader
\* rows and every working default survive (steps_clear.go ShapeOf,
\* defaults.go SetDefaults); the asked reads are the old epoch's and go.
Clear ==
  /\ asked' = [r \in Readers |-> {}]
  /\ memberReader' = IF BugClearDropsReaders
                       THEN [m \in Members |-> FALSE]
                       ELSE memberReader
  /\ readCardsOn' = IF BugClearTurnsOff THEN FALSE ELSE TRUE

Next == (\E c \in Cards : Ask(c) \/ Refuse(c)) \/ Clear

Spec == Init /\ [][Next]_vars

\* The code's rule the invariant checks (read_route.go readerServesTier): a
\* fleet reader whose cell is empty serves every tier a route serves; a
\* friend's reader serves its own model's tiers, an empty cell every tier; a
\* reader whose cell names tiers serves those alone; a held reader serves
\* nothing.
ServesTier(r, c) ==
  /\ ~Held(r)
  /\ IF r \in OwnModelReaders THEN
       IF readerTiers[r] = {} THEN TRUE
       ELSE tierOf[c] \in readerTiers[r]
     ELSE IF readerTiers[r] = {} THEN tierOf[c] \in ServedTiers
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

\* An empty cell reads every served tier: no reader with an empty cell is
\* held to flash alone (the flash-only rule is gone).
EmptyCellReadsEveryServedTier ==
  /\ asked = asked
  /\ \A r \in Readers :
       (~Held(r) /\ readerTiers[r] = {}) => Reads(r) = ServedTiers

\* After Clear (and at every point, since only Clear can change it), every
\* up member has its member reader row.
EveryUpMemberIsAReader ==
  \A m \in Members : memberUp[m] => memberReader[m]

\* A clear never turns a working default off: read-cards stays on.
ReadCardsStaysOn == readCardsOn = TRUE

=============================================================================
