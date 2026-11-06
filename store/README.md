# store

Layer 1 of nova-sprint v2: the one place the sprint's state lives. The
contract is [docs/LAYER-1-STORE.md](../docs/LAYER-1-STORE.md); this is what the
package does with it.

- `PG` is the store in PostgreSQL, schema `sprint` (`schema.sql`, embedded,
  applied by `Migrate`). Open it with `Open(ctx, $NOVA_PG_DSN, $NOVA_PG_PASSWORD)`.
  `Migrate` refuses a store at a schema version other than `SchemaVersion`,
  naming both numbers and the fix, the store's own version first: a store at
  version 1 opened by this binary is refused with "the store is at schema
  version 1, this binary knows schema version 2: run a binary built for
  schema version 1, or recreate the store". It is safe to run concurrently
  (it holds one advisory lock). A schema `sprint` that holds any object
  at all (anything PostgreSQL records as depending on the namespace: a
  relation, type, function, operator, collation, text search object,
  operator family, and also a publication or default privileges that name the schema, so such an "empty" schema is refused too, ...) but no `sprint.meta` is refused: "schema sprint exists and was not made by
  nova-sprint; drop it or point NOVA_PG_DSN elsewhere" (an empty one is an
  empty store). It also checks the shape: `sprint.meta` keeps the sha256 of
  the schema text that made the store, with comments, indentation and blank
  lines stripped (so a comment edit refuses nothing; a `--` is a comment only
  outside a single-quoted string literal, so one inside a literal is hashed),
  and a store at version
  2 whose hash differs from this binary's (or has none) is refused naming
  both hashes:
  "this store was made by a different build of schema 2; recreate it (no live
  store is at v2 yet)". The binary knows schema version 2: version 1 plus the two card kinds
  (`kind` with `sentinel`, `of`, `after`, `result`, the states `blocked` and
  `done`), the card columns `author`, `base_sha`, `prior_head`, `bounces`,
  `cost`, `priced` and `priority` and the result `fault` (layer 2's and the
  lessons of v1), the card columns `tier`, `paths`, `done_when` and `prose`, row `gen`, `held`, `tiers`, `model` and `harness`, the tables `judgements` and `cursors`, and the events' `policy`. There is no upgrade path: a store at
  version 1 is refused like any other version.
- `Fake` (`NewFake()`) is the same store in memory, for unit tests of the
  layers above. It runs the same rules and the same protocol (`apply.go`,
  `check.go`, `writes.go`), so its conditions, writes, refusal reasons and
  receipts are PG's. Where a reason depends on an order of names (the
  consumer a tier drop would strand is the lowest id), both use byte order:
  Fake compares bytes and PG orders `COLLATE "C"`, so the reasons are equal on
  any database collation (an ICU en-US database runs the whole conformance
  suite). Time comes from `Op.At`; it never reads a clock. It
  clones every table and the log on every op (O(n) per op, O(n^2) over a
  long log): it is for tests only.
  `Fake.Drop(opID, landed)` makes the next Apply or Clear of that id report
  `ErrUnknown`, landed or not, so callers can test their replay path.

## A worked example

```go
s, err := store.Open(ctx, os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_PG_PASSWORD"))
if err != nil { return err }
defer s.Close()
if err := s.Migrate(ctx); err != nil { return err } // refuses another schema version

snap, err := s.Snapshot(ctx) // the caller's own copy
if err != nil { return err }
u := snap.Cards["u1"] // a work consumer of producer c1, ready and held by row w1
op := store.Op{
    ID: "take-u1-7", Epoch: snap.Epoch, Actor: "w1", Kind: "take", At: now,
    Expect: []store.Expect{store.CardGen{Card: "u1", Gen: u.Gen}, store.RowHeld{Row: "w1", Held: false}},
    Writes: []store.Write{store.SetCardState{Card: "u1", State: "working"}}, // refused when w1 is at width
}
r, err := s.Apply(ctx, op)
switch {
case errors.Is(err, store.ErrInvalid): // a caller bug: fix the op; nothing was logged
case errors.Is(err, store.ErrUnknown): // the server did not answer: r, found, err := s.Replay(ctx, op.ID); if !found, Apply the same op again
case err != nil: // did not reach the store, or the server refused it before COMMIT (SQLSTATE named): nothing happened
case !r.Accepted: // refused: read r.Reason, take a new snapshot, plan again with a new id
default: // accepted: r.Seq is the event, r.After the touched rows
}
```

## What it guarantees

1. One operation is one transaction: its conditions, its writes and its event
   row land together, or nothing does.
2. An op id applies at most once. A second Apply with a known id returns the
   receipt the first one got, whatever the second one asks for, an invalid
   body included: only a missing id is refused before the lookup.
3. The unknown rule: Apply returns `ErrUnknown` only when ctx is done, when
   the connection was lost after a request may have reached the server (a
   FATAL or PANIC from the server, or a connection exception, class 08,
   included; `pgconn.SafeToRetry` says when nothing was sent, and that is
   known), or on any error during or after COMMIT. It never says nothing
   changed then. Every other error inside the transaction, before COMMIT, is
   known and comes back as a plain error, because the transaction rolls back
   and nothing landed: any error the server returned (named with its
   SQLSTATE and message), and any the client raised (a scan into a column
   whose type changed, an encode, a stored receipt that does not decode, a
   gen, epoch or policy version at the top of its column, named). An invalid
   body is known too (`ErrInvalid`).
4. A card is in exactly one state and has at most one holder. The schema holds
   this (one `NOT NULL` `state` column with a CHECK, one `holder` column with
   a foreign key to a row of the same epoch), not Go. The schema also holds
   the kinds: a producer or sentinel has no `of` and no holder, a consumer always has one, a sentinel
   no brief, a consumer has an `of` naming a producer of the same epoch (a
   foreign key and the `cards_fixed` trigger) in its own stream and of its
   own tier, and no needs, after or priority of its own; the trigger also
   refuses any UPDATE that changes a card's `id`, `epoch`, `kind`, `of`,
   `stream`, `holder` or `tier`, and a second trigger refuses one that
   changes a stream's, row's or judgement's key or epoch. The log is
   append-only (a row trigger refuses UPDATE and DELETE on `sprint.events`, a
   statement trigger TRUNCATE), a policy version is immutable once added (a
   trigger lets only `active` change and refuses DELETE and TRUNCATE), every
   count (pos, width, flaps, gen, attempt, score, pr, bounces, cost, a
   cursor's seq) is 0 or more by a CHECK, and a judgement closes no earlier
   than it opened. A consumer's attempt is fixed at add by the
   store alone (the schema cannot tell the store's writes from a caller's). `of` is the
   one column besides `holder` that is NULL for "none" (the foreign key needs
   it); the Go side reads both as `""`.
5. The log matches the state: folding every accepted event from the first
   (`Rebuild`) gives the snapshot, policy included. Rebuild needs every event,
   so page `Events` past `MaxEvents` to the end. Nothing is deleted; a clear
   opens a new epoch and the old one stays readable in the log.
6. A snapshot is one consistent read (one REPEATABLE READ transaction) of the
   current epoch, with the seq it was read at and the epoch. It is the
   caller's own copy: editing it, a receipt or an event never changes the
   store.
7. Every condition is checked against the rows as they are when the writes
   land: Apply and Clear take one lock (`LOCK TABLE sprint.epoch`) at the
   start and hold it to commit, at READ COMMITTED. Writers run one at a time,
   so there is no stale check, no lost update and no serialization retry, and
   event seqs are commit order with no gaps: `Events(afterSeq)` never skips one.
   A panic inside a write rolls its transaction back, so the lock is released.

## An op

`Op` has an `ID` (unique forever), an `Epoch` (the epoch the caller planned
against; required, epochs start at 1), `Actor`, `Kind` (a label for the log),
`At` (the caller's time), `Expect`, `Writes` and `Note`.

Conditions: `CardGen`, `CardState`, `CardHolder` (`""` is none), `NeedsLanded`
(every need of the card, read from the card inside the transaction, is
landed), `RowIs`, `RowGen`, `RowHeld`, `StreamHeld`, `CardAbsent`,
`JudgementOpen`, `AtSeq` (the last event's seq is N: nothing at all entered
the store since a snapshot at N, a refused op included, since a refusal is an
event and moves seq too). One rule for a value outside its closed set
(card kind, state, result, priority and tier; row kind, state and tiers,
none twice; policy kind), for an empty key (a card, judgement id; a
stream, row, cursor name) and for every name an op points at (a card's
stream, each of its needs and after), and for an integer outside its range
(a `CardGen` or `RowGen` gen, an `AtSeq` seq below 0), in a condition
exactly as in a write: it is `ErrInvalid` ("card state \"flying\" is not one
of ...", "a card needs an id"), never a refusal, and nothing is logged, so
no reason has a gap where a name should be. `CardHolder`'s holder `""`, a
card's `of` and holder `""`, and an answer's actor `""` are none, not an
empty key (a consumer with no of or no holder is refused, by name). There is no width condition and no tier condition:
the writes are the checks (the state write into working for width; put
card, the state write into ready or working, and the row writes for
tiers).

Cards are of two kinds. A `producer` is the work itself (it has needs, a
branch, a head, a PR, a bounce count and a priority, and lands); it never has
a holder. A `sentinel` is a producer with no brief and no consumers (a
release gate or a phase mark: its needs are what it waits for). A consumer
(`work` or `read`) is one attempt at, or one read of, a producer: its `of`
names that producer (never a sentinel), it has no needs, no after and no
priority of its own (its column stays `normal`; it is ordered by its
producer's), it names its holder row at add, and that holder never changes
(there is no holder write: Johnny's review, 2026-11-06, found a working card
could drop its holder and keep the slot), and it records a `result` (`""`, `ok`,
`fix`, `fail`, `fault`), its `cost` in micro-dollars with `priced` (false
when its route has no price, never shown as $0), and on a work consumer the
`prior_head` it continues from, and the `attempt` of its producer it belongs
to, given at add (`PutCard.Attempt`) and never written after. Kind, of,
stream, holder and tier are set by `PutCard` and never change (in Go, and by
the `cards_fixed` trigger). `needs` is hard order (cannot start until they landed), `after` soft
(orders the landing only); layers 2 and 3 read them, the store only checks
that they name other cards of the epoch, once each. A priority is one of
`low`, `normal` (the default), `high`, `critical`, `blocker`; the `read` rung
of the deal order is layer 2's, never a level. The store enforces whose each column
is, in Go and by a CHECK: prior head a work consumer's, bounces a producer's, cost,
priced and result a consumer's, priority, needs and after a producer's or sentinel's, a holder a consumer's,
and SetCardAttempt is refused on a consumer (its attempt is set at add by PutCard). The check is on the kind, before
the value is looked at: a kind-specific write on the wrong kind is refused whatever its value, no-op values included
(`SetCardPriority` normal, `SetCardNeeds` or `SetCardAfter` nil, `SetCardPaths` nil, `SetCardBounces` 0), so it
never raises a gen or logs an accepted event. At add a consumer may name only the default priority (`""` or normal).

Tier. Every card names its tier at add, one of `flash`, `pro`, `heavy`,
`frontier` (a CHECK in the schema; in Go a tier outside the four is a
refusal naming the set); a consumer's must equal its producer's ("card k1
is tier pro, its producer c1 is tier heavy; a consumer takes its producer's
tier"), and no write changes it. A row serves a set of tiers, `tiers`
(each one of the four, once; a CHECK `tiers <@ '{flash,pro,heavy,frontier}'`,
no function), with `model` and `harness` as labels for policy and the
dashboard; `PutRow` takes them, `SetRowTiers` and `SetRowLabels` (model and
harness, one field) change them and raise the row's gen. The writes keep
every live consumer on a row that serves its tier, read inside the
transaction: `PutCard` refuses a consumer whose holder row does not serve
its tier ("row rflash serves flash; consumer needs heavy"), and so does a
`SetCardState` that moves a consumer into ready or working; `SetRowTiers`,
and `PutRow` on overwrite, refuse to drop a tier a live (ready or working)
consumer held by the row needs ("row r1 holds consumer u3 (heavy); cannot
drop heavy"), as narrowing is refused below the working cards. A waiting
consumer holds no tier, so its row may drop it; it is checked again when it
moves. Beside `brief`, which keeps the whole brief as given, a card
carries the contract's structured fields: `paths`, `done_when` and `prose`;
`PutCard` takes them, `SetCardPaths`, `SetCardDoneWhen` and `SetCardProse`
change a producer's or sentinel's, and a consumer carries the copies it was
added with.

Judgements (`sprint.judgements`). A question owed to a person, stored so it
outlives any push: id, kind (a word for the dashboard), about (the card,
stream or row), question, command (the verb that answers it), group key
(shown and pushed as one), state `open`, `answered` or `expired`, deadline
(zero is none), answer, actor, opened and closed times. `PutJudgement`
opens one (refused when it exists; opened at the op's `At`);
`AnswerJudgement{ID, Answer, Actor}` and `ExpireJudgement{ID, Reason}` close
an open one at the op's `At` (the reason is kept as the answer; an empty
actor is the op's), refused unless it is open, and when the op's `At` is
before it opened ("judgement j1 opened at T1, cannot close at T0"). A judgement has no gen:
fence a plan on `JudgementOpen`. It belongs to the epoch, so a clear drops
it; the images and `Rebuild` carry it, and `Snapshot.Judgements` lists the
epoch's.

Cursors (`sprint.cursors`). A subscriber's place in the event log (a row's
push cursor, the dashboard's feed), so a restart resumes where it stopped:
`SetCursor{Name, Seq}` (the last seq it read) inserts it when absent (an
absent cursor reads as 0) and is refused below the stored seq ("cursor
push-w1 is at 42, cannot move back to 7") and above the last event's seq
before the op ("cursor push-w1 cannot pass the log at 42"). Seq is global,
so cursors are too: a clear carries them, and after a clear a subscriber
resumes from its cursor, reading the clear's event and the new epoch's
after it. The images and `Rebuild` carry them, and `Snapshot.Cursors` maps
each name to its seq. Every event also records
`policy`, the active policy version when its op was checked (0 when none);
`Rebuild` ignores it.

The card states are `waiting`, `ready`, `working`, `review`, `merging`,
`landed`, `fail`, `blocked`, `done`. Width caps work in progress, not what is
queued: a row's width counts only the cards it holds in `working`, so a held
card that is ready, waiting, done or anything else takes no room. Which kind
may use which state is layer 2's; to the store only a state outside the set
is wrong (`ErrInvalid`). Rows are `up` or `down`; the coordinator's hold is `held`, apart from
the state.

Writes, one field each, applied in order: `PutStream` (repo, base, pos;
insert or overwrite), `SetStreamHeld`, `PutCard` (kind, of, stream, score,
needs, after, brief, holder, priority, tier, paths, done when, prose, and a
consumer's attempt; the card must not exist; it starts
waiting, gen 0, attempt 0, result `""`; a producer or sentinel is refused an
`of` and a holder, a sentinel a brief, a consumer needs, after, a priority
other than normal, a missing holder, a stream or tier other than its
producer's and an `of` that is missing or is not a producer; the
holder row must exist and serve its tier), `SetCardState` (a consumer moving into ready or working is refused when its row does not serve its tier; into `working` also
when its row already holds as many working cards as its width),
`SetCardPaths`, `SetCardDoneWhen`, `SetCardProse`, `SetCardAttempt`, `SetCardBranch`, `SetCardHead`, `SetCardPR`,
`SetCardReason`, `SetCardScore`, `SetCardAuthor` (the row whose work the head
is), `SetCardBaseSHA` (the base commit the attempt started from),
`SetCardPriorHead`, `SetCardResult`, `SetCardBounces`, `SetCardCost` (cost
and priced together, one field), `SetCardPriority` (refused on a consumer,
normal included; `""` is normal, as at put card), `SetCardNeeds` and `SetCardAfter` (each refused on a
consumer, nil included), `PutRow`
(kind, width, session, tiers, model, harness; insert or overwrite; a new row is down and not held;
state, flaps and held are kept on overwrite; an overwrite may not narrow below the working cards or drop a tier a live consumer needs), `SetRowWidth` (the width
alone), `SetRowHeld` (the coordinator's hold: no deals to the row while
true, whatever its state), `SetRowState`, `SetRowSession`, `SetRowFlaps`,
`SetRowTiers` (refused when it drops a tier a live consumer of the row needs), `SetRowLabels`, `PutPolicy`, `SetActivePolicy`,
`PutJudgement`, `AnswerJudgement`, `ExpireJudgement`, `SetCursor`. `PutPolicy` takes no version: the store
gives it the next one, and the caller learns it from the receipt,
`r.After.Policies` (keyed by version; the new version is the one not in
the snapshot it planned from). The working count is taken at the moment of
the write and excludes the card written, so two takes onto a width-one row in
one op are refused on the second and nothing lands. Every set write that would
change nothing is refused: a card to the state it is in ("card u1 is
already working"), a row likewise ("row r1 is already up"), a stream or row
held to what it is ("stream s1 is already not held"), the active policy
version activated ("policy version 1 is already active"), a cursor set to
its seq ("cursor feed is already at 7"), and every other set-card and
set-row write to the value it holds ("card c1 already has the given
reason", "row w1 already has the given tiers"; an empty list and none are
one value). Tiers, needs and after are sets: both stores keep them sorted
in byte order without repeats, so a reorder is the same value and is
refused like any other; a name twice in what a write sends is still
refused (needs, after) or invalid (tiers). Paths and done when are lists:
their order is kept.
The kind rule comes first. Nothing moves without a change: no gen rises,
and the refusal event carries no image. Put writes are not set writes: a
`PutStream` or `PutRow` overwrite with the same values is accepted. A
width below the working cards the
row holds is refused; width 0 is a row that takes nothing into working (it
may still hold queued cards). An op that writes a card or a row raises its
gen by one, however many of its writes touch it (gen counts the operations
that changed it; a card or row the op adds is at gen 0), so `CardGen` and
`RowGen` fence a plan against any change since its snapshot.

The value limits, each `ErrInvalid` from both stores, naming the field:

- every integer a write carries (pos, score, attempt, pr, bounces, width,
  flaps, policy version) is in 0..2147483647 (a PostgreSQL `integer`,
  nothing meaningful below 0); cost is in 0..9223372036854775807 (a
  `bigint`); the op's epoch is in 1..2147483647;
- every id and name is at most 128 bytes (`MaxName`): the op id, card id,
  row name, stream name, actor, the op's kind label and policy name, and
  every field that names one of them (of, holder, author, needs, after, and
  the card, row and stream a condition or write names). Brief, reason, body,
  head, prior head, branch, base sha and note are unbounded. `Replay` of an
  id no op can have (empty, too long, holding a NUL) is not found;
- every text (ids, names, actor, kind, note, brief, reason, head, prior head,
  branch, author, base sha, needs, after, priority, policy name and body) is
  valid UTF-8 with no NUL byte;
- `At` is set and falls in the years 0..9999.

A refusal reason, or an `ErrInvalid` message, quotes at most `MaxName`
bytes of a value the caller sent, then `...`. A gen, the epoch or a policy
version at 2147483647 cannot rise: the op fails with a plain error naming it
("store: card c1 gen is 2147483647, the most its column holds; it cannot
rise"), nothing lands, in both stores.

`Clear` takes an op of kind `clear` with no writes and at most an `AtSeq`
condition. It opens epoch n + 1 and carries every row into it (same name,
kind, width, session, held, tiers, model and harness; state down, no flaps,
gen raised by one);
streams, cards and judgements stay behind in the old epoch, policy and
cursors are kept. The event carries the epoch it closed; the receipt's
`NewEpoch` is the one it opened. Like Apply, Clear with an op id already
used returns that op's stored receipt and does nothing: an earlier Apply's
receipt (`NewEpoch` 0) when the id was an Apply's.

## The four outcomes of Apply (and Clear)

| Result | What happened | What the caller does |
|---|---|---|
| `Receipt{Accepted: true}` | The epoch matched, every condition held, every write landed; `Seq` is the event, `After` the touched rows. | Go on. |
| `Receipt{Accepted: false, Reason}` | The epoch was wrong, a condition failed, or a write broke a rule (target missing, card exists, row full, a write that changes nothing). Nothing changed; the refusal is an event (`accepted = false`). | Read the reason; take a new snapshot; ask again with a new op id. Exit 1 at the CLI. |
| an error wrapping `ErrInvalid` | The op never entered the store: no id, no epoch, a condition or write outside the closed sets, a value outside its closed set or an empty key, two writes to one field, a value outside the limits. Nothing is logged. | Fix the caller: it is a bug. |
| an error wrapping `ErrUnknown` | The server did not answer: the connection failed, ctx timed out or was cancelled once the request was sent, or an error came during or after COMMIT. Nothing is known. | `Replay(opID)`: if it comes back, that is the receipt; if not, `Apply` again with the same id. Exit 2 at the CLI. |

Any other error means nothing happened, and `Replay` is not needed: the
request did not reach the store, the server returned an error before
COMMIT, whatever its SQLSTATE (a CHECK the rules do not know, a value too
large for an index, a column that is gone, a lock timeout, ...), or the
client failed inside the transaction (a scan, an encode, a receipt that does
not decode, a counter at the top of its column). The transaction is rolled
back, nothing is logged, and the error names the cause: for the server, the
SQLSTATE, the constraint, column or table, and its message.

`Events(afterSeq, max)` is exclusive of `afterSeq`, ascending, and returns at
most `MaxEvents` (1000) events; a larger `max`, or `max <= 0`, is capped
silently. An image that does not decode is an error naming its seq, never a
panic. `Policy(version)` returns one policy version, or `ErrNotFound`.

## Tests

    go test -count=1 -race -parallel 4 ./store/                   # unit tests (Fake) and functional tests (PG)
    go test -count=1 -race -parallel 4 -run Functional ./store/   # functional tests only

The unit tests use no real time, no sockets, no network and no outside
service. The functional tests start a PostgreSQL of their own per test
(`initdb` into a temp dir, a free port, `fsync=off`, stopped with SIGQUIT) and
skip with a message when `initdb` is not on PATH, in
`/opt/homebrew/opt/postgresql@18/bin` or in `/usr/lib/postgresql/*/bin`. Each
server takes System V shared memory; on macOS's default limits, many at once
fail with "could not create shared memory segment", so run with `-parallel 4`
there (docs/TESTING.md). `RunConformance` (conformance_test.go) is
the same set of scenarios run against both, so Fake and PG cannot drift.
`PG.fault` is a fault-injection seam the functional tests set to lose a reply
after COMMIT (guarantee 3) or to panic with the writer lock held; it is nil
in the binary. The functional tests that corrupt an event turn the log's
append-only trigger off for the one statement (`unguardLog`).
