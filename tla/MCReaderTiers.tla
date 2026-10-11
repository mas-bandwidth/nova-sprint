---------------------------- MODULE MCReaderTiers ----------------------------
\* The instances of ReaderTiers.tla (tla/CASES.tsv, group readertiers): one flash
\* reader (rf), two readers with an empty cell (ra1, ra2) and a held friend's
\* reader row (hra), one flash card and one pro card (two reads).
EXTENDS ReaderTiers

\* ReadsNeededIn's tier rule (readers.go ReadsNeeded): one read for a flash
\* card, two for any stronger tier.
MCNeed == [c \in Cards |-> IF tierOf[c] = "flash" THEN 1 ELSE 2]
=============================================================================
