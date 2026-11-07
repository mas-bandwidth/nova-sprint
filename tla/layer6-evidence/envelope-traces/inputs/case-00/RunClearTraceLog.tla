---------------- MODULE RunClearTraceLog ----------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", epoch |-> 1, log |-> <<>>, cursor |-> 0, sent |-> {}, phase |-> "idle"],
  [i |-> 2, action |-> "AppendNotice", epoch |-> 1, log |-> <<[kind |-> "notice", epoch |-> 1]>>],
  [i |-> 3, action |-> "Clear", epoch |-> 2, log |-> <<[kind |-> "notice", epoch |-> 1],[kind |-> "clear", epoch |-> 1]>>],
  [i |-> 4, action |-> "AppendNotice", epoch |-> 2, log |-> <<[kind |-> "notice", epoch |-> 1],[kind |-> "clear", epoch |-> 1],[kind |-> "notice", epoch |-> 2]>>],
  [i |-> 5, action |-> "Prepare", snapshotEpoch |-> 2, through |-> 3, scan |-> 0, phase |-> "scan"],
  [i |-> 6, action |-> "Collected", through |-> 3, scan |-> 3, needed |-> {2,3}, cancelled |-> {1}, phase |-> "send"],
  [i |-> 7, action |-> "EnvelopeConfirmed", members |-> {2,3}, omitted |-> {}, recipient |-> "r1", messageID |-> "1000-1", sent |-> {2,3}, acknowledged |-> {2,3}],
  [i |-> 8, action |-> "SaveCursor", cursor |-> 3, phase |-> "idle"]
>>
=========================================================
