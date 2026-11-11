---------------------------- MODULE BaseRedTraceLog ----------------------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", arg |-> "", how |-> "", fact |-> "none", live |-> TRUE, js |-> <<  >>, quiet |-> FALSE, due |-> FALSE, ticked |-> FALSE, env |-> 0],
  [i |-> 2, action |-> "Read", arg |-> "A", how |-> "A", fact |-> "A", live |-> TRUE, js |-> <<  >>, quiet |-> FALSE, due |-> FALSE, ticked |-> FALSE, env |-> 1],
  [i |-> 3, action |-> "Tick", arg |-> "", how |-> "", fact |-> "A", live |-> TRUE, js |-> <<  >>, quiet |-> FALSE, due |-> FALSE, ticked |-> TRUE, env |-> 1]
>>
=============================================================================
