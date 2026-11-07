---- MODULE InstallTraceLog ----
EXTENDS Naturals
\* Fixed seed 179401, walk 41, scenario 0; actual callback and return observations.
TraceHavePrevious == TRUE
TraceLog == <<
  [i |-> 1, action |-> "Init", active |-> "old", candidate |-> FALSE, checkOK |-> FALSE, version |-> "unread", problem |-> "none", previousPresent |-> TRUE],
  [i |-> 2, action |-> "ReturnFailure", active |-> "old", candidate |-> FALSE, checkOK |-> FALSE, version |-> "unread", problem |-> "external", previousPresent |-> TRUE]
>>
====
