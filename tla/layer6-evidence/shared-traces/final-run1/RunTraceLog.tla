---------------- MODULE RunTraceLog ----------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", lock |-> "none", hook |-> "none", gens |-> <<0,0,0>>, done |-> {}, events |-> <<>>, cursor |-> [r \in {"r1"} |-> 0], sent |-> [r \in {"r1"} |-> {}], edgeDone |-> {}],
  [i |-> 2, action |-> "Take", p |-> "p1", lock |-> "p1", hook |-> "none", procPhase |-> "hook"],
  [i |-> 3, action |-> "Hook", p |-> "p1", lock |-> "p1", hook |-> "p1", procPhase |-> "ready"],
  [i |-> 4, action |-> "Fold", p |-> "p1", procCopy |-> <<0,0,0>>, procFold |-> 0, procCarry |-> <<1,2,3>>, procPhase |-> "select"],
  [i |-> 5, action |-> "Select", p |-> "p1", procBatch |-> <<1,2>>, procCarry |-> <<3>>, procPhase |-> "apply"],
  [i |-> 6, action |-> "Apply", p |-> "p1", gens |-> <<1,0,0>>, done |-> {1}, events |-> <<[kind |-> "tick", id |-> 1]>>, procBatch |-> <<2>>, procApplied |-> 1],
  [i |-> 7, action |-> "Report", c |-> 2, gens |-> <<1,1,0>>, done |-> {1}, events |-> <<[kind |-> "tick", id |-> 1],[kind |-> "report", id |-> 4]>>],
  [i |-> 8, action |-> "Refuse", p |-> "p1", procBatch |-> <<>>, procPutback |-> <<2>>],
  [i |-> 9, action |-> "EndTick", p |-> "p1", procCarry |-> <<2,3>>, procBatch |-> <<>>, procPutback |-> <<>>, procPhase |-> "ready"],
  [i |-> 10, action |-> "PreparePush", p |-> "p1", r |-> "r1", push |-> [p \in {"p1","p2"} |-> [r \in {"r1"} |-> IF p = "p1" THEN [stage |-> "sending", lo |-> 1, hi |-> 2] ELSE [stage |-> "idle", lo |-> 0, hi |-> 0]]]],
  [i |-> 11, action |-> "SendOK", p |-> "p1", r |-> "r1", sent |-> [r \in {"r1"} |-> 1..2], push |-> [p \in {"p1","p2"} |-> [r \in {"r1"} |-> IF p = "p1" THEN [stage |-> "sent", lo |-> 1, hi |-> 2] ELSE [stage |-> "idle", lo |-> 0, hi |-> 0]]]],
  [i |-> 12, action |-> "SaveCursor", p |-> "p1", r |-> "r1", cursor |-> [r \in {"r1"} |-> 2], push |-> [p \in {"p1","p2"} |-> [r \in {"r1"} |-> IF p = "p1" THEN [stage |-> "idle", lo |-> 0, hi |-> 0] ELSE [stage |-> "idle", lo |-> 0, hi |-> 0]]]],
  [i |-> 13, action |-> "LoseLock", p |-> "p1", lock |-> "none", hook |-> "none", procPhase |-> "ready"],
  [i |-> 14, action |-> "NoticeLost", p |-> "p1", lock |-> "none", hook |-> "none", procPhase |-> "down"],
  [i |-> 15, action |-> "Take", p |-> "p2", lock |-> "p2", hook |-> "none", procPhase |-> "hook"],
  [i |-> 16, action |-> "Hook", p |-> "p2", lock |-> "p2", hook |-> "p2", procPhase |-> "ready"],
  [i |-> 17, action |-> "Fold", p |-> "p2", procCopy |-> <<1,1,0>>, procFold |-> 2, procCarry |-> <<2,3>>, procPhase |-> "select"],
  [i |-> 18, action |-> "Select", p |-> "p2", procBatch |-> <<2,3>>, procCarry |-> <<>>, procPhase |-> "apply"],
  [i |-> 19, action |-> "Apply", p |-> "p2", gens |-> <<1,2,0>>, done |-> {1,2}, events |-> <<[kind |-> "tick", id |-> 1],[kind |-> "report", id |-> 4],[kind |-> "tick", id |-> 2]>>, procBatch |-> <<3>>, procApplied |-> 1],
  [i |-> 20, action |-> "Apply", p |-> "p2", gens |-> <<1,2,1>>, done |-> {1,2,3}, events |-> <<[kind |-> "tick", id |-> 1],[kind |-> "report", id |-> 4],[kind |-> "tick", id |-> 2],[kind |-> "tick", id |-> 3]>>, procBatch |-> <<>>, procApplied |-> 2],
  [i |-> 21, action |-> "EndTick", p |-> "p2", procCarry |-> <<>>, procBatch |-> <<>>, procPutback |-> <<>>, procPhase |-> "ready"]
>>
====================================================
