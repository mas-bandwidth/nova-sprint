---------------- MODULE RunClearTraceLog ----------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", epoch |-> 1, log |-> <<>>, cursor |-> 0, sent |-> {}, phase |-> "idle"],
  [i |-> 2, action |-> "AppendNotice", epoch |-> 1, log |-> <<[kind |-> "notice", epoch |-> 1]>>],
  [i |-> 3, action |-> "Prepare", snapshotEpoch |-> 1, through |-> 1, scan |-> 0, phase |-> "scan"],
  [i |-> 4, action |-> "Collected", through |-> 1, scan |-> 1, needed |-> {1}, cancelled |-> {}, phase |-> "send"],
  [i |-> 5, action |-> "EnvelopeConfirmed", members |-> {1}, omitted |-> {}, recipient |-> "r1", messageID |-> "1000-1", sent |-> {1}, acknowledged |-> {1}],
  [i |-> 6, action |-> "SaveCursor", cursor |-> 1, phase |-> "idle"],
  [i |-> 7, action |-> "Clear", epoch |-> 2, log |-> <<[kind |-> "notice", epoch |-> 1],[kind |-> "clear", epoch |-> 1]>>],
  [i |-> 8, action |-> "Prepare", snapshotEpoch |-> 2, through |-> 2, scan |-> 1, phase |-> "scan"],
  [i |-> 9, action |-> "Collected", through |-> 2, scan |-> 2, needed |-> {2}, cancelled |-> {}, phase |-> "send"],
  [i |-> 10, action |-> "EnvelopeConfirmed", members |-> {2}, omitted |-> {}, recipient |-> "r1", messageID |-> "1000-1", sent |-> {1,2}, acknowledged |-> {2}],
  [i |-> 11, action |-> "SaveCursor", cursor |-> 2, phase |-> "idle"],
  [i |-> 12, action |-> "AppendNotice", epoch |-> 2, log |-> <<[kind |-> "notice", epoch |-> 1],[kind |-> "clear", epoch |-> 1],[kind |-> "notice", epoch |-> 2]>>],
  [i |-> 13, action |-> "Prepare", snapshotEpoch |-> 2, through |-> 3, scan |-> 2, phase |-> "scan"],
  [i |-> 14, action |-> "Collected", through |-> 3, scan |-> 3, needed |-> {3}, cancelled |-> {}, phase |-> "send"],
  [i |-> 15, action |-> "EnvelopeConfirmed", members |-> {3}, omitted |-> {}, recipient |-> "r1", messageID |-> "1000-1", sent |-> {1,2,3}, acknowledged |-> {3}],
  [i |-> 16, action |-> "SaveCursor", cursor |-> 3, phase |-> "idle"]
>>
=========================================================
