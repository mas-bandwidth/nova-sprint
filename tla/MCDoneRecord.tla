------------------------------ MODULE MCDoneRecord ------------------------------
\* The instance of DoneRecord.tla (tla/CASES.tsv, group donerecord): two cards, three log
\* lines, two epochs, one line a tick. Both bugs FALSE holds every invariant; BugNoDedup
\* (MCDoneRecordBrokenNoDedup.cfg) breaks NoDoubleCount; BugKeepEpoch
\* (MCDoneRecordBrokenKeepEpoch.cfg) breaks ClearStartsAgain.
\* BugNoOriginGuard allows the absent-head finish and breaks ReviewObservedOnOrigin.
EXTENDS DoneRecord
=============================================================================
