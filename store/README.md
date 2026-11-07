# store

Layer 1 of nova-sprint: the one place the sprint's state lives. It holds
streams, cards, rows, judgements, cursors and policy versions in the PostgreSQL
schema `sprint`, applies one checked operation at a time in one transaction, and
logs every operation that entered it, accepted or refused. The full reference is
[docs/LAYER-1-STORE.md](../docs/LAYER-1-STORE.md).

| Type | Use |
|---|---|
| `PG` | the store in PostgreSQL. `Open(ctx, $NOVA_PG_DSN, $NOVA_PG_PASSWORD)`, then `Migrate(ctx)`. |
| `Fake` | the same store in memory, with the same rules and refusal reasons, for unit tests of layers above. `NewFake()`, `NewFakeFrom(snapshot, policies...)`. |
| `Store` | the interface both implement: `Snapshot`, `Apply`, `Replay`, `Events`, `EventIdentity`, `Restored`, `Clear`, `Policy`. |

Other entry points: `CheckSchema(ctx, dsn)` (what a database holds, read-only),
`RestoreBegin`/`RestoreEnd` (restore a data-only dump in one transaction),
`ValidateHistory` (the log is the whole history of the tables), `Rebuild` and
`Fold` (the one fold of the log into a snapshot), `Balances` (provider balance
reads), `Landings` (producer landing times).

## Example

```go
s, err := store.Open(ctx, os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_PG_PASSWORD"))
if err != nil { return err }
defer s.Close()
if err := s.Migrate(ctx); err != nil { return err }

snap, _ := s.Snapshot(ctx)
u := snap.Cards["u1"] // a work consumer of producer c1, ready and held by row w1
op := store.Op{
    ID: "take-u1-7", Epoch: snap.Epoch, Actor: "w1", Kind: "take", At: now,
    Expect: []store.Expect{store.CardGen{Card: "u1", Gen: u.Gen}, store.RowHeld{Row: "w1", Held: false}},
    Writes: []store.Write{store.SetCardState{Card: "u1", State: "working"}},
}
r, err := s.Apply(ctx, op)
switch {
case errors.Is(err, store.ErrInvalid): // a caller bug; nothing was logged
case errors.Is(err, store.ErrUnknown): // Replay(ctx, op.ID); if not found, Apply the same op again
case err != nil:                       // nothing happened
case !r.Accepted:                      // refused: r.Reason; take a new snapshot, plan again with a new id
default:                               // accepted: r.Seq is the event, r.After the touched rows
}
```

## The four outcomes of `Apply` and `Clear`

| Result | What happened | What the caller does |
|---|---|---|
| `Receipt{Accepted: true}` | the epoch matched, every condition held, every write landed | go on |
| `Receipt{Accepted: false, Reason}` | wrong epoch, a failed condition or a write that broke a rule; nothing changed, the refusal is an event | read the reason, re-plan with a new op id (CLI exit 1) |
| error wrapping `ErrInvalid` | the op never entered the store; nothing is logged | fix the caller |
| error wrapping `ErrUnknown` | the connection failed or the context ended once the request may have been sent, or an error came during or after COMMIT | `Replay(opID)`; if not found, `Apply` again with the same id (CLI exit 2) |

Any other error means the transaction rolled back and nothing happened.

## Rules to remember

- Time and op ids come from the caller; the store never reads a clock.
- `Events(afterSeq, max)` is exclusive of `afterSeq`, ascending, at most
  `MaxEvents` (1000).
- The store holds no width and no tier check; those are configuration, enforced
  by the deal guards from the roster.
- A set write that changes nothing is refused.
- `Fake` runs the same rules as `PG`; refusal reasons that depend on name order
  use byte order in both.

## Tests

```
go test -count=1 -skip Functional ./store/...          # unit tier, against Fake
go test -count=1 -parallel 4 -run Functional ./store/  # functional tier, a throwaway PostgreSQL per test
```

The unit tests use no real time, sockets, network or outside service. The
functional tests start a PostgreSQL of their own per test (`initdb` into a temp
dir, a free port, `fsync=off`) and skip with a message when `initdb` is not on
`PATH`, in `/opt/homebrew/opt/postgresql@18/bin` or in
`/usr/lib/postgresql/*/bin`. `RunConformance` runs one set of scenarios against
`Fake` and `PG` so they cannot drift. The model is `tla/Store.tla`; the code's
random walk is checked against it by trace validation (`trace_tla_test.go`).
