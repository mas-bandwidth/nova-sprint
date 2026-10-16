---------------------------- MODULE MCReaderTiers ----------------------------
\* The instances of ReaderTiers.tla (tla/CASES.tsv, group readertiers): a flash
\* reader (rf), a reader with an empty cell (ra), two readers whose cell
\* names every tier (rx1, rx2), a held friend's reader row (hra) and a pro friend's
\* reader up (fr); a flash, a pro and a frontier primary in review, a flash and
\* a frontier work card ready; routes of flash and pro, heavy and frontier
\* switched on and off; or (RoutesExist = FALSE) a store that starts with no
\* route at all, every tier's switched on and off, real (StoreKind "real",
\* Redis) or the in-memory twin ("mem").
EXTENDS ReaderTiers

\* ReadsNeededIn's tier rule (readers.go ReadsNeeded): one read for a flash
\* card, two for any stronger tier.
MCNeed == [c \in Cards |-> IF tierOf[c] = "flash" THEN 1 ELSE 2]
=============================================================================
