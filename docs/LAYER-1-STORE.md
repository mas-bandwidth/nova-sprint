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

- `sprint.epoch`: one row, `n integer`. Clear is `n = n + 1`. Every other row
  carries the epoch it was written in; the snapshot shows only the current
  epoch's rows. Nothing is deleted.
- `sprint.streams`: `name text`, `repo text`, `base text`, `held boolean`,
  `pos integer`, `epoch integer`. Primary key `(epoch, name)`.
- `sprint.cards`: `id text`, `stream text`, `state text`, `gen integer`,
  `score integer`, `needs text[]`, `holder text` (nullable), `head text`,
  `pr integer`, `brief text`, `reason text`, `epoch integer`. Primary key
  `(epoch, id)`. `state` is checked against the closed set below. `stream`
  references `streams`; `holder` references `rows`. `score` is set once at add
  and never changed by a move; it is the work order.
- `sprint.rows`: `name text`, `kind text` (`worker` or `kin`), `width integer`,
  `state text` (`up`, `down`, `blocked`), `seen timestamptz`, `flaps integer`,
  `epoch integer`. Primary key `(epoch, name)`.
- `sprint.events`: `seq bigserial`, `op text unique`, `actor text`,
  `kind text`, `accepted boolean`, `reason text`, `before jsonb`, `after jsonb`,
  `receipt jsonb`, `at timestamptz`, `epoch integer`. Append-only. One row per
  operation asked for, accepted or refused.
- `sprint.policy`: `version serial`, `kind text` (`script` or `data`),
  `name text`, `body text`, `active boolean`, `actor text`, `at timestamptz`.
  Layer 5 fills it; the table exists now so there is one store.

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
    Snapshot(ctx) (Snapshot, error)                 // the whole current epoch
    Apply(ctx, Op) (Receipt, error)                 // one transaction
    Replay(ctx, opID string) (Receipt, bool, error) // the receipt an op already got
    Events(ctx, afterSeq int64, max int) ([]Event, error)
    Clear(ctx, Op) (epoch int, err error)           // n = n + 1; itself an event
}
```

An `Op` is: `ID` (chosen by the caller, unique forever), `Actor`, `Kind` (a
word for the event log, such as `add`, `take`, `finish`), `At` (the time, passed
in by the caller, never `now()` in SQL), `Expect` (conditions), `Writes`
(a closed set of typed writes), `Note`.

Conditions (`Expect`): card `X` is in state `S` with gen `G`; card `X` has
holder `R` (or none); row `R` is in state `S`; stream `S` is held or not held;
card `X` does not exist; the epoch is `N`. Every condition is checked inside the
same transaction as the writes, against the rows as they are at that moment.

Writes (closed set): put stream; set stream held; put card (add); set card
state, with gen incremented by one; set card holder; set card head and pr;
set card reason; put row; set row state, seen, flaps; put policy version; set
active policy version. Nothing else. A write the engine does not know is a
refusal at the Go level before any SQL runs.

`Apply` returns one of three things:

1. `Receipt{Accepted: true, ...}` with the event seq and the after-state of each
   touched row: every condition held, every write landed, the event row is in
   the same transaction.
2. `Receipt{Accepted: false, Reason: ...}`: a condition failed or a write was
   illegal; nothing changed; the refusal is logged as an event with
   `accepted = false`. Exit code 1 at the CLI.
3. `ErrUnknown`: the transport failed after the request left (connection
   dropped mid-commit). Nothing is known. The caller calls `Replay(opID)`: if
   the op landed, its receipt comes back; if not, the caller may `Apply` again
   with the same id. Exit code 2 at the CLI.

`Replay` of an op id that was accepted or refused returns the stored receipt
and changes nothing. A second `Apply` with an id already in `events` returns
the stored receipt and changes nothing, whatever its body says.

## What it guarantees

1. One operation is one transaction: whole, with its event row, or nothing.
2. Idempotent by op id: the same id never applies twice.
3. A transport failure is reported as unknown, never as unchanged.
4. A card is in exactly one state and has at most one holder. The schema
   enforces this (`state` is one column with a CHECK; `holder` is one column
   with a foreign key), not Go.
5. The log matches the state: replaying every accepted event from an empty
   epoch rebuilds the snapshot. Nothing is deleted except by epoch.
6. Snapshot is one consistent read (one `REPEATABLE READ` transaction) of the
   current epoch.

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
