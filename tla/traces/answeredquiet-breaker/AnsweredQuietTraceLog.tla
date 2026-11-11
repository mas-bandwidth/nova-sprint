---------------------------- MODULE AnsweredQuietTraceLog ----------------------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", cause |-> FALSE, js |-> <<  >>, quiet |-> FALSE, ticked |-> FALSE, env |-> 0],
  [i |-> 2, action |-> "Cause", cause |-> TRUE, js |-> <<  >>, quiet |-> FALSE, ticked |-> FALSE, env |-> 1],
  [i |-> 3, action |-> "Tick", cause |-> TRUE, js |-> << "open" >>, quiet |-> FALSE, ticked |-> TRUE, env |-> 1],
  [i |-> 4, action |-> "Answer", cause |-> TRUE, js |-> << "answered" >>, quiet |-> TRUE, ticked |-> FALSE, env |-> 2],
  [i |-> 5, action |-> "Tick", cause |-> TRUE, js |-> << "answered" >>, quiet |-> TRUE, ticked |-> TRUE, env |-> 2],
  [i |-> 6, action |-> "Cause", cause |-> FALSE, js |-> << "answered" >>, quiet |-> TRUE, ticked |-> FALSE, env |-> 3],
  [i |-> 7, action |-> "Tick", cause |-> FALSE, js |-> << "answered" >>, quiet |-> TRUE, ticked |-> TRUE, env |-> 3],
  [i |-> 8, action |-> "Cause", cause |-> TRUE, js |-> << "answered" >>, quiet |-> TRUE, ticked |-> FALSE, env |-> 4],
  [i |-> 9, action |-> "Tick", cause |-> TRUE, js |-> << "answered" >>, quiet |-> TRUE, ticked |-> TRUE, env |-> 4]
>>
=============================================================================
