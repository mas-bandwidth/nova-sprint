# card

Layer 2 of nova-sprint: the card machine. Every legal move of a producer card, a
consumer card, a row and a stream, as a pure function from the records a move
reads to the plan layer 1 applies, or a refusal. No store, no clock, no I/O, no
globals. The full reference is
[docs/LAYER-2-CARD-MACHINE.md](../docs/LAYER-2-CARD-MACHINE.md); the model is
`tla/CardMachine.tla` and `card/trace.go` maps the code's state onto it.

## Calling it

```go
plan, r := card.Move(p, consumers, rows, stream, needs, card.Start{Row: "r1", Branch: "b", Base: sha})
if r != nil { /* refused: r.Reason names the fix */ }
op := store.Op{ID: id, Epoch: snap.Epoch, Actor: who, Kind: "start", At: now, Expect: plan.Expect, Writes: plan.Writes}
rec, err := st.Apply(ctx, op) // a refused op is rec.Accepted == false, err == nil
```

`card.FromStore`, `card.RowFromStore(r, width, tiers)` and `card.StreamFromStore`
turn the store's records into the machine's; width and tiers come from the
roster.

| Function | Moves |
|---|---|
| `Move(p, consumers, rows, stream, needs, e)` | producer and sentinel events (`Resolve`, `Park`, `Reached`, `Start`, `Finished`, `Broken`, `Faulted`, `AskRead`, `AskReads`, `ReadYes`, `ReadFix`, `WaiveRed`, `Landed`, `Red`, `Released`, `Cancel`, `Replace`, `NeedFailed`, `Waive`, `Relink`, `Edit`, `Rescore`, `SetPriority`, `Block`, `Unblock`) |
| `MoveConsumer(c, p, r, e)` | `Report`, `Withdraw` |
| `MoveRow(r, e)` | `Seen`, `Gone`, `Hold`, `Unhold` |
| `MoveStream(s, e)` | `HoldStream{Reason, Ready, Seq}`, `UnholdStream` |
| `MoveAdd(e, cards)` | a new producer or sentinel |
| `Merge(plans...)` | sequential composition of plans into one op |
| `ReportPlan(...)` | a row's report as one plan (the consumer's `Report` plus the producer's follow-on move) |

- `consumers` passed to `Move` is every consumer whose `Of` is the producer, and
  the list must be complete: a working producer whose current work consumer is
  not passed is refused.
- `needs` is every card the event reads (for `Relink` every card reachable through
  needs and after; a walk that reaches a card not passed is refused).
- `Merge` re-plans each move against the records read before any write of the op,
  with each earlier move's writes folded in; the last write of a field wins, a
  write that changes nothing is dropped, and a later write that would erase a move
  into `working` is refused.
- Consumer ids are the machine's: `<producer>.w<attempt>` for work and
  `<producer>.r<review>.<n>` for reads. A card id is at most 104 bytes, contains no
  `.`, and so every consumer id fits in the store's 128.
- Events carry no actor; who may run a verb is layer 3's.

## Tests

```
go test -count=1 -skip Functional ./card/...   # unit tier
go test -count=1 -run Functional ./card/...    # merged plans against a throwaway PostgreSQL
```

`move_test.go` accepts every table row once with its plan asserted exactly and
refuses it from every other state with its exact reason. `conformance_test.go`
drives producers through the happy, fix, red, fault, release, withdraw, block,
park, need, sentinel, preemption, row, stale-relink, stream-hold, drop and add
paths on `store.Fake`, applying each plan as a real op. `report_test.go` covers
`ReportPlan`. `trace_test.go` and `trace_tla_test.go` project a random walk onto
the model and check it is a behaviour of `tla/CardMachine.tla`. The Merge tests
compare each merged op with its plans applied one at a time on a shadow `Fake`
and, in the functional tier, against PostgreSQL.
