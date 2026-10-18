------------------------------ MODULE MCDoneRecord ------------------------------
\* The instance of DoneRecord.tla (tla/CASES.tsv, group donerecord): two cards, three log
\* lines, two epochs, two layout versions, one line a tick. All bugs FALSE holds every
\* invariant; BugNoDedup (MCDoneRecordBrokenNoDedup.cfg) breaks NoDoubleCount; BugKeepEpoch
\* (MCDoneRecordBrokenKeepEpoch.cfg) breaks ClearStartsAgain; BugNoOriginGuard allows the
\* absent-head finish and breaks ReviewObservedOnOrigin; BugOverwriteNewer
\* (MCDoneRecordBrokenOverwriteNewer.cfg) drops the layout_newer guard and breaks
\* OlderBuildNeverWritesNewerLayout. TornWrite cuts a write after page 0; under LiveSpec's fair
\* tick the torn record is rebuilt (TornRepaired), and BugPageZeroGuard
\* (MCDoneRecordBrokenPageZeroGuard.cfg), the tick guarded on page 0's cursor alone, breaks it.
EXTENDS DoneRecord
=============================================================================
