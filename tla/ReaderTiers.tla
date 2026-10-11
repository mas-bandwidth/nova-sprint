------------------------------ MODULE ReaderTiers ------------------------------
\* No existing model covers this ask. ReadsByRoom places a read on a reader
\* with room and has no tiers. DirtyTick models a reader up, away and a
\* returned read asked again in place, and has no tiers either. This module is
\* the small model of the rule those two do not state.
\*
\* A reader whose tier set is empty reads every tier. Ask places a card only
\* on a reader who reads the card's tier, and only when enough such readers
\* exist (one for flash, two for pro). The invariant: no read is ever asked
\* of a reader outside the primary's tier.
\*
\* A friend's reader row (reader-<friend>) is one member with the friend to the
\* server, and a held member serves nothing (docs/SPEC-SPRINT.md section 11,
\* hold): a held friend's reader reads no tier, whatever its cell names, so it is
\* asked no read. BugIgnoreHeld = TRUE is the reversed witness, a hold that is
\* ignored: the held reader reads its tiers as if up, is asked a read, and
\* NoReadToAHeldReader fails.
\*
\* The instance is the module. It is run as the cases MCReaderTiers.cfg and
\* MCReaderTiersBrokenHeld.cfg (tla/CASES.tsv, group readertiers).

EXTENDS Naturals, FiniteSets

\* BugIgnoreHeld: TRUE ignores a held reader's hold (the reversed witness).
CONSTANT BugIgnoreHeld

Readers == {"rf", "ra1", "ra2", "hra"}
Cards == {"cf", "cp"}

tierOf == [cf |-> "flash", cp |-> "pro"]
\* rf reads flash only. An empty set is every tier.
readerTiers == [rf |-> {"flash"}, ra1 |-> {}, ra2 |-> {}, hra |-> {}]
AllTiers == {"flash", "pro"}

\* hra is a held friend's reader row (reader-<friend>): its tiers cell is empty,
\* so unheld it reads every tier; held, it reads none.
heldReaders == {"hra"}

Need(c) == IF tierOf[c] = "flash" THEN 1 ELSE 2
Held(r) == r \in heldReaders /\ ~BugIgnoreHeld
Reads(r) == IF Held(r) THEN {} ELSE IF readerTiers[r] = {} THEN AllTiers ELSE readerTiers[r]

VARIABLE asked

Init == asked = [r \in Readers |-> {}]

Eligible(c) == {r \in Readers : tierOf[c] \in Reads(r) /\ c \notin asked[r]}

Ask(c) ==
  /\ Cardinality(Eligible(c)) >= Need(c)
  /\ \E r \in Eligible(c) :
       asked' = [asked EXCEPT ![r] = @ \cup {c}]

\* Too few readers of the card's tier: the few-readers refusal. Nothing is asked.
Refuse(c) ==
  /\ Cardinality(Eligible(c)) < Need(c)
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
