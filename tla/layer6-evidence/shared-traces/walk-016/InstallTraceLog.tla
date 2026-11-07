---- MODULE InstallTraceLog ----
EXTENDS Naturals
\* Fixed seed 179401, walk 16, scenario 1; actual callback and return observations.
TraceHavePrevious == TRUE
TraceLog == <<
  [i |-> 1, action |-> "Init", active |-> "old", candidate |-> FALSE, checkOK |-> FALSE, version |-> "unread", problem |-> "none", previousPresent |-> TRUE],
  [i |-> 2, action |-> "CheckCall", active |-> "old", candidate |-> TRUE, checkOK |-> FALSE, version |-> "unread", problem |-> "check", previousPresent |-> TRUE],
  [i |-> 3, action |-> "ReturnFailure", active |-> "old", candidate |-> TRUE, checkOK |-> FALSE, version |-> "unread", problem |-> "check", previousPresent |-> TRUE]
>>
====
