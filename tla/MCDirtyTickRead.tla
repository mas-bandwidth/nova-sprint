--------------------------- MODULE MCDirtyTickRead ---------------------------
\* The small instances of DirtyTickRead: two tables of one record, another
\* writer's one step, a display write, a stop and a start, one clear, one
\* queued change, one part of the tick; each configuration names the rule it
\* breaks (Broken; "none" for none).
EXTENDS DirtyTickRead
=============================================================================
