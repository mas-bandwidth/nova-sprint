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
  other version, naming both numbers, and also keeps a hash of the schema text that made the store, refusing a store made by a different build of the same version, with one exception: a store made by round 16's schema is upgraded in place (round 17, below).
- `sprint.epoch`: one row (`id = 1` by CHECK), `n integer`. Clear is
  `n = n + 1`. Every stream, card and row carries the epoch it was written in;
  the snapshot shows only the current epoch's rows. Nothing is deleted.
- `sprint.streams`: `name text`, `repo text`, `base text`, `held boolean`,
  `pos integer`, `epoch integer`. Primary key `(epoch, name)`.
- `sprint.cards`: `id text`, `kind text` (`producer`, `sentinel`, `work`, `read`),
  `of text` (the producer a consumer belongs to; empty on a producer),
  `stream text`, `state text`, `gen integer`, `attempt integer`,
  `score integer`, `needs text[]` (hard: cannot start until these landed), `after text[]` (soft: may start any time; orders its landing only behind targets that are themselves queued to land, and never holds it behind a target that is not, as in v1 (LESSONS 2.15; PRIORITY D29, M20)), `holder text` (nullable), `author text`,
  `branch text`, `base_sha text`, `prior_head text` (on a work consumer: the
  head the attempt continues from, empty on a first attempt; LESSONS 3.9),
  `head text`, `pr integer`, `result text`
  (`""`, `ok`, `fix`, `fail`, `fault`), `bounces integer` (on a producer:
  the returns that count toward `blocked`; a `fault` never raises it;
  LESSONS 2.24), `cost bigint` (on a consumer: what its run cost, in
  micro-dollars) and `priced boolean` (false when the run's route has no
  price, so an unpriced run is shown as unpriced, never as $0; LESSONS 1.17),
  `priority text NOT NULL DEFAULT 'normal'` (checked against `low`,
  `normal`, `high`, `critical`, `blocker`; the deal order, lowest to
  highest, is Glenn's of 2026-11-06: "There is low, normal, high, read,
  critical, blocker in specific order.", where `read` is the rung every read
  consumer occupies and is never a producer's level; Glenn 2026-11-06: "Default is normal." and
  "Priorities are manually set by request.": a level is declared on a
  producer by a person or by the coordinator on request, never computed or
  raised by the system; a consumer has no level of its own and is ordered by
  its producer's, read from that row, never a second copy; LESSONS 5.3),
  `brief text`, `reason text`, `red boolean NOT NULL DEFAULT false` (on a
  producer: its last read's verdict was red, so a later waiver can require
  it; false on every other kind, by the CHECK `cards_red_check`; mutable,
  so the `cards_fixed` trigger does not name it; round 17), `epoch integer`.
  Primary key `(epoch, id)`. Every column but `holder` and `of` is `NOT NULL` (text
  defaults to the empty string, `pr`, `bounces` and `cost` to 0, `priced` to
  false). `state`, `kind` and `result` are
  checked against their sets. `(epoch, stream)` references `streams`;
  `(epoch, holder)` references `rows`; `(epoch, of)` references `cards` when
  `of` is set. The two kinds (layer 2's producer and consumer cards, Glenn
  2026-09-29 and 2026-11-06) share one table and one write set; what the
  store itself enforces about them (in Go and, where SQL can say it, in the schema's CHECKs and one trigger that refuses any change to `kind`, `of` or `stream`): a sentinel has no brief, no holder, no consumer and no `of`; a producer never has a holder (the holder
  write is refused on a producer, naming it); a consumer's `of` is set at add,
  names a producer of the same epoch, and never changes; a consumer has no
  needs. `gen` starts at 0 and goes up by one on every op that writes the
  card, so an expectation on gen catches any change; it is a version, not an
  attempt. `attempt` is raised only by its own write (layer 2 says when).
  `score` is the work order: set at add, changed only by the explicit score
  write, never by a move. A card is added in state `waiting`, gen 0, attempt
  0, no holder; a consumer is added with its holder and the attempt it belongs to in the same op (a producer or sentinel with a non-zero attempt at add is refused).
- `sprint.rows`: `name text`, `kind text` (`worker` or `kin`), `width integer`,
  `state text` (`up`, `down`), `held boolean` (the coordinator's hold: no deals to this row while true, whatever its state; Glenn 2026-11-06: "the states that we have per-machine and per-friend ... up, down, held"), `session text` (the hook's session,
  so a late report from an old session can be told apart), `flaps integer`,
  `gen integer`, `epoch integer`. A row's width has one source, nova-config:
  the run loop writes `set row width` when nova-config's value differs from
  the store's, and nothing else writes it, so a nova-config change reaches the
  store with no hand sync (LESSONS 1.12). Primary key `(epoch, name)`; every column
  `NOT NULL`; `kind` and `state` checked against their sets. `gen` rises by
  one on every op that writes the row, so a row plan is fenced like a card
  plan. There is no heartbeat column and no heartbeat op: presence is the
  bus's fact (`nova-bus who`), read by the run loop at tick time; the store
  records a row's state only when it changes. v1 filled its log with
  heartbeats.
- `sprint.events`: `seq bigint`, `op text unique`, `actor text`, `kind text`,
  `accepted boolean`, `reason text`, `before jsonb`, `after jsonb`,
  `receipt jsonb`, `at timestamptz`, `epoch integer`. Append-only. One row per
  operation that entered the store, accepted or refused. Because writers run
  one at a time (below), `seq` is commit order with no gaps, so a reader's
  cursor is a committed prefix: `Events(afterSeq)` never skips an event.
  `before` and `after` hold the full row of each touched stream, card, row or
  policy version, so folding the accepted events in `seq` order from the first
  event rebuilds the snapshot. `receipt` holds the receipt without its
  after-image (round 15): Replay, a repeated Apply and Events rebuild
  `Receipt.After` from a copy of the event's `after` column, so a receipt
  reads as it did; a row written before round 15, whose receipt carries its
  own `after`, still reads the same. The events are layer 4's outbox: a deal
  is an accepted event, and the bus push reads from a cursor.
- Indexes beyond the primary keys (`events.op` is unique), each serving one
  store query that would otherwise scan the epoch's cards (rounds 15 and
  16): `cards_working (epoch, holder) WHERE state = 'working'`, the width
  count on every move into working and every width write (readers' per-row
  working counts use it too); `cards_live (epoch, holder, id COLLATE "C")
  WHERE state IN ('ready', 'working')`, the tier check on a tiers write. Every
  other store read is by a primary key or a prefix of one (a snapshot's
  epoch), and the foreign keys and trigger lookups reach primary keys, so
  nothing else is indexed; a reader that needs another index (a dashboard
  query by stream or state) names it in its own layer.
- `sprint.policy`: `version serial`, `kind text` (`script` or `data`),
  `name text`, `body text`, `active boolean`, `actor text`, `at timestamptz`.
  Global, not epoch-scoped: a clear keeps the policy. At most one version is
  active (a unique partial index). Layer 5 fills it; the table exists now so
  there is one store. Every event records `policy integer`, the active
  version when it applied, so a decision can always be traced to the rules
  that made it (promise 6; PROMISE reviews 2026-11-06).
- `sprint.judgements`: `id text`, `kind text` (a word for the dashboard:
  `red`, `fix`, `stuck`, `blocked`, `flap`, `conflict`, `landed-sentinel`,
  and the rest layer 3 names), `about text` (the card, stream or row it
  concerns), `question text`, `command text` (the exact verb that answers
  it), `group_key text` (judgements with one key are shown and pushed as
  one), `state text` (`open`, `answered`, `expired`), `deadline timestamptz`
  (nullable), `answer text`, `actor text`, `opened_at timestamptz`,
  `closed_at timestamptz` (nullable), `epoch integer`. Primary key `(epoch,
  id)`. A judgement is a stored record, not a bus message: the push is made
  from the row and the row outlives the push, so nothing owed is forgotten
  (promise 1), the dashboard's "needs you" column reads this table (promise
  5), and the coordinator is told once per group (promise 6). Writes: put
  judgement (refused if the id exists); answer judgement (state answered,
  answer, actor); expire judgement (state expired, reason in answer). A
  close is refused inside the write unless the judgement is open, and when
  the op's time is before `opened_at` ("judgement J opened at T1, cannot
  close at T0"), so a judgement closes at most once whatever the plan
  carried (the model's `JudgementClosedOnce`; the code's rule, adopted
  2026-11-06). Condition: judgement `J` is open. Layer 3 opens them, layer 6
  answers them. Judgements belong to their epoch and are not carried across
  a clear.
- `sprint.cursors`: `name text` (primary key), `seq bigint`. A subscriber's
  place in the event log (the run loop's push cursor per row, the
  dashboard's feed cursor), saved so a restart can resume where it stopped.
  The store gives the primitive, a monotone cursor bounded by the log; that
  a restart never pushes twice or skips is layer 4's guarantee, built on it
  by advancing the cursor only after a push is confirmed (promise 6). Global,
  as seq is: a clear keeps it, and a subscriber resumes where it was, so the
  old epoch is not pushed again by a subscriber that kept its cursor. Write:
  set cursor (refused below the cursor's current
  value, and above the last event's seq: "cursor X cannot pass the log at
  42"); an absent cursor reads as 0.

Two columns for the tier (Glenn 2026-11-06: "flash, pro, heavy, frontier ...
making sure the right models did the work"): on cards, `tier text` (one of
the four; a producer's, set at add from the brief; a consumer inherits its
producer's at add); on rows, `tiers text[]` (the tiers this row serves; a
bench row's from its model, a kin's from its own word), with `model text` and
`harness text` as labels for policy and the dashboard. The put-card write
refuses a consumer whose holder row does not serve its tier (read from the
row inside the transaction), as does a move into ready or working, and the
row writes refuse to drop a tier a live consumer of the row needs, so a
consumer on a row that does not serve its tier is refused by the store,
never by layer 2's good behaviour. And the card contract's structured fields as
columns, since the table is the structure: `paths text[]`, `done_when
text[]`, and `prose text` (the task a mind reads), beside `brief text` which
keeps the whole brief as given.

### Card states

The closed set, locked by Glenn 2026-09-29 plus one exit and one consumer
state: `waiting`, `ready`, `working`, `review`, `merging`, `landed`, `fail`, `blocked`,
`done`. `landed` means the code is on the base branch and is final. `fail` is
the record of a card that stopped any other way (cancelled, replaced, its need
failed, or a consumer withdrawn) with the reason kept. `done` is a consumer
that reported. `blocked` is a producer that bounced too many times (Glenn
2026-11-05: "if a card flaps around more than n times, it goes into blocked
primary card state. blocked -> waiting -> ready"; coloured black on the
board); it is not final, and only the coordinator's unblock leaves it. Which
transitions are legal, and which states each kind may use, is layer 2's;
to this layer a state outside the set is `ErrInvalid`. A card is live when it is in
`ready` or `working`; live is what layer 2's producer invariant counts (one
live work consumer). Width counts consumers in `working` only, never `ready`
(below; LESSONS 3.3).

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
`X` has holder `R` (or none); card `X` is red or not red (`CardRed`); every need of card `X` is `landed` (read from the
card's own `needs` inside the transaction, never from a caller's list); row
`R` is in state `S`; row `R` is at gen `G` (`RowGen`); row `R` is held or not held (`RowHeld`); stream `S` is held or not held; card `X` does not exist;
the store is at seq `N` (a fence
for a plan that needs nothing at all to have changed since its snapshot). The
op's epoch must equal the store's epoch; a mismatch is a refusal. Every
condition is checked inside the same transaction as the writes, against the
rows as they are at that moment.

Writes (closed set), one field each: put stream (insert or overwrite: repo,
base, pos; `pos` is the order streams land in); set stream held; put card
(add: stream, score, needs, brief; refused if the card exists; every need must
name a different card of the same epoch, no duplicates); set card state; a
consumer's holder is fixed at add; there is no holder write (Johnny's review,
2026-11-06: with a holder write, a working card could drop its holder and
keep the slot); put card for a consumer names a row of the same epoch, a
producer or sentinel never has one, and a `done` or `fail` consumer keeps its
holder for the record and does not count. Width caps work in progress, not what is queued (LESSONS 3.3;
PRIORITY D21; ruled by Rowan 2026-11-06): the count is the consumers a row
holds in `working`, never those in `ready`. A state write that takes a
consumer into `working` is
refused when that row already holds as many `working` cards as its width,
counted at the moment of the write and not counting the card being written,
so one op with two such writes to a width-one row is refused on the second
and nothing lands, and no path over-fills a row; there is no separate width
condition, the write is the check. How many `ready` consumers a row may hold
ahead of its width is policy's number (PRIORITY D21), never the store's;
set card author; set
card branch; set card base_sha; set card head; set card
pr; set card result (a consumer only); set card bounces (a producer only); set card prior_head (a work consumer only); set card attempt (never a consumer: its attempt is set at add); set card cost (a consumer only; cost and priced
together, one field; each kind-specific write is refused on the wrong kind, naming the kind and the write, and the schema carries the matching CHECKs); set card reason; set card score; set card red (`SetCardRed`: a producer only, refused on a consumer naming
its producer as the place to record the verdict and on a sentinel as never
read, whatever the value; a same-value write is refused like every set
write); set card priority (`SetCardPriority`: one
of the five levels, refused on a consumer, written only by the verb a
person or the coordinator runs on request); set card needs; set card after (same rule as needs); put row (insert or
overwrite: kind, width, session); set row width; set row held; set row state; set row
session; set row flaps; put policy version; set active policy version
(deactivates the one active before it in the same transaction). An op that
writes a card raises that card's gen by one, however many of its writes
touch the card, and the same for a row: gen counts the operations that
changed it. Put row and set row width refuse a width below the number of
`working` cards the row holds. Together, the refusals make "working holders
never exceed width" true by every path. The condition `RowGen(R, G)` fences a row
plan as `CardGen` fences a card plan. Every integer a write carries must fit the column
(PostgreSQL `integer`, 32-bit signed; `cost` alone is `bigint`, micro-dollars, 0..MaxInt64); a value outside it is `ErrInvalid`
from Fake and PG alike, never `ErrUnknown`. A snapshot is the caller's own
copy: editing it never changes the store (the Fake copies every slice).
Set card needs follows the add rule (other cards of the same epoch, no
duplicates); a cycle through needs is layer 2's refusal. The op's epoch is
checked on a clear too: a clear names the epoch it closes, so a retried clear
with a new id after the first landed is refused, not applied twice. The
clear's event carries the carried-forward rows in its after-image, which is
how rebuild reproduces the copy. A set write whose target does not exist is a
refusal. Writes in one op
apply in order, so a later write sees a row an earlier one put. Two writes to
one field in one op are invalid. `kind` on the op is a label for the log; the
writes are the truth, and rebuild reads the writes' after-images, never the
label.

Writers run one at a time: `Apply` and `Clear` begin by taking one lock
(`LOCK TABLE sprint.epoch`) and hold it to commit, at `READ COMMITTED`. A
sprint sees a few operations a second at most; one writer at a time is the
simplest thing that makes every condition exact and every seq commit order.
There are no serialization retries because there are no serialization
failures. Nothing that waits on the outside world runs while the lock is
held: no network I/O, no git, no bus send, no read of a whole history; a
caller does that work before it calls `Apply` and passes the result in, so
one slow call never stalls every writer (LESSONS 3.23).

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
4. `ErrUnknown`: the outcome is not known: the context is done, the
   connection was lost after a request may have reached the server, or any
   error came during or after COMMIT. The caller calls `Replay(opID)`: if the
   op landed, its receipt comes back; if not, the caller may `Apply` again
   with the same id. Exit code 2 at the CLI. Every other error inside the
   transaction, before COMMIT, means it rolled back and the outcome is known:
   an error the server returned, whatever its SQLSTATE, or one the client
   raised (a scan, an encode, a stored receipt that does not decode, a
   counter at the top of its column). It is a plain error naming the cause,
   never `ErrUnknown` (the second cold read found a 3 KB id and a missing
   column reported as unknown, the third a scan error and a full gen, each of
   which sends a caller into a replay loop that can never end).

Ids and names (op id, card id, row and stream names, actor, kind label,
policy name) are at most 128 bytes, `ErrInvalid` above, so no index can
refuse one; brief, reason, body, head, branch and note are unbounded text
with no index on them.

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
carried forward into the new epoch in the same transaction (copied: same name, held flag,
kind, width and session, state `down`, flaps 0), because the rows are the
fleet, not the sprint's work, and a clear must not make every worker and kin
register again. Nothing is ever deleted: a clear opens a new epoch, it does
not remove the old one.

Schema version 2 (2026-11-06) is the version with the two card kinds:
`kind`, `of`, `author`, `base_sha`, `result`, the state `done`, and row
`gen`; with the lessons placed the same day it also carries `prior_head`,
`bounces`, `cost`, `priced` and the result `fault` (LESSONS 1.17, 2.24,
3.9), `priority` (Glenn's rulings of 2026-11-06, above), and from the promise
round the same day `tier`, `paths`, `done_when`, `prose`, the rows' `tiers`,
`model` and `harness`, `sprint.judgements`, `sprint.cursors` and
`events.policy`. Nothing is owed to a later layer; a later column arrives with
a schema version bump and a design change, never as a special case. The
`cards_fixed` trigger fixes `id`, `epoch`, `kind`, `of`, `stream`, `holder`
and `tier` once a card exists, and rows and streams have their name and epoch
fixed the same way; a consumer's `stream` equals its producer's. The schema
hash in `sprint.meta` is taken over the schema text with comments and blank
lines stripped, so a comment edit refuses no store.

`Replay` returns `(receipt, found, err)`: `found` is false when no event has
that id. A refused event has no before or after images; rebuild folds only
accepted events. `Events` crosses epochs (each event carries its epoch, the
clear event included), so a tail sees the clear.

One PostgreSQL role, `sprint`, owns the schema and is the only writer; the
run loop and the verbs connect as it. Workers, kin and the bus never get the
DSN: they talk to the store only through the engine's verbs. Making the role
is nova-fleet's postgres play (layer 6 adds the row).

Cost is recorded on the consumer that spent it (work, read, and the fix
attempts alike) by the op that reports it; a producer's, a stream's and the
sprint's cost are sums read from those rows, never a second record kept
beside them (LESSONS 1.17, 1.1).

Owed to layer 6, not part of this layer's lock: a backup (a dump of the
`sprint` schema, named with the epoch and seq it was taken at) and a restore
into an empty schema, run by a layer 6 verb and checked by rebuilding from
the event log; any future op that would lose rows is taken only after a
backup (LESSONS 1.16). The gate is `nova-sprint up` on the first real store.

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
  state; conditions checked against the same state the writes change; ops
  land only in their epoch; `working` holders never exceed width). Reversed witnesses
  in `tla/witnesses/`, each a variant that TLC must fail: a write that lands
  without its event row; a replay that applies again; a condition checked
  against a stale snapshot (lost update); a second holder; an op without the
  epoch check landing after a clear; a width check on a stale count. Reach
  checks in `tla/reach/`, each also a required violation, show the passing
  run really reaches a take, a refusal, a recovery after a transport failure,
  a stale-epoch refusal, a needs-ready deal and a full row. `tla/ci-tlc.sh`
  runs all of them and fails on any wrong outcome.
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
- Owed to layer 6, not part of this layer's lock: a load test, functional
  tier, at four times live size (thousands of cards, their consumers, a long
  event log), timing `Snapshot` and `Apply` against v1's bound "the tick
  stays under one second at load"; the per-call numbers are unmeasured until
  then (LESSONS 1.15). The gate is the first dogfood sprint.
- A cold reader on the final head, then the layer is locked.

## What the lock approves (2026-11-06)

The layer 1 lock approves the tested database primitive: the schema, the
operations, the four outcomes and the seven guarantees, as held by the
conformance suite on Fake and PostgreSQL (C and ICU collations), the
functional tests, the model with its witnesses and reach checks, five cold
reads and the friends' reads. Three things named in this file are deferred
to later, named gates and are not claimed by the lock: the backup and
restore verb and the load test (layer 6's `up` and the first dogfood sprint),
and "used for real work", which no layer can show before the first sprint
runs on it. (Stella's READ5, 2026-11-06.)

## Size

About 2.8k lines of Go plus 0.25k of SQL, measured (`wc -l`, tests excluded:
2,829 Go and 255 SQL at round 12); the first estimate, 1.5k from the operation
count, left out the guarantees twelve rounds of review added: the full-body
check before any read (closed sets, limits, empty keys), the write table, the
unknown rule and its known cases, the counter bounds, tier serving, the
same-value refusals, the schema hash and the foreign-schema check, and the
triggers and CHECKs that hold the contract in SQL as well as in Go.

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
- Epochs start at 1, so an op whose epoch is 0 is one that forgot to set it
  (`ErrInvalid`), never a match.
- A row that is put for the first time starts `down`; it is up when the run
  loop sees its hook on the bus.
- `put card` sets stream, score, needs and brief, so an op may put a card and
  set its state in the same transaction (add and ready at once); put card plus
  set score in one op is two writes to one field and invalid.
- The test servers run with `LC_ALL=C`: PostgreSQL 18 on macOS refuses to
  start under a shell with no valid locale.

Recorded with round 9 (2026-11-06): `events.policy` is the version active when the op was checked, before its writes, so the op that activates a policy records the version before it; opening and answering a judgement in one op is `ErrInvalid` (two writes to one state); `tier` on a card is fixed at add (in the trigger), a consumer's equal to its producer's; the schema hash in `sprint.meta` is computed from `schema.sql` at build.

Recorded with round 10 (2026-11-06, the lock round): tier serving is the writes' own check (put card, a move into ready or working, and the row writes; no condition); a consumer's stream is its producer's; cursors are global and carried by a clear; the unknown rule is the one above; a gen, epoch or policy version that would pass 2147483647 is a plain error naming it, in both stores; Migrate refuses a schema `sprint` it did not make; the schema hash ignores comments, indentation and blank lines; a condition on a state outside its set is `ErrInvalid`; setting a card to the state it is in is refused; the triggers fix every key and epoch and keep the log append-only, and every count is 0 or more by a CHECK.

Recorded with round 11 (2026-11-06, from the fourth cold read): reasons that depend on an order of names use byte order in both stores (PG `COLLATE "C"`), checked by the whole conformance suite on an ICU en-US database; the log and the policy versions refuse TRUNCATE, and a policy version is immutable but for `active` and never deleted; Migrate's foreign-schema check counts types and functions as well as relations; a value outside its closed set and an empty key are `ErrInvalid` in a write exactly as in a condition; every set write that would change nothing is refused (no gen moves); the schema hash strips a `--` comment only outside a single-quoted literal; each write is one entry in the write table (`specOf`).

Recorded with round 12 (2026-11-06, from the fifth cold read): every name an op points at (a card's stream, needs and after) takes the key rule; tiers, needs and after are sets kept in canonical order in both stores; Migrate's foreign-schema check counts any object that depends on the namespace (`pg_depend`); integer conditions are range-checked like writes; `SetCardPriority{""}` is normal; each condition is one entry in the condition table (`condOf`); the schema hash also skips double-quoted identifiers, and scans dollar quotes as code (the stated limit).

Round 13 (2026-11-06, from building layer 2): `cards.review integer` (a
producer's count of entries into review; a read consumer's copy at cut, fixed)
with `SetCardReview` (producer only, +1), and `rows.reason text` written by
`SetRowHeld`. Schema version 2 still (no live store).

Round 14 (2026-11-06, from the first cold read of layer 2): `cards.finding
text NOT NULL DEFAULT ''` (producer: the last read's finding; work consumer: a
copy at cut, fixed), written by `SetCardFinding` (producer only).

Recorded with round 17 (2026-11-06; Glenn-approved context: a red read
verdict is recorded on the producer so that a later waive can require it,
where the card machine had been using `pr` for it): `cards.red boolean NOT
NULL DEFAULT false` with the CHECK `cards_red_check` (`kind = 'producer' OR
NOT red`); the write `SetCardRed{Card, Red}`, producer only (a consumer is
refused "card u1 is of kind work; SetCardRed is only for a producer: record
the verdict on its producer c1", a sentinel "... and a sentinel is never
read"; a missing card "card c9 does not exist"; the same value "card c1
already has the given red"; each a logged refusal, like every kind rule,
not `ErrInvalid`, which is only for an op the store never reads); the
condition `CardRed{Card, Red}` ("card c1 is not red, expected red"); `red`
in the snapshot, every after-image and so `Rebuild`, in Fake and PG alike
(an event written before round 17 has no `red` in its images and reads as
false, which is what every card was). The `cards_fixed` trigger is
unchanged. The column is added by an `ALTER TABLE sprint.cards ADD COLUMN
IF NOT EXISTS red ...` after the `CREATE TABLE`, not inside it, so a new
store and an upgraded one get it the same way. The schema hash moves with
it. What `Migrate` does on an existing round-16 store (hash
`round16Hash`, the hash of `store/testdata/schema-round16.sql`, the
schema at 36c1bbd6), in its one transaction under the advisory lock: it
finds version 2 and that hash and takes it instead of refusing; runs
`schema.sql`, in which every `CREATE ... IF NOT EXISTS` is a no-op, every
`CREATE OR REPLACE FUNCTION` and trigger is replaced by its identical
text, and the `ALTER` adds `red` (PostgreSQL stores the default without
rewriting the table, and the CHECK is validated against the existing rows,
all false, under an ACCESS EXCLUSIVE lock on `sprint.cards` for the
transaction); then writes the new hash into `sprint.meta` and commits. The
events are not touched. A second `Migrate` is a no-op; a store at any
other hash is still refused. Model: `red` is a plain card attribute
(`SetRed`, `CardRed`), the invariant `RedOnlyOnProducers`, the reversed
witness `StoreRedOnConsumer` (the write without its kind check) and the
reach checks `NeverRed`, `NeverRedKindRefused` and `NeverSameRedRefused`.

Model rounds 6 and 7 (2026-11-06): same-value refusal, policy immutability and the append-only log modelled; 51 runs. One known difference, harmless: the store inserts an absent cursor set to 0; the model treats an absent cursor as 0 and would refuse it.

## Scale: hundreds or thousands of cards (Glenn, 2026-11-06)

"remember, we'll have sprints with hundreds or thousands of cards." So the
whole-epoch `Snapshot` is the cold-start read only. In steady state the run
loop and the dashboard keep a local copy of the state and advance it from
`Events(afterSeq)` with their saved cursor, the same fold `Rebuild` runs, so a
tick or a dashboard refresh reads only what changed since the last one,
whatever the sprint's size. A snapshot per tick is a defect in layer 3. The
deferred load test's target is thousands of producers with their consumers
and judgements at a one-second tick, every worker reporting at once, with
Apply latency under the writer lock and the incremental read both measured.

## Measured at the bar (2026-11-06, hetzner2, PostgreSQL 18.6, fsync on, bench shared with TLC)

After round 15 (indexes, each event's after-image stored once; round 16
kept the two indexes a store query uses and re-measured on a quiet bench, below), 5,000 producers with their consumers, 125,000 events:
tick 119 ms median, 153 max (was 327 / 387); tick while 200 reports/s arrive
132 median, 178 max; report median wait 2.1 ms; 2,000-report burst 2.8 s;
dashboard refresh from the local copy 24 ms; incremental read per tick 13
ms; cold snapshot 45 ms; cold rebuild of the log 4.9 s read plus 83 ms fold;
lock wait 21 ms mean, 168 max. At 2,000 producers every number is at or
under these. Logs: store/testdata/load-*.log. The width count went from 3.8
ms (a sequential scan) to 0.04 ms with one partial index. Remaining tick
cost is about 1,500 one-row round trips; by Glenn's ruling the per-tick
budget absorbs bursts rather than further tuning here.
