---------------------------- MODULE MCReadsByRoomThree ----------------------------
\* The three-reader instance of ReadsByRoom.tla for the reversed witnesses that
\* need a reader to spare: the rework's first read asked of any reader (the
\* finder skippable only when a card needs fewer readers than there are) and
\* the ask while a read is broken (a card with a broken read asks another only
\* when a reader is still free). Two pro cards (two reads each), two attempts
\* a card; three readers on machines of widths 2, 1 and 1.
EXTENDS ReadsByRoom

MCReaders == {"r1", "r2", "r3"}
MCWidth == [r \in MCReaders |-> IF r = "r1" THEN 2 ELSE 1]
MCCards == {"p1", "p2"}
MCNeed == [c \in MCCards |-> 2]
=============================================================================
