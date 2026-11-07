---------------- MODULE SetupHandoffTraceLog ----------------
EXTENDS Naturals, Sequences
TraceLog == <<
  [i |-> 1, action |-> "Init", epoch |-> 0, recordEpoch |-> 0, binding |-> TRUE, seat |-> "admin", lease |-> "free", credential |-> FALSE, schema |-> FALSE, record |-> "absent", exposed |-> {}],
  [i |-> 2, action |-> "Prefix1", seat |-> "admin", lease |-> "free"],
  [i |-> 3, action |-> "Acquire1", lease |-> "ours"],
  [i |-> 4, action |-> "PreparedRole", credential |-> TRUE, schema |-> TRUE, lease |-> "ours"],
  [i |-> 5, action |-> "SaveRecord", record |-> "setup", recordEpoch |-> 0, epoch |-> 0, credential |-> TRUE, schema |-> TRUE, lease |-> "ours"],
  [i |-> 6, action |-> "Release", lease |-> "free", exposed |-> {"status","id"}],
  [i |-> 7, action |-> "Launch2", seat |-> "sprint", lease |-> "free"],
  [i |-> 8, action |-> "Prevalidate", record |-> "setup", recordEpoch |-> 0, epoch |-> 0, binding |-> TRUE, credential |-> TRUE, schema |-> TRUE, lease |-> "free"],
  [i |-> 9, action |-> "Prefix2", lease |-> "free"],
  [i |-> 10, action |-> "Acquire2", lease |-> "ours"],
  [i |-> 11, action |-> "Validate", record |-> "setup", recordEpoch |-> 0, epoch |-> 0, binding |-> TRUE, credential |-> TRUE, schema |-> TRUE, lease |-> "ours"],
  [i |-> 12, action |-> "Verify2", lease |-> "ours"],
  [i |-> 13, action |-> "Effect", lease |-> "free", exposed |-> {"status","id"}, attemptEffect |-> TRUE]
>>
==============================================================
