--------------------------- MODULE MCDirtyTickRead ---------------------------
\* The small instances of DirtyTickRead. The pass case and the newer witnesses:
\* two tables of one record, another writer's one step, a display write, a stop
\* and a start, one clear, one queued change, one part of every kind. The
\* two-part case and the five earlier witnesses: two tables of two records,
\* another writer's two steps, two parts, no clear and no queued change (the
\* two-part case with tick parts alone and no display write). Each configuration names the rule it
\* breaks (Broken; "none" for none).
EXTENDS DirtyTickRead
=============================================================================
