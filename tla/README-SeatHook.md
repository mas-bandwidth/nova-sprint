# SeatHook: proof and durable delivery

`SeatHook.tla` checks two specifications independently. `Spec` retains the
challenge, proof, cadence, live-window and command-gate cases. `DurableSpec`
holds those variables fixed and explores the transport protocol in PR115.
`Spec`'s cadence asks one challenge per connection (`NoRechallenge`). `Act`
records transport activity separately from `Renew`, which persists a new lease
only after `RenewStep` units of improvement. Successful ACKs and admitted commands
also record activity. Keepalive neither proves nor acknowledges anything, including
an unproven connection. `KeepaliveCannotProveOrAck` compares the actual action's
before/after proof and ACK state. `RenewFailurePreserves` and
`LapseFailurePreserves` similarly check real state equality on failed writes.
`NoEarlyLapse` checks the last act's silence bound; `SingleLapse` bounds judgments
per session. Successful note, end-record and owner-notice writes are treated as
one step: partial end writes, failed bus delivery and ambiguous store commits are
not verified by this cadence model.

The active control proves at time zero, then advances through eight clock units, past multiple initial
three-unit lease bounds (`ClockReachesBound`), with keepalives at most one unit
apart and a successful due renewal before the next clock step. Those are explicit
bounded scheduling/storage assumptions; weak fairness alone cannot preserve a
finite lease. `ActiveLeaseLive` checks the stored gate deadline as well as
`HookStays` checking the connection. The general control permits arbitrary gaps
and failed renewals; it promises no uninterrupted gate availability. The separate
`SilentSpec` proves a single initially answering session, then allows no acts and
checks eventual lapse with progressing time and fair successful lapse handling.
The model's two-unit renewal threshold illustrates batching, not a literal
conversion of all production times (30 seconds / 15 minutes / 1-minute keepalive).
Go regression tests pin those exact durations and the early gate-expiry example.

Reversed witnesses cover the old proof-based deadline, rechallenge, repeated
lapse, omitted renewal, keepalive proving or waiving ACKs, a failed renewal mutating the stored lease, and a failed lapse-note write
ending the stream. Each is required to violate its named property.
This avoids a product of unrelated timing and delivery states; it is not a
composed proof of the complete hook implementation.

The durable instance has two notification-generation keys, two seat/epoch
scopes, three sessions and at most two outside events (drop/restart, replacement,
or scope change). Keys represent individual note IDs plus review generation,
or computed machine-episode identities. Two keys may share a display group;
this model gives each its own frame and does not verify the grouping algorithm.
In Go, a frame triggered by a fresh sibling can include already-ACKed eligible
siblings as context. `NoAckedReplay` excludes a standalone ACKed obligation; it
does not claim that an ACKed key never appears in such a coalesced frame.
Pending frame IDs and the sent set are session-local. ACKed keys and the push
sequence high-water survive reconnect and server restart. A scope change clears
ACKs and pending items but preserves the sequence high-water. A failed record or
ACK write restores the previous state; failed replacement leaves the old stream
current. Frame sending is a separate action after the record commit, so a drop
between commit and send is explored.

`EligibleUnackedDelivered` checks every currently relevant, eligible, unacked
key: it is eventually sent on the current session, acknowledged, closed, or
superseded by a different scope. A send on a previous session cannot
satisfy the obligation after reconnect. A successful transport write does not
prove client consumption. ACKs may legally cancel an obligation;
there is no assumption that the user eventually acknowledges. Held keys are
excluded from a delivery snapshot, then `HeldEventuallyDue` requires their
release or cancellation. A later review/episode is a new key, rather than reuse
of an ACKed key. A snapshot already committed can still be sent after its source
closes, matching the record-before-frame implementation.

These liveness claims assume outside disruption eventually stops (the bounded
instance allows two events), and weak fairness of reconnect, each successful
record, each successful frame send and each held key becoming due. Thus they
assume eventual store/wire availability and a seat that keeps the hook live.
They make no delivery promise during perpetual restart, failed writes, a held
review that never becomes due, or an unanswered challenge that keeps ending the
hook. Sequence capacity is six, enough for all three delivery phases of two
keys; exhaustion is not used to evade an eligible delivery obligation. This is
a finite instance, not a proof against uint64 overflow.

The `seathook-durable` group includes a passing control and twelve reversed
witnesses: lost ACK history, held delivery, ACKed replay, missing session guard
with a stale cumulative ACK, reset durable sequence, send before record,
failed-look rollback, failed-ACK rollback,
retire before replacement commit, retained epoch ACKs, retained epoch pending
items, and lost reconnect replay. The last witness must fail liveness; the
others must fail their named invariant or action property.
`WriteFailureRollback` and `ReplacementFailureKeepsOld` compare the actual
transport record before and after a failed operation. Their mutants respectively
retain a sent key, erase pending keys, or disconnect the old session; no
mutant-controlled verdict flag participates in those equality checks. ACKs require the current session
token; missing or old tokens refuse before
mutation. Future numeric ACKs above the session's high-water also refuse.
The initial state includes legacy wire ID 1 with durable sequence 0: records
from before this upgrade have no persisted high-water. This makes session
binding necessary even though all new sequence IDs persist monotonically.
Clients must copy the session token from PUSH and supply `--session` with
`hook --ack`; legacy numeric-only ACK commands safely refuse. The
`seathook` group has fifteen witnesses and three passing controls.

Remaining abstraction boundaries include grouped frames, garbage collection of
inactive key history, storage corruption, mutex scheduling, full end-write
failure behavior and composition with proof expiry. Go regression tests cover
implementation paths; TLC results apply only to the model and bounds recorded
in `RUNS.tsv`.
