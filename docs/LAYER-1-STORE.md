# Layer 1: the store

The one place the sprint's state lives. Everything above it reads a snapshot and
asks for one operation; nothing else writes. Approved by Glenn 2026-11-06
("OK I like this. It sounds about right! Let's go."); the choices inside the
layer are Rowan's (Glenn: "the person implementing should be able to select the
tools they want to use").

## What it is for

nova-sprint v2 is a card dealer: cards in streams with needs, dealt to rows
(workers and kin), read, landed, refused when a rule breaks. This layer is the
floor under that. It knows nothing about dealing, reading or landing: it holds
rows of state, applies one checked operation at a time, and remembers every
operation it accepted. Layer 2 (the card machine) says which transitions are
legal; layer 3 (the steps) builds the operations; this layer makes them land
whole or not at all.

## The store

One PostgreSQL schema, `sprint`, in the PostgreSQL that nova-config already
uses. Reached by the same two variables nova-config uses: `NOVA_PG_DSN`
(`postgres://user@host:port/db`, no password) and `NOVA_PG_PASSWORD`. Plain SQL
through pgx; no ORM. The schema file is embedded in the binary and applied with
`CREATE ... IF NOT EXISTS`, so applying it twice is a no-op.

### Tables

- `sprint.meta`: one row (`id = 1` by CHECK), `schema integer`. `Migrate`
  creates the schema at the version the binary knows and refuses a store at any
  other version, naming both numbers.
- `sprint.epoch`: one row (`id = 1` by CHECK), `n integer`. Clear is
  `n = n + 1`. Every stream, card and row carries the epoch it was written in;
  the snapshot shows only the current epoch's rows. Nothing is deleted.
- `sprint.streams`: `name text`, `repo text`, `base text`, `held boolean`,
  `pos integer`, `epoch integer`. Primary key `(epoch, name)`.
- `sprint.cards`: `id text`, `stream text`, `state text`, `gen integer`,
  `attempt integer`, `score integer`, `needs text[]`, `holder text`
  (nullable), `branch text`, `head text`, `pr integer`, `brief text`,
  `reason text`, `epoch integer`. Primary key `(epoch, id)`. Every column but
  `holder` is `NOT NULL` (text defaults to the empty string, `pr` to 0).
  `state` is checked against the closed set below. `(epoch, stream)`
  references `streams`; `(epoch, holder)` references `rows`. `gen` starts at 0
  and goes up by one on every write to the card, so an expectation on gen
  catches any change; it is a version, not an attempt. `attempt` is the
  number of the attempt the holder is on, raised only by its own write when
  a new attempt starts (layer 2 says when). `score` is the work order: set at
  add, changed only by the explicit score write, never by a move. A card is
  added in state `waiting`, gen 0, attempt 0, no holder.
- `sprint.rows`: `name text`, `kind text` (`worker` or `kin`), `width integer`,
  `state text` (`up`, `down`, `blocked`), `session text` (the hook's session,
  so a late report from an old session can be told apart), `flaps integer`,
  `epoch integer`. Primary key `(epoch, name)`; every column `NOT NULL`;
  `kind` and `state` checked against their sets. There is no heartbeat column
  and no heartbeat op: presence is the bus's fact (`nova-bus who`), read by
  the run loop at tick time; the store records a row's state only when it
  changes. v1 filled its log with heartbeats.
- `sprint.events`: `seq bigint`, `op text unique`, `actor text`, `kind text`,
  `accepted boolean`, `reason text`, `before jsonb`, `after jsonb`,
  `receipt jsonb`, `at timestamptz`, `epoch integer`. Append-only. One row per
  operation that entered the store, accepted or refused. Because writers run
  one at a time (below), `seq` is commit order with no gaps, so a reader's
  cursor is a committed prefix: `Events(afterSeq)` never skips an event.
  `before` and `after` hold the full row of each touched stream, card, row or
  policy version, so folding the accepted events in `seq` order from the first
  event rebuilds the snapshot. The events are layer 4's outbox: a deal is an
  accepted event, and the bus push reads from a cursor.
- `sprint.policy`: `version serial`, `kind text` (`script` or `data`),
  `name text`, `body text`, `active boolean`, `actor text`, `at timestamptz`.
  Global, not epoch-scoped: a clear keeps the policy. At most one version is
  active (a unique partial index). Layer 5 fills it; the table exists now so
  there is one store.

### Card states

The closed set, locked by Glenn 2026-09-29 plus one exit: `waiting`, `ready`,
`working`, `review`, `merging`, `landed`, `left`. `landed` means the code is on
the base branch and is final. `left` is the record of a card that stopped any
other way (cancelled, replaced, its need failed) with the reason kept. Which
transitions are legal is layer 2's; this layer only refuses a state outside the
set. (The flapping row goes to row state `blocked`; that is a row state, not a
card state.)

## The operations

The Go API, package `store`:

```go
type Store interface {
    Snapshot(ctx) (Snapshot, error)                 // the whole current epoch, with Seq and Epoch
    Apply(ctx, Op) (Receipt, error)                 // one transaction
    Replay(ctx, opID string) (Receipt, bool, error) // the receipt an op already got
    Events(ctx, afterSeq int64, max int) ([]Event, error) // afterSeq exclusive, seq order, max capped at 1000
    Clear(ctx, Op) (Receipt, error)                 // n = n + 1; same protocol as Apply
    Policy(ctx, version int) (PolicyVersion, error) // the body layer 5 runs
}
```

An `Op` is: `ID` (chosen by the caller, unique forever), `Epoch` (the epoch
the caller planned against; required), `Actor`, `Kind` (a word for the event
log, such as `add`, `take`, `finish`), `At` (the time, passed in by the caller,
never `now()` in SQL), `Expect` (conditions), `Writes` (a closed set of typed
writes), `Note` (kept in the receipt).

Conditions (`Expect`): card `X` is at gen `G`; card `X` is in state `S`; card
`X` has holder `R` (or none); every need of card `X` is `landed` (read from the
card's own `needs` inside the transaction, never from a caller's list); row
`R` is in state `S`; row `R` holds fewer than `W` cards (holders counted
inside the transaction, so two deals cannot overfill a row); stream `S` is
held or not held; card `X` does not exist; the store is at seq `N` (a fence
for a plan that needs nothing at all to have changed since its snapshot). The
op's epoch must equal the store's epoch; a mismatch is a refusal. Every
condition is checked inside the same transaction as the writes, against the
rows as they are at that moment.

Writes (closed set), one field each: put stream (insert or overwrite: repo,
base, pos; `pos` is the order streams land in); set stream held; put card
(add: stream, score, needs, brief; refused if the card exists; every need must
name a different card of the same epoch, no duplicates); set card state; set
card holder (a name or none); set card attempt; set card branch; set card
head; set card pr; set card reason; set card score; set card needs; put row
(insert or overwrite: kind, width, session); set row state; set row session;
set row flaps; put policy version; set active policy version (deactivates the
one active before it in the same transaction). Every card write raises gen by
one. A set write whose target does not exist is a refusal. Writes in one op
apply in order, so a later write sees a row an earlier one put. Two writes to
one field in one op are invalid. `kind` on the op is a label for the log; the
writes are the truth, and rebuild reads the writes' after-images, never the
label.

Writers run one at a time: `Apply` and `Clear` begin by taking one lock
(`LOCK TABLE sprint.epoch`) and hold it to commit, at `READ COMMITTED`. A
sprint sees a few operations a second at most; one writer at a time is the
simplest thing that makes every condition exact and every seq commit order.
There are no serialization retries because there are no serialization
failures.

`Apply` ends one of four ways:

1. `Receipt{Accepted: true, ...}` with the event seq and the after-state of each
   touched row: every condition held, every write landed, the event row is in
   the same transaction.
2. `Receipt{Accepted: false, Reason: ...}`: the epoch was wrong, a condition
   failed, or a write broke a rule (target missing, card exists, bad state
   name); nothing changed; the refusal is logged as an event with
   `accepted = false`. Exit code 1 at the CLI. The id is used: a retry after a
   refusal is a new op with a new id.
3. `ErrInvalid`: the op never entered the store (no id, no epoch, a write type
   outside the closed set, two writes to one field). Nothing is logged. A
   caller bug.
4. `ErrUnknown`: the request may have reached the server and the outcome is
   not known (any failure after the transaction began, held conservatively).
   The caller calls `Replay(opID)`: if the op landed, its receipt comes back;
   if not, the caller may `Apply` again with the same id. Exit code 2 at the
   CLI.

Any other error means the request did not reach the store and nothing
happened. A constraint violation from PostgreSQL during the writes is an engine
defect, not a refusal: the transaction rolls back and the error is returned
with the constraint named.

`Replay` of an op id that was accepted or refused returns the stored receipt
and changes nothing. A second `Apply` with an id already in `events` returns
the stored receipt and changes nothing, whatever its body says: a changed body
is never a new operation. The receipt carries the original op's id, kind,
actor, epoch and at, so a caller can see what it is crediting. Two concurrent
applies with one id are serialized by the lock; the second sees the event row
and returns the first's receipt.

`Clear` is an `Op` with kind `clear`, no writes, and at most the seq fence as
a condition. It is idempotent by id, logged with the epoch it closed, and its
receipt carries the new epoch. Streams and cards of the old epoch stay readable
through `Events` and are not in the next `Snapshot`; policy is kept; rows are
carried forward into the new epoch in the same transaction (copied: same name,
kind, width and session, state `down`, flaps 0), because the rows are the
fleet, not the sprint's work, and a clear must not make every worker and kin
register again. Nothing is ever deleted: a clear opens a new epoch, it does
not remove the old one.

Owed to later layers, named here so they are not lost: an `author` on a card
(the row whose work a read judges, so a row is never dealt its own card to
read) and a `base_sha` at deal are layer 2's columns; row labels for policy
(tier, model, harness) are layer 5's. Each arrives with a schema version bump
and a design change, never as a special case.

`Replay` returns `(receipt, found, err)`: `found` is false when no event has
that id. A refused event has no before or after images; rebuild folds only
accepted events. `Events` crosses epochs (each event carries its epoch, the
clear event included), so a tail sees the clear.

One PostgreSQL role, `sprint`, owns the schema and is the only writer; the
run loop and the verbs connect as it. Workers, kin and the bus never get the
DSN: they talk to the store only through the engine's verbs. Making the role
is nova-fleet's postgres play (layer 6 adds the row).

## What it guarantees

1. One operation is one transaction: whole, with its event row, or nothing.
2. Idempotent by op id: the same id never applies twice.
3. A transport failure is reported as unknown, never as unchanged.
4. A card is in exactly one state and has at most one holder. The schema
   enforces this (`state` is one `NOT NULL` column with a CHECK; `holder` is
   one column with a foreign key), not Go.
5. The log matches the state: folding every accepted event from the first in
   seq order rebuilds the snapshot, policy included. Nothing is deleted except
   by epoch, and an old epoch stays readable in the log.
6. Snapshot is one consistent read (one `REPEATABLE READ` transaction) of the
   current epoch, and carries the seq it was read at and the epoch, so a plan
   made from it can fence on either.
7. A condition is checked against the rows as they are when the writes land:
   writers are serialized, so there is no stale check and no lost update.

What the model's atomic step stands for in SQL: one transaction, from the
lock to the commit. The lock is what makes the correspondence hold; without
it the StaleCheck witness is exactly what `READ COMMITTED` would do.

## How it is checked

- `tla/Store.tla` with `tla/Store.cfg`: operations as atomic steps over cards,
  rows, events and applied op ids; invariants for the guarantees above
  (single state and holder are structural; idempotent replay; log matches
  state; conditions checked against the same state the writes change).
  Reversed witnesses in `tla/witnesses/`, each a variant that TLC must fail:
  a write that lands without its event row; a replay that applies again; a
  condition checked against a stale snapshot (lost update); a second holder.
- Unit tests, package `store`: pure Go against `store.Fake` (an in-memory
  implementation of the same interface used only by tests above this layer),
  time passed in, no sockets, no real time, no network.
- Functional tests, package `store`, file `functional_test.go`: every
  operation and every refusal against a real throwaway PostgreSQL started by
  the test (`initdb` into `t.TempDir()`, free port, `fsync=off`, the same
  recipe as nova-tools `config/functional_test.go`); each test its own server,
  all tests `t.Parallel()`; skipped with a clear message when `initdb` is not
  on PATH. Includes: the unknown-then-replay path (a connection killed between
  request and commit), two concurrent applies on the same expectation (one
  accepted, one refused), rebuild from the event log equals the snapshot.
- A cold reader on the final head, then the layer is locked.

## Size

About 1.5k lines of Go (schema, one function per write and condition,
snapshot, replay, fake). An estimate from the operation count, not a measure.

## Decided inside the layer (Rowan, 2026-11-06), one line each, easy to overrule

- PostgreSQL for state, not Redis: the state wants transactions and history;
  Redis stays the bus.
- Plain SQL via pgx; no ORM, no migrations tool: one `schema.sql`, idempotent.
- No in-memory mode in the binary: `store.Fake` exists for unit tests only.
- The caller supplies time and op ids: that is what makes the layer testable
  without a clock and replayable after a crash.
- Refusals are logged as events: a refused move is a fact the coordinator
  wants to see, and v1 hid them.
- Epoch-scoped rows instead of deletes: clear is one increment and history
  stays readable to the first card.
- The policy table is created now and used by layer 5: one store, one schema
  file.
- Snapshot is the whole epoch: a sprint is thousands of cards at most; one
  read is simpler than a query language, and layers 2 and 3 are pure functions
  over it.
