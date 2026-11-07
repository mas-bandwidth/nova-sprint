---- MODULE InstallTraceLog ----
EXTENDS Naturals
\* Fixed seed 179401, walk 8, scenario 4; actual callback and return observations.
TraceHavePrevious == TRUE
TraceLog == <<
  [i |-> 1, action |-> "Init", active |-> "old", candidate |-> FALSE, checkOK |-> FALSE, version |-> "unread", problem |-> "none", previousPresent |-> TRUE],
  [i |-> 2, action |-> "CheckCall", active |-> "old", candidate |-> TRUE, checkOK |-> TRUE, version |-> "unread", problem |-> "none", previousPresent |-> TRUE],
  [i |-> 3, action |-> "VersionCall", active |-> "old", candidate |-> TRUE, checkOK |-> TRUE, version |-> "requested-version", problem |-> "none", previousPresent |-> TRUE],
  [i |-> 4, action |-> "ReturnSuccess", active |-> "candidate", candidate |-> TRUE, checkOK |-> TRUE, version |-> "requested-version", problem |-> "none", previousPresent |-> TRUE]
>>
====
