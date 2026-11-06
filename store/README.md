# store

Layer 1 of nova-sprint v2: the one place the sprint's state lives. The
contract is [docs/LAYER-1-STORE.md](../docs/LAYER-1-STORE.md); this is what the
package does with it.

- `PG` is the store in PostgreSQL, schema `sprint` (`schema.sql`, embedded,
  applied by `Migrate`). Open it with `Open(ctx, $NOVA_PG_DSN, $NOVA_PG_PASSWORD)`.
- `Fake` is the same store in memory, for unit tests of the layers above. It
  runs the same rules (`apply.go`), so its conditions, writes, refusal reasons
  and receipts are PG's. Time comes from `Op.At`; it never reads a clock.
  `Fake.Drop(opID, landed)` makes the next Apply of that id report
  `ErrUnknown`, landed or not, so callers can test their replay path.

## What it guarantees

1. One operation is one transaction: its conditions, its writes and its event
   row land together, or nothing does.
2. An op id applies at most once. A second Apply with a known id returns the
   receipt the first one got, whatever the second one asks for.
3. When the transport fails after the request may have reached the server,
   Apply says the outcome is unknown; it never says nothing changed.
4. A card is in exactly one state and has at most one holder. The schema holds
   this (one `state` column with a CHECK, one `holder` column with a foreign
   key to a row of the same epoch), not Go.
5. The log matches the state: folding every accepted event (`Rebuild`) gives
   the snapshot. Nothing is deleted; a clear starts a new epoch.
6. A snapshot is one consistent read (one REPEATABLE READ transaction) of the
   current epoch.

Every Apply and Clear takes the same table lock first, so operations run one
at a time, every condition is read after the previous operation committed,
and event seqs are in commit order with no gaps: a reader of `Events(afterSeq)`
never misses one.

## The three outcomes of Apply

| Apply returns | What happened | What the caller does |
|---|---|---|
| `Receipt{Accepted: true}` | Every condition held, every write landed; `Seq` is the event, `After` the touched rows. | Go on. |
| `Receipt{Accepted: false, Reason}` | A condition failed or a write was refused. Nothing changed; the refusal is an event (`accepted = false`). | Read the reason; take a new snapshot before asking again with a new op id. Exit 1 at the CLI. |
| an error wrapping `ErrUnknown` | The request may have reached the server; nothing is known. | `Replay(opID)`: if it comes back, that is the receipt; if not, `Apply` again with the same op id. Exit 2 at the CLI. |

Any other error (an op without an id, a clear with conditions, a failure to
begin the transaction) means nothing was sent and nothing changed.

## Tests

    go test ./store/                    # unit tests (Fake) and functional tests (PG)
    go test -run Functional ./store/    # functional tests only

The unit tests use no real time, no sockets, no network and no outside
service. The functional tests start a PostgreSQL of their own per test
(`initdb` into a temp dir, a free port, `fsync=off`) and skip with a message
when `initdb` is not on PATH, in `/opt/homebrew/opt/postgresql@18/bin` or in
`/usr/lib/postgresql/*/bin`. `RunConformance` (conformance_test.go) is the same
set of scenarios run against both, so Fake and PG cannot drift.
