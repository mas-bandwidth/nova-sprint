# Layer 1: the store

Package `store`. The one place the sprint's state lives. Everything above it
reads a snapshot and asks for one operation; nothing else writes.

The store knows nothing about dealing, reading or landing. It holds rows of
state, applies one checked operation at a time, and remembers every operation it
accepted or refused. Layer 2 (the card machine) says which card transitions are
legal, layer 3 (the steps) builds the operations, and this layer makes each one
land whole or not at all.

- [Storage](#storage): PostgreSQL schema `sprint`, connection, migration
- [Data model](#data-model): tables, columns, kind rules
- [Operations](#operations): `Op`, conditions, writes
- [Outcomes](#outcomes-of-apply): the four ways `Apply` ends
- [Log, epochs, replay](#log-epochs-and-replay)
- [Guarantees](#guarantees)
- [Schema identity, restore, history validation](#schema-identity-restore-and-history-validation)
- [Fakes](#fakes)
- [Scale](#scale)
- [Testing](#testing)

## Storage

One PostgreSQL schema, `sprint`, in the PostgreSQL that nova-config uses.
`NOVA_PG_DSN` (`postgres://user@host:port/db`, no password) and
`NOVA_PG_PASSWORD` select it. Plain SQL through pgx, no ORM. `schema.sql` is
embedded in the binary and idempotent (`CREATE ... IF NOT EXISTS`, `ALTER ...
ADD COLUMN IF NOT EXISTS`).

```go
s, err := store.Open(ctx, os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_PG_PASSWORD"))
defer s.Close()
err = s.Migrate(ctx)
```

One PostgreSQL role, `sprint`, owns the schema and is the only writer. The run
loop and the verbs connect as it. Workers, kin and the bus never get the DSN;
they reach the store only through the engine's verbs.

### Migrate

`Migrate` runs in one transaction under an advisory lock (concurrent Migrates
are safe, a second run is a no-op):

| Found | Result |
|---|---|
| no schema `sprint` | creates it from `schema.sql`, records `sprint.meta.hash`, and writes the fresh-store stop (below) |
| `sprint.meta` at this version and hash | no-op |
| `sprint.meta` at an earlier build of this version that the binary lists (`knownOlder`) | runs `schema.sql` over it (adds missing columns, replaces functions and triggers), records the new hash |
| `sprint.meta` at another version | refused: `the store is at schema version N, this binary knows schema version M` |
| same version, unknown hash | refused: `the store's schema hash is X, this binary's is Y: this store was made by a different build of schema 2` |
| schema `sprint` holding any object but no `sprint.meta` | refused as foreign: `schema sprint exists and was not made by nova-sprint; drop it or point NOVA_PG_DSN elsewhere` |

"Holding any object" is every `pg_depend` row on the namespace (relation, type,
function, operator, collation, ...), so a schema with only a publication or
default privileges is foreign too. An empty schema is an empty store.

The hash is the sha256 of `schema.sql` with `--` comments, indentation and blank
lines stripped (a `--` inside a single-quoted literal or double-quoted
identifier is kept), so a comment edit refuses no store. When schema `sprint`
already exists Migrate skips `CREATE SCHEMA`, so the owning role needs no
`CREATE` on the database.

**A fresh store is stopped.** Migrate writes the sprint's stop (`never started`,
`store.FreshStop`) into the epoch row in the transaction that creates the
schema, and only then. Nothing moves until the coordinator runs `nova-sprint
start`. Fenced ops on a fresh store are refused: `the sprint is stopped (never
started): nova-sprint start`.

## Data model

All tables except `meta`, `policy`, `balances` and `cursors` are scoped by
`epoch`; a snapshot shows only the current epoch's rows. Nothing is ever
deleted.

| Table | Key | Holds |
|---|---|---|
| `sprint.meta` | `id = 1` | `schema` version, `hash`, `restored_at`, `restored_digest` |
| `sprint.epoch` | `id = 1` | `n` (current epoch, from 1), the stop (`stopped`, `stop_reason`), the commit identity (`commit_name`, `commit_email`) |
| `sprint.streams` | `(epoch, name)` | `repo`, `base`, `held`, `pos`, `mode`, `forge` |
| `sprint.cards` | `(epoch, id)` | the cards (below) |
| `sprint.rows` | `(epoch, name)` | workers' and kin's sprint state (below) |
| `sprint.judgements` | `(epoch, id)` | questions owed to a person |
| `sprint.events` | `seq` (unique `op`) | the append-only log |
| `sprint.policy` | `version` (serial) | policy versions; global, kept by a clear |
| `sprint.cursors` | `name` | subscribers' places in the log; global |
| `sprint.balances` | `id` | provider balance reads; global |

Ids and names (op id, card id, row/stream/policy names, actor, kind label) are
at most `MaxName` = 128 bytes; brief, reason, body, head, branch and note are
unbounded. All text must be valid UTF-8 with no NUL. Counters are PostgreSQL
`integer` (0..2147483647); `cost` alone is `bigint`. A value outside its column
is `ErrInvalid` from both stores, never `ErrUnknown`.

### Streams

`name`, `repo`, `base`, `held` (no producer of the stream is dealt while true),
`pos` (the order streams land in), `mode` (`auto`, `group`, `hand`; how its
producers land; `auto` when unset), `forge` (the forge it lands through, `""`
for none). Landing settings are stream state, read with the snapshot, never
policy data.

### Cards

One table for all four kinds: `producer`, `sentinel` (both bookkeeping, never
held), and the consumers `work` and `read` (what a row is handed).

| Column | Meaning |
|---|---|
| `id`, `kind`, `of`, `stream`, `tier` | identity. `of` names a consumer's producer (empty on a producer or sentinel). `tier` is one of `flash`, `pro`, `heavy`, `frontier`; a consumer's equals its producer's. |
| `state` | `waiting`, `ready`, `working`, `review`, `merging`, `landed`, `fail`, `blocked`, `done` |
| `gen` | a version: 0 at add, +1 for every op that writes the card (once per op however many writes touch it). Not an attempt. |
| `attempt` | raised only by `SetCardAttempt`; a consumer carries the attempt it belongs to, set at add |
| `score` | the work order; set at add, changed only by `SetCardScore` |
| `needs` | hard dependencies: cannot start until all are `landed`. Producer or sentinel only. |
| `after` | soft: may start any time; orders landing only behind targets that are themselves queued to land. Producer or sentinel only. |
| `holder` | the row a consumer is dealt to; fixed at add; null on a producer or sentinel |
| `author`, `branch`, `base_sha`, `head`, `pr` | the work's git identity: the row whose work the head is, the branch, the base commit the attempt started from, the head commit, the pull request |
| `prior_head` | work consumer only: the head the attempt continues from, `""` on a first attempt |
| `result` | consumer only: `""`, `ok`, `fix`, `fail`, `fault` |
| `bounces` | producer only: the returns that count toward `blocked` (a `fault` never raises it) |
| `review` | producer: its entries into review (`SetCardReview`, +1); read consumer: its producer's value at cut, fixed |
| `finding` | producer: the last read's finding; work consumer: its producer's at cut, fixed |
| `red` | producer only: the last read's verdict was red, so a later waiver can require it |
| `cost`, `priced`, `usage` | consumer only: what its run cost in micro-dollars, whether the route had a price at all (an unpriced run is never shown as $0), and the run's usage record (package `usage`'s words; the store keeps them and reads none) |
| `priority` | `low`, `normal`, `high`, `critical`, `blocker`; producer or sentinel only (a consumer is `normal` and ordered by its producer's level). Set by a person or the coordinator on request, never computed. |
| `brief`, `paths`, `done_when`, `prose` | the card contract: `brief` is the whole brief as given, the others its structured fields. A consumer carries copies set at add. A sentinel has no brief. |
| `reason` | why the card is in its state |
| `model` | consumer only: the model its deal named; fixed at add |

States: `landed` means the code is on the base branch and is final. `fail` is a
card that stopped any other way (cancelled, replaced, its need failed, a consumer
withdrawn), with the reason kept. `done` is a consumer that reported. `blocked`
is a producer that bounced too many times; only the coordinator's unblock leaves
it. A card is *live* in `ready` or `working`. Which transitions are legal and
which kinds use which states is layer 2's; to this layer a state outside the set
is `ErrInvalid`.

**Kind rules held by the schema** (CHECKs and one trigger, `cards_fixed`) **and
again by the Go writes** (so a violation is a logged refusal, never a constraint
error):

- A consumer always has a holder and an `of`; a producer or sentinel never has
  either. A consumer's `of` is a producer of the same epoch, stream and tier.
- `id`, `epoch`, `kind`, `of`, `stream`, `holder`, `tier` never change; neither
  does a read's `review` or a work's `finding`.
- Only producers or sentinels have `needs`, `after`, a non-normal `priority`;
  sentinels have no brief; only a producer has `bounces` and `red`; only a work
  consumer has `prior_head`; `result`, `cost`, `priced`, `usage` belong to
  consumers; `model` to consumers; `review` to producers and reads; `finding`
  to producers and works.
- A card is in exactly one state and has at most one holder: one `NOT NULL`
  state column with a CHECK, one holder column with a foreign key.
- The store holds no width and no tier check. How many `working` consumers a row
  may hold, and which tiers it serves, are configuration (nova-config through
  package `roster`), enforced by the deal guards of layer 2.

### Rows

A row is sprint state only; what a row *is* (kind, width, tiers served, model,
harness) is nova-config's and is never copied here.

| Column | Meaning |
|---|---|
| `name`, `state` | `up` or `down`; a row put for the first time starts `down` |
| `session`, `role` | the bus session and role (`worker`, `coordinator`; the coordinator is also a worker) the hook recorded. Both set or both `""`. |
| `flaps` | count of up/down flaps |
| `held`, `reason` | the coordinator's hold: no deals to the row while true, whatever its state |
| `gen` | +1 per op that writes the row, so a row plan can be fenced (`RowGen`) |
| `load1_centi`, `cores`, `slots_used`, `slots_total`, `tiers` | the worker's last heartbeat: 1-minute load average in hundredths, logical cores (0: never beaten), nova-reserve slots in use and in all, and the tiers its harness proved it serves (comma list in `Tiers` order, `""` none). Telemetry: no store rule reads it and writing it moves no gen. |

There is no heartbeat op for presence: presence is the bus's fact (`nova-bus
who`) and the store records a row's state only when it changes.

### Judgements

A stored record, not a bus message: the push is made from the row and the row
outlives it, so nothing owed is forgotten. `id`, `kind` (a word for the
dashboard: `red`, `fix`, `stuck`, `blocked`, `flap`, `conflict`, ...), `about`
(card, stream or row), `question`, `command` (the exact verb that answers it),
`group_key` (judgements with one key are shown and pushed as one), `state`
(`open`, `answered`, `expired`), `deadline` (nullable), `answer`, `actor`,
`opened_at`, `closed_at`. It has no gen; `JudgementOpen` fences it. A judgement
closes at most once and never at a time before it opened. Judgements belong to
their epoch and are not carried by a clear.

### Cursors

A subscriber's place in the event log (the run loop's push cursor per row, the
dashboard's feed cursor). Global, as `seq` is: a clear keeps it. The store
provides a monotone cursor bounded by the log; that a subscriber never pushes
twice or skips is the subscriber's guarantee, by advancing the cursor only after
a push is confirmed. An absent cursor reads as 0.

### Policy

`version`, `kind` (`script`, `data`), `name`, `body`, `active`, `actor`, `at`.
Global (a clear keeps it); at most one version is active (unique partial index);
a version is immutable but for `active` and is never deleted. Every event
records `policy`, the version active when the op was checked, so a decision can
be traced to the rules that made it.

### Stop and identity

Both are single values for the store, kept on the epoch row and by a clear.

- **Stop** (`Snapshot.Stopped`, `StopReason`): a stopped sprint moves nothing,
  because Run calls no Tick. The store refuses nothing because of it: ops from
  people still land. Ops that must not land while stopped carry `Running`.
- **Identity** (`Snapshot.Identity`, `Identity{Name, Email}`): the git identity
  every commit of the sprint is authored and committed as, because a repository's
  contributor agreement keys off the forge account. Zero value = not set
  (`Identity.Set()` is false).

### Balances

`Balances` (`AddBalance`, `LastBalance`, on `*PG` and `*Fake`) records provider
balance reads for `nova-sprint cost reconcile`: provider, time, the store's seq
at the read, the balance as an exact decimal and the provider's raw answer (no
secret in it). Rows are only added; spend over a window is the fall between two
reads. Global. Not an op and not logged.

## Operations

```go
type Store interface {
    Snapshot(ctx) (Snapshot, error)                       // the whole current epoch, with Seq, Epoch
    Apply(ctx, Op) (Receipt, error)                       // one transaction
    Replay(ctx, opID string) (Receipt, bool, error)       // the receipt an op already got
    Events(ctx, afterSeq int64, max int) ([]Event, error) // afterSeq exclusive, ascending, max capped at MaxEvents (1000); max <= 0 is MaxEvents
    EventIdentity(ctx, seq int64) (op string, epoch int, found bool, err error) // reads no image
    Restored(ctx) (digest string, at time.Time, ok bool, err error)
    Clear(ctx, Op) (Receipt, error)                       // opens a new epoch
    Policy(ctx, version int) (PolicyVersion, error)       // ErrNotFound when absent
}
```

An `Op`:

| Field | |
|---|---|
| `ID` | chosen by the caller, unique forever |
| `Epoch` | the epoch the caller planned against; required (epochs start at 1, so 0 is a caller that forgot) |
| `Actor`, `Kind` | who asked; a label for the log (`add`, `take`, `finish`, ...). Rebuild never reads the label; the writes are the truth. |
| `At` | the time, passed in; the store never reads a clock |
| `Expect` | conditions, all of which must hold |
| `Writes` | a closed set of typed writes, applied in order (a later write sees what an earlier one put) |
| `Note` | kept in the receipt |

The op's epoch must equal the store's, else the op is refused. Every condition
is checked inside the same transaction as the writes, against the rows as they
are at that moment.

### Conditions (`Expect`)

| Condition | Holds when |
|---|---|
| `CardGen{Card, Gen}` | the card is at that gen |
| `CardState{Card, State}` | the card is in that state |
| `CardHolder{Card, Holder}` | the card has that holder (`""` is none) |
| `CardRed{Card, Red}` | the card's red flag is that |
| `NeedsLanded{Card}` | every need of the card, read from the card itself in the transaction, is `landed` |
| `CardAbsent{Card}` | the card does not exist |
| `RowIs{Row, State}` | the row is `up`/`down` |
| `RowGen{Row, Gen}` | the row is at that gen |
| `RowHeld{Row, Held}` | the row's hold is that |
| `StreamHeld{Stream, Held}` | the stream's hold is that |
| `JudgementOpen{ID}` | the judgement is open |
| `Running{}` | the sprint is not stopped |
| `AtSeq{Seq}` | the last event's seq is `Seq` (nothing has entered the store since the snapshot) |

A condition on a missing target is a refusal (`card c9 does not exist`). A
condition naming a state outside its set can never hold, so it is `ErrInvalid`.
There is no width condition and no tier condition.

### Writes

Each write sets one field; two writes to the same field in one op are
`ErrInvalid` (`a second write to card c1 field state`). A set write whose target
does not exist is a refusal. **A set write that would change nothing is
refused** (`row r1 is already up`, `card c1 already has the given reason`), so
nothing moves without a change and no gen is raised.

| Write | Effect and refusals |
|---|---|
| `PutStream{Name, Repo, Base, Pos, Mode, Forge}` | insert or overwrite; `held` kept on overwrite, false on insert; `""` mode is `auto` |
| `SetStreamHeld`, `SetStreamMode`, `SetStreamForge` | one setting each; refused when unchanged (`stream s1's mode is already "hand"`); a mode outside `StreamModes` is `ErrInvalid` |
| `PutCard{ID, Kind, Of, Stream, Tier, ...}` | adds a card in `waiting`, gen 0; refused if it exists or its stream is missing; see *Adding a card* |
| `SetCardState` | refused if the card is already in it. No width or tier check (see Cards). |
| `SetCardBranch`, `SetCardHead`, `SetCardPR`, `SetCardReason`, `SetCardScore`, `SetCardAuthor`, `SetCardBaseSHA` | plain sets, any kind |
| `SetCardResult`, `SetCardCost{Cost, Priced}`, `SetCardUsage` | consumer only (cost and priced are one field) |
| `SetCardPriorHead` | work consumer only |
| `SetCardBounces`, `SetCardFinding`, `SetCardRed` | producer only; `SetCardRed` on a consumer is refused naming the producer to record on, on a sentinel as "never read" |
| `SetCardReview` | producer only; raises it by 1, refused at the integer maximum |
| `SetCardAttempt` | producer or sentinel only (a consumer's is set at add) |
| `SetCardPaths`, `SetCardDoneWhen`, `SetCardProse` | producer or sentinel only (a consumer carries the copy it was added with) |
| `SetCardPriority` | producer or sentinel only, any value including `normal`; `""` is `normal` |
| `SetCardNeeds`, `SetCardAfter` | producer or sentinel only; every name must be another card of the epoch, no duplicates, not itself. Stored sorted in byte order. A cycle through needs is layer 2's refusal. |
| `PutRow{Name, Session, Role}` | the hook's one write: inserts (state `down`) or sets session and role; state, flaps, held, reason and heartbeat are kept. Session and role both set or both `""` (unhook), role in `RowRoles`, else `ErrInvalid`. Hooking a row another session holds is refused: `row w1 is hooked by session sess-a; unhook it first`. The same session may hook again. An advertised `tiers` survives only while the session is unchanged. |
| `SetRowState`, `SetRowFlaps` | plain sets |
| `SetRowHeld{Row, Held, Reason}` | sets the hold and its reason; refused when the hold is already that |
| `SetRowLoad{Row, Load1Centi, Cores, SlotsUsed, SlotsTotal, Tiers}` | the heartbeat; `Cores >= 1`, `SlotsUsed <= SlotsTotal`, `Tiers` a comma list of `Tiers` in order, each once (else `ErrInvalid`); **moves no gen**; refused when unchanged |
| `PutJudgement{ID, Kind, About, Question, Command, Group, Deadline}` | opens a judgement at the op's `At`; refused if the id exists |
| `AnswerJudgement{ID, Answer, Actor}` | answered, closed at `At`; `Actor ""` is the op's actor |
| `ExpireJudgement{ID, Reason}` | expired, reason kept as the answer |
| `SetCursor{Name, Seq}` | inserts or advances; refused at its current value (`cursor c is already at 0`, an absent cursor counting as 0), below it (`cannot move back`), and above the last event (`cursor c cannot pass the log at 42`) |
| `PutPolicy{Kind, Name, Body}` | adds the next version, inactive; actor and time from the op |
| `SetActivePolicy{Version}` | activates a version and deactivates the active one in the same transaction; refused if missing or already active |
| `SetStopped{Stopped, Reason}` | stops or restarts the sprint; refused when already in that state |
| `SetIdentity{Name, Email}` | both non-empty; refused when unchanged |

Closing a judgement (answer or expire) is refused unless it is open, and when the
op's time is before `opened_at` (`judgement J opened at T1, cannot close at T0`).
Opening and closing one judgement in one op is two writes to its state and so
`ErrInvalid`.

**Adding a card** (`PutCard`) is refused, with a reason, when:

- the card exists, or its stream does not;
- it is a sentinel with a brief;
- a producer or sentinel has an `Of`, a `Holder`, an attempt, a finding, a model
  or a review, or a non-normal priority is given to a consumer;
- a consumer has no `Of`, or its `Of` is missing or not a producer, or its
  stream or tier differs from its producer's;
- a work consumer's finding differs from its producer's current finding, or a
  read consumer's review differs from its producer's current review (the copy is
  taken at cut and fixed);
- a consumer has no holder, or the holder row does not exist;
- a consumer has needs or after; or any need/after is itself, repeated, or not a
  card of the epoch.

A consumer is added with its holder and the attempt it belongs to in the same
op, so a card can be added and moved to `ready` in one transaction.

Prior head, bounces, cost and result are never set at add.

## Outcomes of `Apply`

| Result | Meaning | Caller |
|---|---|---|
| `Receipt{Accepted: true}` | epoch matched, every condition held, every write landed; `Seq` is the event, `After` the touched rows | go on |
| `Receipt{Accepted: false, Reason}` | wrong epoch, a failed condition or a write that broke a rule. Nothing changed; the refusal is logged as an event with `accepted = false`. The op id is used. | read the reason, take a new snapshot, ask again with a new id (CLI exit 1) |
| error wrapping `ErrInvalid` | the op never entered the store: no id, no epoch, a condition or write outside the closed sets, a value outside its set or limits, an empty key, two writes to one field. Nothing is logged. | a caller bug: fix it |
| error wrapping `ErrUnknown` | the outcome is unknown: ctx done, connection lost after the request may have reached the server, or any error during or after COMMIT | call `Replay(opID)`: a receipt means it landed; not found means `Apply` again with the same id (CLI exit 2) |

Any other error means the transaction rolled back and the outcome is known: the
request never reached the store, the server returned an error before COMMIT
(whatever its SQLSTATE), or the client failed inside the transaction (a scan, an
encode, an undecodable receipt, a counter at the top of its column). The error
names the cause (for the server the SQLSTATE, constraint, column or table). A
PostgreSQL constraint violation during the writes is an engine defect, not a
refusal. Such errors are never `ErrUnknown`: reporting a known failure as
unknown sends a caller into a replay loop that never ends.

Typical caller:

```go
snap, _ := s.Snapshot(ctx) // the caller's own copy; editing it never changes the store
u := snap.Cards["u1"]
op := store.Op{
    ID: "take-u1-7", Epoch: snap.Epoch, Actor: "w1", Kind: "take", At: now,
    Expect: []store.Expect{store.CardGen{Card: "u1", Gen: u.Gen}, store.RowHeld{Row: "w1", Held: false}},
    Writes: []store.Write{store.SetCardState{Card: "u1", State: "working"}},
}
r, err := s.Apply(ctx, op)
switch {
case errors.Is(err, store.ErrInvalid): // fix the op; nothing was logged
case errors.Is(err, store.ErrUnknown): // r, found, _ := s.Replay(ctx, op.ID); if !found, Apply the same op again
case err != nil:                       // nothing happened
case !r.Accepted:                      // r.Reason; re-plan with a new id
default:                               // r.Seq is the event, r.After the touched rows
}
```

## Log, epochs and replay

**Events.** `sprint.events` is append-only (a trigger refuses UPDATE, DELETE and
TRUNCATE): one row per operation that entered the store, accepted or refused.
`seq` is commit order with no gaps (writers run one at a time), so a reader's
cursor is a committed prefix and `Events(afterSeq)` never skips an event. `before`
and `after` hold the full row of each touched stream, card, row, judgement,
cursor or policy version (and the stop and identity when written). Folding the
accepted events in `seq` order from the first rebuilds the snapshot: `Rebuild`,
`Fold`, run's copy in layer 6 and the trace checks all call the one `Fold`. A
refused event has no images. The receipt is stored without its after-image;
`Replay`, a repeated `Apply` and `Events` reconstruct `Receipt.After` from the
event's `after`. `Events` crosses epochs (each event carries its epoch, a clear
included). The events are layer 4's outbox: a deal is an accepted event and the
bus push reads from a cursor.

`Rebuild` starts from the fresh store: epoch 1, empty, stopped with reason
`never started`.

**Replay.** `Replay(opID)` returns `(receipt, found, err)`: the stored receipt of
an op that was accepted or refused, changing nothing; `found` is false when no
event has that id (an id no op could have is also not found). A second `Apply`
with an id already in `events` returns the stored receipt whatever its body says;
a changed body is never a new operation. The receipt carries the original op's
id, kind, actor, epoch, `at` and note. Two concurrent applies of one id are
serialized by the lock; the second sees the first's event.

**Epochs.** `Clear` is an `Op` of kind `clear` with no writes and at most an
`AtSeq` condition. It is idempotent by id, logged with the epoch it closed, and
its receipt's `NewEpoch` is the one it opened. The op's epoch is checked, so a
retried clear with a new id after the first landed is refused, not applied twice.
A clear:

- empties the snapshot's streams, cards and judgements (they stay readable
  through `Events`);
- carries every row into the new epoch in the same transaction, `down`, flaps 0,
  gen +1 (session, role, hold and heartbeat kept), because rows are the fleet and
  a clear must not make every worker register again; the clear's after-image
  carries these rows;
- keeps the policy, cursors, stop and identity.

**Counters.** A gen, the epoch or a policy version that would pass 2147483647 is
a plain error naming the counter, in both stores.

## Guarantees

1. One operation is one transaction: whole, with its event row, or nothing.
2. Idempotent by op id: the same id never applies twice.
3. A transport failure is reported as unknown, never as unchanged.
4. A card is in exactly one state and has at most one holder, by the schema.
5. The log matches the state: folding every accepted event from the first rebuilds
   the snapshot, policy included. Nothing is deleted; an old epoch stays readable.
6. A snapshot is one consistent read (one `REPEATABLE READ` transaction) of the
   current epoch and carries the `Seq` and `Epoch` it was read at, so a plan made
   from it can fence on either.
7. A condition is checked against the rows as they are when the writes land.

**How:** `Apply` and `Clear` begin by taking one lock (`LOCK TABLE sprint.epoch IN
SHARE ROW EXCLUSIVE MODE`) held to commit, at `READ COMMITTED`. A sprint sees a
few operations a second, so one writer at a time is the simplest thing that makes
every condition exact and every seq commit order; there are no serialization
failures and no retries. The lock is what lets the model's atomic step stand for
one SQL transaction. Nothing that waits on the outside world runs while it is
held (no network, git, bus send, or read of a whole history): a caller does that
first and passes the result in.

Refusals that depend on an order of names (for example the first offending card)
use byte order in both stores (`COLLATE "C"` in PostgreSQL), so reasons are equal
on any database collation.

## Schema identity, restore and history validation

**`CheckSchema(ctx, dsn) (SchemaIdentity, error)`** (and `PG.Schema`, `Fake.Schema`)
says what a database holds without changing it, for status or doctor commands.
It runs in one `READ ONLY` transaction it rolls back, so a role with only `USAGE`
on `sprint` and `SELECT` on `sprint.meta` can run it, and it uses Migrate's own
checks, so it is clean exactly when Migrate would take the store. The password
comes as pgx takes it (`PGPASSWORD`, a pgpass file), not `NOVA_PG_PASSWORD`.
`SchemaIdentity` is `Present`, `Hash` (what `sprint.meta` records), `Expected`
(this binary's), `Current`, `KnownOlder` (an earlier build Migrate upgrades),
`Owner` and `Round` (an integer naming the schema build). Errors name the fix:

| Found | Error says |
|---|---|
| database missing | create it, or point `NOVA_PG_DSN` at the store's database |
| no schema `sprint`, or an empty one | run Migrate |
| foreign schema | Migrate's refusal |
| `sprint.meta` unreadable | grant this role `USAGE` on schema sprint and `SELECT` on `sprint.meta`, or check as the owner |
| no meta row | recreate the store |
| another version or hash | Migrate's words |

**Restore** of a `pg_dump --data-only --schema=sprint` dump, on one connection
in one transaction the caller owns (`pgx.Tx`):

1. `RestoreBegin(ctx, tx, digest)`: `digest` is the sha256 of the dump's bytes in
   lowercase hex, computed by the caller (anything else is refused). Takes
   Migrate's advisory lock, refuses a foreign schema and a schema whose tables
   hold any row (`sprint.cards has rows; a restore loads only into an empty
   store`), creates the schema if absent, runs `schema.sql`'s DDL without its two
   seed rows (the dump carries `meta` and `epoch`), and disables `cards_fixed`.
   The trigger is off because `pg_dump` copies cards in physical order, where an
   updated producer can follow its consumers, and the trigger checks each
   consumer's producer row by row.
2. The caller loads the dump (each statement as psql would, each COPY by
   `CopyFrom`).
3. `RestoreEnd(ctx, tx)`: re-enables `cards_fixed` and runs it over every card;
   requires `sprint.meta` at this binary's version and hash, and the epoch row;
   runs `ValidateHistory`; records `restored_at` and `restored_digest` in
   `sprint.meta`; unhooks every hooked row of the current epoch in one ordinary
   logged op (`restore-unhook-<digest>-<seq>`, actor `store-restore`, kind
   `unhook`, `PutRow{Name}` per row) so no hook from the dump is live; refuses if
   a row is still hooked; and returns the restored store's `SchemaIdentity`.
   Errors are prefixed `the dump does not hold together:` for history failures.

On any error the caller rolls back and nothing of the restore remains; commit
only when `RestoreEnd` returns none. `Restored(ctx)` returns the digest and time,
so a retried restore can tell the data is already in (the Fake is never
restored).

**`ValidateHistory(ctx, tx)`** checks that the log is the whole history of the
tables and names the first thing that is not, in this order: no gap in seq from 1
to the last; each event's stored receipt is its own; each event is in the epoch
the log is in at that point and the epoch row is the log's last; the stored
policy versions are exactly those the log wrote, each equal to its last image,
so the active one is the log's last activation; every cursor is at or below the
last seq; and `Rebuild` over the events equals the snapshot. It only reads, and
a verb can run it on a live store in a `REPEATABLE READ` transaction. Messages are
prefixed `store: history:`.

## Fakes

`store.NewFake()` is an in-memory `Store` for unit tests of the layers above. It
runs the same protocol, conditions, writes and refusal reasons as `PG` (shared
code in `apply.go`, `check.go`, `writes.go`, `ops.go`), and the conformance suite
runs the same scenarios against both. Time comes from `Op.At`. It clones every
table and the log on every op, so it is for tests only. It starts stopped, as a
fresh store does. `Fake.Drop(opID, landed)` makes the next `Apply` or `Clear` of
that id report `ErrUnknown`, landed or not, so a caller can test its replay path.

`store.NewFakeFrom(snapshot, policies...) (*Fake, error)` is a Fake whose
streams, cards, rows, judgements, cursors, epoch, seq, stop and identity are the
snapshot's, for dry-running a plan built from a snapshot with PG's checks. Its
`Snapshot` equals the input and its next event is `Seq+1`; the earlier log is
not carried (`Events` starts after it, `Replay` of an earlier id is not found).
Pass every policy version the store holds: a snapshot names only the active
one. An input that is not a snapshot (epoch below 1, negative seq, a map key that
is not its entry's name, a policy version below 1 or repeated, an active version
not given) is `ErrInvalid`.

## Scale

`Snapshot` is the whole epoch: the cold-start read. In steady state the run loop
and the dashboard keep a local copy and advance it from `Events(afterSeq)` with a
saved cursor, the same fold `Rebuild` runs, so a tick reads only what changed
whatever the sprint's size. A snapshot per tick is a defect in layer 3. Indexes
beyond the primary keys each serve one store query that would otherwise scan the
epoch's cards: `cards_working (epoch, holder) WHERE state = 'working'` for
per-row working counts, `events.op` unique for replay, `balances_provider_at`.
Every other read is by a primary key or a prefix of one. A reader that needs
another index names it in its own layer.

`Landings(ctx, epoch, upToSeq)` (on `*PG` and `*Fake`) returns the time of every
producer landing in an epoch up to a seq, oldest first, for the dashboard's rate;
`LandedIn(event)` counts the producers one event moved to landed. A producer an
import carried over already landed (reason starting `store.ImportedReason`) is not
a landing of the sprint.

## Testing

| Tier | Where | What |
|---|---|---|
| Unit | package `store`, against `Fake` | no real time, sockets or network; time passed in |
| Conformance | `RunConformance` in `conformance_test.go` | the same scenarios against `Fake`, `PG` and `PG` on an ICU-collation database, so the stores cannot drift |
| Functional | `functional_test.go` and other `TestFunctional*` | every op and refusal against a real throwaway PostgreSQL: `initdb` into `t.TempDir()`, free port, `fsync=off`, one server per test, all `t.Parallel()` |
| Model | `tla/Store.tla` | the store's atomic steps; see below |
| Traces | `trace_tla_test.go`, `tla/StoreTrace.tla` | the code's random walk is checked to be a behaviour of the model |

```
go test -count=1 -skip Functional ./store/...          # unit tier
go test -count=1 -parallel 4 -run Functional ./store/  # functional tier (needs initdb)
bash tla/ci-tlc.sh                                      # all models, witnesses, reach checks (a bench host)
```

Functional tests skip with a message when `initdb` is not on `PATH`, in
`/opt/homebrew/opt/postgresql@18/bin` or in `/usr/lib/postgresql/*/bin`. Each
server takes System V shared memory; on macOS's default limits run with
`-parallel 4`. The servers run with `LC_ALL=C` (PostgreSQL on macOS refuses to
start under a shell with no valid locale). They cover the unknown-then-replay
path (a connection killed between request and commit), two concurrent applies on
one expectation (one accepted, one refused), rebuild from the log equals the
snapshot, migration from each known older schema build
(`store/testdata/schema-*.sql`), that `CheckSchema` writes nothing, and the
restore and history checks with one mutation of a dump per check. `PG.fault` is
the fault-injection seam they use to lose a reply after COMMIT or to panic with
the writer lock held; it is nil in the binary. Tests that corrupt an event turn
the log's append-only trigger off for one statement (`unguardLog`).

### The model

`tla/Store.tla` (cfgs `Store.cfg`, `StoreRecords.cfg`) models operations as atomic
steps over cards, rows, events and applied op ids. Invariants: single state and
holder, producers never held, consumers well-formed, idempotent replay, the
applied set equals the log, the log matches the state, ops land only in their
epoch, conditions are checked against the same state the writes change, a
refusal changes nothing, no spurious refusal, one gen per op, a clear carries
the rows, a judgement closes once, cursors are monotone and within the log, the
log is append-only, policy versions are immutable, receipts match their events,
red only on producers, a hook is not stolen, a fresh store moves nothing until
started.

Each rule has a reversed witness in `tla/witnesses/Store*.tla`, a variant that
TLC must fail: a write that lands without its event row (`StoreNoEvent`), a
replay that applies again (`StoreReplayApplies`), a condition checked against a
stale snapshot (`StoreStaleCheck`, `StoreRowStale`), a second holder
(`StoreProducerHolds`, `StoreSentinelHolds`), an op without the epoch check
(`StoreEpochUnfenced`), a cursor that moves back or passes the log, a judgement
closed twice, a rewritten policy, a tampered log, red on a consumer, a stolen
hook, a dropped stop, a store-side width cap that breaks `NoSpuriousRefusal`
(`StoreWidthInStore`: width belongs to the roster), and a fresh store running.
Reach checks in `tla/reach/` are required violations that show the passing run
really reaches a take, a refusal, a recovery after a transport failure, a
stale-epoch refusal and the other states the invariants range over.

The model abstracts away: streams, the `AtSeq` condition and policy activation,
every card column no rule reads (score, branch, head, pr, brief, paths, reason,
result, cost, usage, ...), the heartbeat, flaps, the stop's reason, judgement
fields other than state. `TraceOp` (`trace.go`) refuses to project an op that
writes any of them. The store's state is projected onto the model's variables by
`TraceState`/`TraceOp`, the one place the code's state is read in the model's
terms.

## Source map

| File | |
|---|---|
| `store.go` | types, closed sets, `Fold`/`Rebuild` |
| `ops.go` | every condition and write, and the `specOf` table (one entry per write: fields, body check, kind rule, change) |
| `check.go` | body validation (`ErrInvalid`), limits |
| `apply.go` | the shared protocol, conditions, put helpers |
| `writes.go` | the write implementations and their refusal reasons |
| `pg.go`, `pgread.go` | `PG`: Open, Migrate, Apply, snapshot and event reads |
| `fake.go`, `fakefrom.go` | the in-memory store |
| `schema.go`, `schema.sql` | schema identity and the embedded schema |
| `restore.go`, `history.go` | restore and history validation |
| `landings.go`, `balances.go` | landing times; provider balances |
| `trace.go` | projection onto the TLA+ model |
