---- MODULE InstallTraceLog ----
EXTENDS Naturals
\* Fixed seed 179401, walk 60, scenario 0; actual callback and return observations.
TraceHavePrevious == FALSE
TraceLog == <<
  [i |-> 1, action |-> "Init", active |-> "none", candidate |-> FALSE, checkOK |-> FALSE, version |-> "unread", problem |-> "none", previousPresent |-> FALSE],
  [i |-> 2, action |-> "ReturnFailure", active |-> "none", candidate |-> FALSE, checkOK |-> FALSE, version |-> "unread", problem |-> "external", previousPresent |-> FALSE]
>>
====
