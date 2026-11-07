---------------- MODULE RunClearTraceLog ----------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", epoch |-> 1, log |-> <<>>, cursor |-> 0, sent |-> {}, phase |-> "idle"],
  [i |-> 2, action |-> "Clear", epoch |-> 2, log |-> <<[kind |-> "clear", epoch |-> 1]>>],
  [i |-> 3, action |-> "AppendNotice", epoch |-> 2, log |-> <<[kind |-> "clear", epoch |-> 1],[kind |-> "notice", epoch |-> 2]>>],
  [i |-> 4, action |-> "AppendNotice", epoch |-> 2, log |-> <<[kind |-> "clear", epoch |-> 1],[kind |-> "notice", epoch |-> 2],[kind |-> "notice", epoch |-> 2]>>],
  [i |-> 5, action |-> "Prepare", snapshotEpoch |-> 2, through |-> 3, scan |-> 0, phase |-> "scan"],
  [i |-> 6, action |-> "Collected", through |-> 3, scan |-> 3, needed |-> {1,2,3}, cancelled |-> {}, phase |-> "send"],
  [i |-> 7, action |-> "Send", n |-> 1, sent |-> {1}, acknowledged |-> {1}],
  [i |-> 8, action |-> "Send", n |-> 2, sent |-> {1,2}, acknowledged |-> {1,2}],
  [i |-> 9, action |-> "Send", n |-> 3, sent |-> {1,2,3}, acknowledged |-> {1,2,3}],
  [i |-> 10, action |-> "SaveCursor", cursor |-> 3, phase |-> "idle"]
>>
=========================================================
