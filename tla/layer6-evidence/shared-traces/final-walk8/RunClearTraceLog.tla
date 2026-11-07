---------------- MODULE RunClearTraceLog ----------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", epoch |-> 1, log |-> <<>>, cursor |-> 0, sent |-> {}, phase |-> "idle"],
  [i |-> 2, action |-> "AppendNotice", epoch |-> 1, log |-> <<[kind |-> "notice", epoch |-> 1]>>],
  [i |-> 3, action |-> "AppendNotice", epoch |-> 1, log |-> <<[kind |-> "notice", epoch |-> 1],[kind |-> "notice", epoch |-> 1]>>],
  [i |-> 4, action |-> "Prepare", snapshotEpoch |-> 1, through |-> 2, scan |-> 0, phase |-> "scan"],
  [i |-> 5, action |-> "Collected", through |-> 2, scan |-> 2, needed |-> {1,2}, cancelled |-> {}, phase |-> "send"],
  [i |-> 6, action |-> "Send", n |-> 1, sent |-> {1}, acknowledged |-> {1}],
  [i |-> 7, action |-> "Send", n |-> 2, sent |-> {1,2}, acknowledged |-> {1,2}],
  [i |-> 8, action |-> "SaveCursor", cursor |-> 2, phase |-> "idle"],
  [i |-> 9, action |-> "Clear", epoch |-> 2, log |-> <<[kind |-> "notice", epoch |-> 1],[kind |-> "notice", epoch |-> 1],[kind |-> "clear", epoch |-> 1]>>],
  [i |-> 10, action |-> "Prepare", snapshotEpoch |-> 2, through |-> 3, scan |-> 2, phase |-> "scan"],
  [i |-> 11, action |-> "Collected", through |-> 3, scan |-> 3, needed |-> {3}, cancelled |-> {}, phase |-> "send"],
  [i |-> 12, action |-> "Send", n |-> 3, sent |-> {1,2,3}, acknowledged |-> {3}],
  [i |-> 13, action |-> "SaveCursor", cursor |-> 3, phase |-> "idle"]
>>
=========================================================
