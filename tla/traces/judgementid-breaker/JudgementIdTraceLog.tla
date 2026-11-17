---------------------------- MODULE JudgementIdTraceLog ----------------------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", js |-> {[ep |-> 3, st |-> "open", cnt |-> 3], [ep |-> 4, st |-> "open", cnt |-> 4], [ep |-> 5, st |-> "open", cnt |-> 5]}, count |-> 5, cause |-> FALSE, due |-> FALSE, ticked |-> FALSE, env |-> 0],
  [i |-> 2, action |-> "Deadline", js |-> {[ep |-> 3, st |-> "open", cnt |-> 3], [ep |-> 4, st |-> "open", cnt |-> 4], [ep |-> 5, st |-> "open", cnt |-> 5]}, count |-> 5, cause |-> FALSE, due |-> TRUE, ticked |-> FALSE, env |-> 1],
  [i |-> 3, action |-> "Tick", js |-> {[ep |-> 3, st |-> "open", cnt |-> 3], [ep |-> 4, st |-> "open", cnt |-> 4], [ep |-> 5, st |-> "open", cnt |-> 5]}, count |-> 5, cause |-> FALSE, due |-> TRUE, ticked |-> TRUE, env |-> 1]
>>
=============================================================================
