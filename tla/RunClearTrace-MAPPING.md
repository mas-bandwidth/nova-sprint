# Clear collector trace relation

RunClearTrace is not bound to code: the pusher has no clear summary or
old-backlog suppression for the relation to observe, and no emitter feeds it
in the trace gate. This file defines the relation for when one does.

Input: RunClearTraceLog.TraceLog, records i/action plus Required(r) fields.
Send rows also carry n, the one actually confirmed obligation in
that batch. The recipient-envelope extension below uses a confirmed member set.
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
A collector-completion hook supplies the end and batches observations.

Between Prepare and the immediately next Collected record, l stays fixed while
unchanged RunClear.Scan runs. The actual observed log, cursor, epoch and through
remain fixed. Scan has exactly one successor: increment scan once, then add
that immutable log index to needed or cancelled based on kind/epoch. It cannot
invent events, reorder or skip indexes. FinishScan requires scan=through and
matches the actual Collected end and complete sets. A wrong result has no
matching successor; TraceMatched refuses that record. Original invariants apply
to every hidden state. This is a deterministic macro-step refinement, not a
claim about internal Go loop order or invented intermediate implementation rows.

The stable fixture and the seeded action-selection walks require that the
prefix fit the real scan budget and one
obligation per batch. Send(n) follows actual intended-recipient/message-ID
confirmation; no model Sends are hidden. Cursor success/refusal follows the
actual store receipt. Changed inputs/failures must be retained, not adjusted
to fit the relation. Exact source, input, model and checker bytes are pinned.

Finite matching does not prove general batching, overflow, arbitrary scans,
secrecy or implementation liveness. Clear observation does not atomically
exclude an old prepared send. Those limitations stand.

The ten walks select enabled append-notice, Clear, or Push from actual fixture
store state and cursor ranks. They may perform several push rounds. No-work Push
is not an enabled emitted action. The initial stable case and these walks do
not exercise an in-flight Clear, actual crash, or refused cursor write.

## One confirmed recipient envelope

The recipient-envelope extension uses `EnvelopeConfirmed` records containing
`members`, `omitted`, `recipient`, `messageID`, cumulative `sent`, and the current
collection's `acknowledged` set. A post-validation delivery hook
supplies collection identity (From/Through/End/Epoch), exact rendered Subject/Body,
original Included/Omitted Notice values, and validated recipient/message ID.
The fixture matches original Notice.Line values uniquely to actual immutable
obligations, checks their presence in the exact body, and rejects ambiguous IDs.
It also binds the delivery to the previously observed collection. Merely seeing
the pre-render collector groups cannot establish final envelope membership.

One concrete confirmed envelope corresponds to a finite sequence of existing
abstract RunClear.Send transitions. With the next trace record fixed, the wrapper
chooses the least remaining member ID, acknowledges it, and repeats until every
member has been acknowledged. Only then does it match that single concrete row
and advance the trace index. No append, Clear, scan, cursor, crash or other external
action can interleave with this expansion. All original invariants hold at every
intermediate abstract state. These are not fabricated per-group physical sends.
The relation preserves the existing RunClear.Next and its invariants unchanged.

Members must belong to the actual collection's needed set, and omitted must equal
the remaining needed IDs. Omitted actionable IDs are never marked sent. The
initial bounded envelope fixtures require omitted={} and fit the real body budget.
Production intentionally advances its cursor over renderer omissions after
sending a summary that directs the recipient to `next`. The current model has no
retrievability/summary coverage branch: such an omitted actionable obligation is
not sent or Clear-cancelled, so that intended cursor advance would not match.
This is an explicit model-scope mismatch, not a claim that production prevents
that advance or a demonstrated implementation bug. General overflow and
`next` discoverability need a separately reviewed relation and remain outside
this checkpoint. Send(n) records stay interpretable for one-obligation-per-send traces; a
recipient-envelope emitter records exactly one EnvelopeConfirmed row per
validated message.
