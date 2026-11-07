---- MODULE InstallTraceLog ----
EXTENDS Naturals
\* Fixed seed 179401, walk 3, scenario 3; actual callback and return observations.
TraceHavePrevious == FALSE
TraceLog == <<
  [i |-> 1, action |-> "Init", active |-> "none", candidate |-> FALSE, checkOK |-> FALSE, version |-> "unread", problem |-> "none", previousPresent |-> FALSE],
  [i |-> 2, action |-> "CheckCall", active |-> "none", candidate |-> TRUE, checkOK |-> TRUE, version |-> "unread", problem |-> "none", previousPresent |-> FALSE],
  [i |-> 3, action |-> "VersionCall", active |-> "none", candidate |-> TRUE, checkOK |-> TRUE, version |-> "requested-version", problem |-> "none", previousPresent |-> FALSE],
  [i |-> 4, action |-> "ReturnFailure", active |-> "none", candidate |-> TRUE, checkOK |-> TRUE, version |-> "requested-version", problem |-> "external", previousPresent |-> FALSE]
>>
====
