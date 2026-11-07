---------------- MODULE RunClearTraceLog ----------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", epoch |-> 1, log |-> <<>>, cursor |-> 0, sent |-> {}, phase |-> "idle"],
  [i |-> 2, action |-> "Clear", epoch |-> 2, log |-> <<[kind |-> "clear", epoch |-> 1]>>],
  [i |-> 3, action |-> "Prepare", snapshotEpoch |-> 2, through |-> 1, scan |-> 0, phase |-> "scan"],
  [i |-> 4, action |-> "Collected", through |-> 1, scan |-> 1, needed |-> {1}, cancelled |-> {}, phase |-> "send"],
  [i |-> 5, action |-> "EnvelopeConfirmed", members |-> {1}, omitted |-> {}, recipient |-> "r1", messageID |-> "1000-1", sent |-> {1}, acknowledged |-> {1}],
  [i |-> 6, action |-> "SaveCursor", cursor |-> 1, phase |-> "idle"],
  [i |-> 7, action |-> "AppendNotice", epoch |-> 2, log |-> <<[kind |-> "clear", epoch |-> 1],[kind |-> "notice", epoch |-> 2]>>],
  [i |-> 8, action |-> "AppendNotice", epoch |-> 2, log |-> <<[kind |-> "clear", epoch |-> 1],[kind |-> "notice", epoch |-> 2],[kind |-> "notice", epoch |-> 2]>>],
  [i |-> 9, action |-> "Prepare", snapshotEpoch |-> 2, through |-> 3, scan |-> 1, phase |-> "scan"],
  [i |-> 10, action |-> "Collected", through |-> 3, scan |-> 3, needed |-> {2,3}, cancelled |-> {}, phase |-> "send"],
  [i |-> 11, action |-> "EnvelopeConfirmed", members |-> {2,3}, omitted |-> {}, recipient |-> "r1", messageID |-> "1000-1", sent |-> {1,2,3}, acknowledged |-> {2,3}],
  [i |-> 12, action |-> "SaveCursor", cursor |-> 3, phase |-> "idle"]
>>
=========================================================
