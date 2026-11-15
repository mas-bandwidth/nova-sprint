---------------------------- MODULE MergeNagTraceLog ----------------------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", arg |-> "", st |-> [c1 |-> "none", c2 |-> "none", c3 |-> "none"], coord |-> FALSE, pushed |-> 0, ticked |-> FALSE],
  [i |-> 2, action |-> "Tick", arg |-> "", st |-> [c1 |-> "none", c2 |-> "none", c3 |-> "none"], coord |-> FALSE, pushed |-> 0, ticked |-> TRUE],
  [i |-> 3, action |-> "Tick", arg |-> "", st |-> [c1 |-> "none", c2 |-> "none", c3 |-> "none"], coord |-> FALSE, pushed |-> 0, ticked |-> TRUE],
  [i |-> 4, action |-> "Enter", arg |-> "c2", st |-> [c1 |-> "none", c2 |-> "young", c3 |-> "none"], coord |-> FALSE, pushed |-> 0, ticked |-> FALSE],
  [i |-> 5, action |-> "Tick", arg |-> "", st |-> [c1 |-> "none", c2 |-> "young", c3 |-> "none"], coord |-> FALSE, pushed |-> 0, ticked |-> TRUE],
  [i |-> 6, action |-> "Enter", arg |-> "c1", st |-> [c1 |-> "young", c2 |-> "young", c3 |-> "none"], coord |-> FALSE, pushed |-> 0, ticked |-> FALSE],
  [i |-> 7, action |-> "Hook", arg |-> "", st |-> [c1 |-> "young", c2 |-> "young", c3 |-> "none"], coord |-> TRUE, pushed |-> 0, ticked |-> FALSE],
  [i |-> 8, action |-> "Hook", arg |-> "", st |-> [c1 |-> "young", c2 |-> "young", c3 |-> "none"], coord |-> FALSE, pushed |-> 0, ticked |-> FALSE],
  [i |-> 9, action |-> "Ages", arg |-> "", st |-> [c1 |-> "young", c2 |-> "old", c3 |-> "none"], coord |-> FALSE, pushed |-> 0, ticked |-> FALSE],
  [i |-> 10, action |-> "Ages", arg |-> "", st |-> [c1 |-> "old", c2 |-> "old", c3 |-> "none"], coord |-> FALSE, pushed |-> 0, ticked |-> FALSE],
  [i |-> 11, action |-> "Hook", arg |-> "", st |-> [c1 |-> "old", c2 |-> "old", c3 |-> "none"], coord |-> TRUE, pushed |-> 0, ticked |-> FALSE],
  [i |-> 12, action |-> "Tick", arg |-> "", st |-> [c1 |-> "old", c2 |-> "old", c3 |-> "none"], coord |-> TRUE, pushed |-> 2, ticked |-> TRUE]
>>
=============================================================================
