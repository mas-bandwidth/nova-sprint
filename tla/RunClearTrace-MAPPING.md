# Clear collector trace relation

Input: RunClearTraceLog.TraceLog, records i/action plus Required(r) fields.
Send also carries n, the one actually confirmed obligation in that batch.
Bounds: two notice events, one Clear, one crash. Physical store positions map
to rank among actual recipient-relevant notices and Clear events. Irrelevant
seed/cursor/refusal rows do not enter that rank; source checks still observe
them. Clear records carry their pre-increment epoch; current epoch is observed.

Prepare binds actual collector input cursor, snapshot epoch and through.
Collected binds actual returned end, complete real batches and notice membership:
needed contains actually batched notice IDs plus the Clear summary; cancelled
contains actual notification callbacks suppressed from the batches. Cross-check
the actual summary count against that set. Do not compute these fields by
executing the model or assuming the snapshot upper bound was fully scanned.
The production collector-completion hook provides end/batches observations.

Between Prepare and the immediately next Collected record, l stays fixed while
unchanged RunClear.Scan runs. The actual observed log, cursor, epoch and through
remain fixed. Scan has exactly one successor: increment scan once, then add
that immutable log index to needed or cancelled based on kind/epoch. It cannot
invent events, reorder or skip indexes. FinishScan requires scan=through and
matches the actual Collected end and complete sets. A wrong result has no
matching successor; TraceMatched refuses that record. Original invariants apply
to every hidden state. This is a deterministic macro-step refinement, not a
claim about internal Go loop order or invented intermediate implementation rows.

The checked stable fixture and ten seeded action-selection walks require the
prefix fit the real scan budget and one
obligation per batch. Send(n) follows actual intended-recipient/message-ID
confirmation; no model Sends are hidden. Cursor success/refusal follows the
actual store receipt. Changed inputs/failures must be retained, not adjusted
to fit the relation. Exact source, input, model and checker bytes are pinned.

Finite matching does not prove general batching, overflow, arbitrary scans,
secrecy or implementation liveness. Clear observation does not atomically
exclude an old prepared send. Those limitations are unchanged.

The ten walks select enabled append-notice, Clear, or Push from actual fixture
store state and cursor ranks. They may perform several push rounds. No-work Push
is not an enabled emitted action. The initial stable case and these walks do
not exercise an in-flight Clear, actual crash, or refused cursor write.
