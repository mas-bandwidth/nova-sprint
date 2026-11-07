---------------- MODULE RunTraceLog ----------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", physicalSeq |-> 1, lock |-> "none", hook |-> "none", gens |-> <<0,0,0>>, done |-> {}, events |-> <<>>, cursor |-> [r \in {"r1"} |-> 0], sent |-> [r \in {"r1"} |-> {}], edgeDone |-> {}, stopped |-> FALSE],
  [i |-> 2, action |-> "Take", physicalSeq |-> 1, p |-> "p1", lock |-> "p1", hook |-> "none", procPhase |-> "hook"],
  [i |-> 3, action |-> "Hook", physicalSeq |-> 1, p |-> "p1", lock |-> "p1", hook |-> "p1", procPhase |-> "ready"],
  [i |-> 4, action |-> "ReadControl", physicalSeq |-> 1, p |-> "p1", observedSeq |-> 1, observedStopped |-> FALSE],
  [i |-> 5, action |-> "Fold", physicalSeq |-> 1, p |-> "p1", procCopy |-> <<0,0,0>>, procFold |-> 0, procCarry |-> <<1,2,3>>, procPhase |-> "select"],
  [i |-> 6, action |-> "Select", physicalSeq |-> 1, p |-> "p1", procBatch |-> <<1,2>>, procCarry |-> <<3>>, procPhase |-> "apply"],
  [i |-> 7, action |-> "CaptureFence", physicalSeq |-> 1, p |-> "p1", expectedSeq |-> 1, procFenced |-> TRUE],
  [i |-> 8, action |-> "Report", physicalSeq |-> 2, c |-> 1, gens |-> <<1,0,0>>, done |-> {}, events |-> <<[kind |-> "report", id |-> 4]>>],
  [i |-> 9, action |-> "Refuse", physicalSeq |-> 3, p |-> "p1", procBatch |-> <<2>>, procPutback |-> <<1>>, expectedSeq |-> 1, receiptSeq |-> 3, procFenced |-> FALSE],
  [i |-> 10, action |-> "ReadControl", physicalSeq |-> 3, p |-> "p1", observedSeq |-> 3, observedStopped |-> FALSE],
  [i |-> 11, action |-> "CaptureFence", physicalSeq |-> 3, p |-> "p1", expectedSeq |-> 3, procFenced |-> TRUE],
  [i |-> 12, action |-> "Apply", physicalSeq |-> 4, p |-> "p1", gens |-> <<1,1,0>>, done |-> {2}, events |-> <<[kind |-> "report", id |-> 4],[kind |-> "tick", id |-> 2]>>, procBatch |-> <<>>, procApplied |-> 1, expectedSeq |-> 3, receiptSeq |-> 4, procFenced |-> FALSE],
  [i |-> 13, action |-> "EndTick", physicalSeq |-> 4, p |-> "p1", procCarry |-> <<1,3>>, procBatch |-> <<>>, procPutback |-> <<>>, procPhase |-> "ready"],
  [i |-> 14, action |-> "PreparePush", physicalSeq |-> 4, p |-> "p1", r |-> "r1", push |-> [p \in {"p1","p2"} |-> [r \in {"r1"} |-> IF p = "p1" THEN [stage |-> "sending", lo |-> 1, hi |-> 2] ELSE [stage |-> "idle", lo |-> 0, hi |-> 0]]]],
  [i |-> 15, action |-> "SendOK", physicalSeq |-> 4, p |-> "p1", r |-> "r1", sent |-> [r \in {"r1"} |-> 1..2], push |-> [p \in {"p1","p2"} |-> [r \in {"r1"} |-> IF p = "p1" THEN [stage |-> "sent", lo |-> 1, hi |-> 2] ELSE [stage |-> "idle", lo |-> 0, hi |-> 0]]]],
  [i |-> 16, action |-> "SaveCursor", physicalSeq |-> 5, p |-> "p1", r |-> "r1", cursor |-> [r \in {"r1"} |-> 2], push |-> [p \in {"p1","p2"} |-> [r \in {"r1"} |-> IF p = "p1" THEN [stage |-> "idle", lo |-> 0, hi |-> 0] ELSE [stage |-> "idle", lo |-> 0, hi |-> 0]]]],
  [i |-> 17, action |-> "LoseLock", physicalSeq |-> 5, p |-> "p1", lock |-> "none", hook |-> "none", procPhase |-> "ready"],
  [i |-> 18, action |-> "NoticeLost", physicalSeq |-> 5, p |-> "p1", lock |-> "none", hook |-> "none", procPhase |-> "down"],
  [i |-> 19, action |-> "Take", physicalSeq |-> 5, p |-> "p2", lock |-> "p2", hook |-> "none", procPhase |-> "hook"],
  [i |-> 20, action |-> "Hook", physicalSeq |-> 5, p |-> "p2", lock |-> "p2", hook |-> "p2", procPhase |-> "ready"],
  [i |-> 21, action |-> "ReadControl", physicalSeq |-> 5, p |-> "p2", observedSeq |-> 5, observedStopped |-> FALSE],
  [i |-> 22, action |-> "Fold", physicalSeq |-> 5, p |-> "p2", procCopy |-> <<1,1,0>>, procFold |-> 2, procCarry |-> <<1,3>>, procPhase |-> "select"],
  [i |-> 23, action |-> "Select", physicalSeq |-> 5, p |-> "p2", procBatch |-> <<1,3>>, procCarry |-> <<>>, procPhase |-> "apply"],
  [i |-> 24, action |-> "CaptureFence", physicalSeq |-> 5, p |-> "p2", expectedSeq |-> 5, procFenced |-> TRUE],
  [i |-> 25, action |-> "Apply", physicalSeq |-> 6, p |-> "p2", gens |-> <<2,1,0>>, done |-> {1,2}, events |-> <<[kind |-> "report", id |-> 4],[kind |-> "tick", id |-> 2],[kind |-> "tick", id |-> 1]>>, procBatch |-> <<3>>, procApplied |-> 1, expectedSeq |-> 5, receiptSeq |-> 6, procFenced |-> FALSE],
  [i |-> 26, action |-> "ReadControl", physicalSeq |-> 6, p |-> "p2", observedSeq |-> 6, observedStopped |-> FALSE],
  [i |-> 27, action |-> "CaptureFence", physicalSeq |-> 6, p |-> "p2", expectedSeq |-> 6, procFenced |-> TRUE],
  [i |-> 28, action |-> "Apply", physicalSeq |-> 7, p |-> "p2", gens |-> <<2,1,1>>, done |-> {1,2,3}, events |-> <<[kind |-> "report", id |-> 4],[kind |-> "tick", id |-> 2],[kind |-> "tick", id |-> 1],[kind |-> "tick", id |-> 3]>>, procBatch |-> <<>>, procApplied |-> 2, expectedSeq |-> 6, receiptSeq |-> 7, procFenced |-> FALSE],
  [i |-> 29, action |-> "EndTick", physicalSeq |-> 7, p |-> "p2", procCarry |-> <<>>, procBatch |-> <<>>, procPutback |-> <<>>, procPhase |-> "ready"]
>>
====================================================
