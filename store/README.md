# store

Layer 1 of nova-sprint v2: the one place the sprint's state lives. The
contract is [docs/LAYER-1-STORE.md](../docs/LAYER-1-STORE.md); this is what the
package does with it.

- `PG` is the store in PostgreSQL, schema `sprint` (`schema.sql`, embedded,
  applied by `Migrate`). Open it with `Open(ctx, $NOVA_PG_DSN, $NOVA_PG_PASSWORD)`.
  `Migrate` refuses a store at a schema version other than `SchemaVersion`,
  naming both numbers.
- `Fake` is the same store in memory, for unit tests of the layers above. It
  runs the same rules (`apply.go`, `writes.go`), so its conditions, writes,
  refusal reasons and receipts are PG's. Time comes from `Op.At`; it never
  reads a clock. `Fake.Drop(opID, landed)` makes the next Apply or Clear of
  that id report `ErrUnknown`, landed or not, so callers can test their replay
  path.

## What it guarantees

1. One operation is one transaction: its conditions, its writes and its event
   row land together, or nothing does.
2. An op id applies at most once. A second Apply with a known id returns the
   receipt the first one got, whatever the second one asks for.
3. When the transport fails after the request may have reached the server,
   Apply says the outcome is unknown; it never says nothing changed.
4. A card is in exactly one state and has at most one holder. The schema holds
   this (one `NOT NULL` `state` column with a CHECK, one `holder` column with
   a foreign key to a row of the same epoch), not Go.
5. The log matches the state: folding every accepted event from the first
   (`Rebuild`) gives the snapshot, policy included. Nothing is deleted; a
   clear opens a new epoch and the old one stays readable in the log.
6. A snapshot is one consistent read (one REPEATABLE READ transaction) of the
   current epoch, with the seq it was read at and the epoch.
7. Every condition is checked against the rows as they are when the writes
   land: Apply and Clear take one lock (`LOCK TABLE sprint.epoch`) at the
   start and hold it to commit, at READ COMMITTED. Writers run one at a time,
   so there is no stale check, no lost update and no serialization retry, and
   event seqs are commit order with no gaps: `Events(afterSeq)` never skips one.

## An op

`Op` has an `ID` (unique forever), an `Epoch` (the epoch the caller planned
against; required, epochs start at 1), `Actor`, `Kind` (a label for the log),
`At` (the caller's time), `Expect`, `Writes` and `Note`.

Conditions: `CardGen`, `CardState`, `CardHolder` (`""` is none), `NeedsLanded`
(every need of the card, read from the card inside the transaction, is
landed), `RowIs`, `RowUnder` (the row holds fewer cards than its width; both read
from the row inside the transaction, never from the caller), `StreamHeld`, `CardAbsent`, `AtSeq` (the last event's seq is
N: nothing changed since a snapshot).

Writes, one field each, applied in order: `PutStream` (repo, base, pos;
insert or overwrite), `SetStreamHeld`, `PutCard` (stream, score, needs,
brief; the card must not exist; it starts waiting, gen 0, attempt 0, no
holder), `SetCardState`, `SetCardHolder`, `SetCardAttempt`, `SetCardBranch`,
`SetCardHead`, `SetCardPR`, `SetCardReason`, `SetCardScore`, `SetCardNeeds`,
`PutRow` (kind, width, session; insert or overwrite; a new row is down; a
width below the cards the row holds is refused), `SetRowState`,
`SetRowSession`, `SetRowFlaps`, `PutPolicy`, `SetActivePolicy`. An op that
writes a card raises its gen by one, however many of its writes touch it (gen
counts the operations that changed the card; a card the op adds is at gen 0).
Needs must name other cards of the same epoch, once each.

`Clear` takes an op of kind `clear` with no writes and at most an `AtSeq`
condition. It opens epoch n + 1 and carries every row into it (same name,
kind, width and session; state down, no flaps); streams and cards stay behind
in the old epoch, policy is kept. The event carries the epoch it closed; the
receipt's `NewEpoch` is the one it opened.

## The four outcomes of Apply (and Clear)

| Result | What happened | What the caller does |
|---|---|---|
| `Receipt{Accepted: true}` | The epoch matched, every condition held, every write landed; `Seq` is the event, `After` the touched rows. | Go on. |
| `Receipt{Accepted: false, Reason}` | The epoch was wrong, a condition failed, or a write broke a rule (target missing, card exists, bad state name). Nothing changed; the refusal is an event (`accepted = false`). | Read the reason; take a new snapshot; ask again with a new op id. Exit 1 at the CLI. |
| an error wrapping `ErrInvalid` | The op never entered the store: no id, no epoch, a condition or write outside the closed sets, two writes to one field. Nothing is logged. | Fix the caller: it is a bug. |
| an error wrapping `ErrUnknown` | The request may have reached the server; nothing is known. | `Replay(opID)`: if it comes back, that is the receipt; if not, `Apply` again with the same id. Exit 2 at the CLI. |

Any other error means the request did not reach the store and nothing
happened, except one: a PostgreSQL constraint violation during the writes is
an engine defect (the rules let through what the schema refuses). The
transaction is rolled back, nothing is logged, and the error names the
constraint.

`Events(afterSeq, max)` is exclusive of `afterSeq`, ascending, and returns at
most `MaxEvents` (1000) events; a larger `max`, or `max <= 0`, is capped
silently. `Policy(version)` returns one policy version, or `ErrNotFound`.

## Tests

    go test ./store/                    # unit tests (Fake) and functional tests (PG)
    go test -run Functional ./store/    # functional tests only

The unit tests use no real time, no sockets, no network and no outside
service. The functional tests start a PostgreSQL of their own per test
(`initdb` into a temp dir, a free port, `fsync=off`, stopped with SIGQUIT) and
skip with a message when `initdb` is not on PATH, in
`/opt/homebrew/opt/postgresql@18/bin` or in `/usr/lib/postgresql/*/bin`.
`RunConformance` (conformance_test.go) is the same set of scenarios run
against both, so Fake and PG cannot drift.
