# store

Layer 1 of nova-sprint v2: the one place the sprint's state lives. The
contract is [docs/LAYER-1-STORE.md](../docs/LAYER-1-STORE.md); this is what the
package does with it.

- `PG` is the store in PostgreSQL, schema `sprint` (`schema.sql`, embedded,
  applied by `Migrate`). Open it with `Open(ctx, $NOVA_PG_DSN, $NOVA_PG_PASSWORD)`.
  `Migrate` refuses a store at a schema version other than `SchemaVersion`,
  naming both numbers, and is safe to run concurrently (it holds one advisory
  lock). The binary knows schema version 2 (version 1 plus the card columns
  `author` and `base_sha`, layer 2's). There is no upgrade path yet: a store
  at version 1 is refused like any other version.
- `Fake` (`NewFake()`) is the same store in memory, for unit tests of the
  layers above. It runs the same rules and the same protocol (`apply.go`,
  `check.go`, `writes.go`), so its conditions, writes, refusal reasons and
  receipts are PG's. Time comes from `Op.At`; it never reads a clock.
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
c := snap.Cards["c1"]
op := store.Op{
    ID: "take-c1-7", Epoch: snap.Epoch, Actor: "w1", Kind: "take", At: now,
    Expect: []store.Expect{store.CardGen{Card: "c1", Gen: c.Gen}},
    Writes: []store.Write{store.SetCardState{Card: "c1", State: "working"}, store.SetCardHolder{Card: "c1", Holder: "w1"}},
}
r, err := s.Apply(ctx, op)
switch {
case errors.Is(err, store.ErrInvalid): // a caller bug: fix the op; nothing was logged
case errors.Is(err, store.ErrUnknown): // r, found, err := s.Replay(ctx, op.ID); if !found, Apply the same op again
case err != nil: // did not reach the store, or an engine defect: nothing happened
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
3. When the transport fails after the request may have reached the server,
   Apply says the outcome is unknown; it never says nothing changed. A
   failure whose outcome is known (an invalid body, a PostgreSQL data or
   constraint error) is never reported as unknown.
4. A card is in exactly one state and has at most one holder. The schema holds
   this (one `NOT NULL` `state` column with a CHECK, one `holder` column with
   a foreign key to a row of the same epoch), not Go.
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
landed), `RowIs`, `StreamHeld`, `CardAbsent`, `AtSeq` (the last event's seq is
N: nothing changed since a snapshot). There is no width condition: the holder
write is the check.

Writes, one field each, applied in order: `PutStream` (repo, base, pos;
insert or overwrite), `SetStreamHeld`, `PutCard` (stream, score, needs,
brief; the card must not exist; it starts waiting, gen 0, attempt 0, no
holder), `SetCardState`, `SetCardHolder` (a row's name is refused when that
row already holds as many cards as its width, counted at the moment of the
write and excluding the card written, so a second holder write in one op
counts the first and nothing lands, and re-setting the same holder is a
no-op), `SetCardAttempt`, `SetCardBranch`, `SetCardHead`, `SetCardPR`,
`SetCardReason`, `SetCardScore`, `SetCardAuthor` (the row whose work the head
is), `SetCardBaseSHA` (the base commit the attempt started from),
`SetCardNeeds`, `PutRow` (kind, width, session; insert or overwrite; a new row
is down; a width below the cards the row holds is refused; width 0 is a row
that takes nothing), `SetRowState`, `SetRowSession`, `SetRowFlaps`,
`PutPolicy`, `SetActivePolicy`. An op that writes a card raises its gen by
one, however many of its writes touch it (gen counts the operations that
changed the card; a card the op adds is at gen 0). Needs must name other
cards of the same epoch, once each. The card states are `waiting`, `ready`,
`working`, `review`, `merging`, `landed`, `fail`.

The value limits, each `ErrInvalid` from both stores, naming the field:

- every integer a write carries (pos, score, attempt, pr, width, flaps,
  policy version) is in 0..2147483647 (a PostgreSQL `integer`, nothing
  meaningful below 0); the op's epoch is in 1..2147483647;
- every text (ids, names, actor, kind, note, brief, reason, head, branch,
  author, base sha, needs, policy name and body) is valid UTF-8 with no NUL
  byte;
- `At` is set and falls in the years 0..9999.

`Clear` takes an op of kind `clear` with no writes and at most an `AtSeq`
condition. It opens epoch n + 1 and carries every row into it (same name,
kind, width and session; state down, no flaps); streams and cards stay behind
in the old epoch, policy is kept. The event carries the epoch it closed; the
receipt's `NewEpoch` is the one it opened.

## The four outcomes of Apply (and Clear)

| Result | What happened | What the caller does |
|---|---|---|
| `Receipt{Accepted: true}` | The epoch matched, every condition held, every write landed; `Seq` is the event, `After` the touched rows. | Go on. |
| `Receipt{Accepted: false, Reason}` | The epoch was wrong, a condition failed, or a write broke a rule (target missing, card exists, bad state name, row full). Nothing changed; the refusal is an event (`accepted = false`). | Read the reason; take a new snapshot; ask again with a new op id. Exit 1 at the CLI. |
| an error wrapping `ErrInvalid` | The op never entered the store: no id, no epoch, a condition or write outside the closed sets, two writes to one field, a value outside the limits. Nothing is logged. | Fix the caller: it is a bug. |
| an error wrapping `ErrUnknown` | The request may have reached the server; nothing is known. | `Replay(opID)`: if it comes back, that is the receipt; if not, `Apply` again with the same id. Exit 2 at the CLI. |

Any other error means nothing happened: the request did not reach the store,
or PostgreSQL refused a write with a data (SQLSTATE class 22) or constraint
(class 23) error, an engine defect (the rules let through what the schema
refuses). The transaction is rolled back, nothing is logged, and the error
names the SQLSTATE and the constraint or column.

`Events(afterSeq, max)` is exclusive of `afterSeq`, ascending, and returns at
most `MaxEvents` (1000) events; a larger `max`, or `max <= 0`, is capped
silently. An image that does not decode is an error naming its seq, never a
panic. `Policy(version)` returns one policy version, or `ErrNotFound`.

## Tests

    go test ./store/                    # unit tests (Fake) and functional tests (PG)
    go test -run Functional ./store/    # functional tests only

The unit tests use no real time, no sockets, no network and no outside
service. The functional tests start a PostgreSQL of their own per test
(`initdb` into a temp dir, a free port, `fsync=off`, stopped with SIGQUIT) and
skip with a message when `initdb` is not on PATH, in
`/opt/homebrew/opt/postgresql@18/bin` or in `/usr/lib/postgresql/*/bin`. Each
server takes System V shared memory; on macOS's default limits, many at once
can fail with "could not create shared memory segment", so `-parallel 6` is
the safe setting on a busy machine. `RunConformance` (conformance_test.go) is
the same set of scenarios run against both, so Fake and PG cannot drift.
`PG.fault` is a fault-injection seam the functional tests set to lose a reply
after COMMIT (guarantee 3) or to panic with the writer lock held; it is nil
in the binary.
