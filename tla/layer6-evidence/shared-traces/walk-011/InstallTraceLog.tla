---- MODULE InstallTraceLog ----
EXTENDS Naturals
\* Fixed seed 179401, walk 11, scenario 1; actual callback and return observations.
TraceHavePrevious == FALSE
TraceLog == <<
  [i |-> 1, action |-> "Init", active |-> "none", candidate |-> FALSE, checkOK |-> FALSE, version |-> "unread", problem |-> "none", previousPresent |-> FALSE],
  [i |-> 2, action |-> "CheckCall", active |-> "none", candidate |-> TRUE, checkOK |-> FALSE, version |-> "unread", problem |-> "check", previousPresent |-> FALSE],
  [i |-> 3, action |-> "ReturnFailure", active |-> "none", candidate |-> TRUE, checkOK |-> FALSE, version |-> "unread", problem |-> "check", previousPresent |-> FALSE]
>>
====
