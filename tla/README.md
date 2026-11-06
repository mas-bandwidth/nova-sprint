# tla/: the store model (layer 1)

`Store.tla` models the contract in `docs/LAYER-1-STORE.md`: each operation is
one atomic step over `cards`, `rows`, `events` and `applied`. The caller's view
is modelled too: `phase` per op (`idle`, `unknown`, `done`), the `receipt` the
caller holds, and `lost` (transport failures so far). A transport failure is
either a request lost before the store saw it, or a reply lost after the store
applied it. Either way the caller is left `unknown` and recovers with `Replay`,
or with `Apply` again.

The instance: 2 cards, 2 rows, 7 fixed ops, `MaxLoss = 1` failure per op.
- o1 adds c1.
- o2 and o3 put rows r1 and r2.
- o4 and o5 race to take c1 on the same expectation (`waiting`, gen 0, no
  holder).
- o6 reassigns c1 to r2. It is refused by the foreign key if r2 does not exist.
- o7 adds c2 and sets r1 down in one op. It is refused unless r1 is up.

`witnesses/` holds four reversed witnesses. Each is a copy of `Store.tla` with
one deliberate fault, and TLC must report a violation for each. In each
witness `.cfg` the target invariant is listed first: when one state breaks
several invariants, TLC names the first one in the list.

## Running

Run TLC on a bench host, never on the working machine. From this directory:

```
java -jar tla2tools.jar -workers 4 -config Store.cfg Store.tla
```

From `witnesses/`:

```
java -jar tla2tools.jar -config StoreNoEvent.cfg StoreNoEvent.tla
java -jar tla2tools.jar -config StoreReplayApplies.cfg StoreReplayApplies.tla
java -jar tla2tools.jar -config StoreStaleCheck.cfg StoreStaleCheck.tla
java -jar tla2tools.jar -config StoreTwoHolders.cfg StoreTwoHolders.tla
```

`sh run-tlc.sh /path/to/tla2tools.jar [java]` runs all five in parallel and
writes `logs/<Module>.log`. `sh summarize-logs.sh` prints the result lines.
`sh show-trace.sh <Module>` prints the steps of a counterexample.

## Invariants

- **TypeOK**: every variable stays in its type. A card's gen is bounded by the
  number of ops.
- **SingleStateSingleHolder**: every card has exactly one state from the
  closed set (or is absent), at most one holder, and that holder is a row that
  exists. This is structural in `Store.tla` and stated anyway.
- **IdempotentById**: no op id appears twice in the event log.
- **AppliedIsLog**: the op ids the store remembers are exactly the ids in the
  event log.
- **LogMatchesState**: folding the accepted events from empty gives the
  current cards and rows, and each event's recorded after-state is the fold up
  to and including that event.
- **ConditionsAtomic**: every accepted op's conditions held against the state
  immediately before its own writes (the fold of every earlier event), so no
  update is lost.
- **NoChangeOnRefusal**: a refused op's before- and after-states are equal, and
  both are the state immediately before it.
- **ReceiptMatches**: a caller that thinks it knows the outcome holds exactly
  the stored receipt. Unknown is never reported as unchanged.

## Expected results

| Module | Fault | Expected result |
|---|---|---|
| `Store` | none | pass, no violation |
| `witnesses/StoreNoEvent` | an accepted op's writes land without appending its event | violation of LogMatchesState |
| `witnesses/StoreReplayApplies` | Replay (and Apply of a known id) re-applies the writes and logs again | violation of IdempotentById |
| `witnesses/StoreStaleCheck` | conditions are checked against a snapshot read before another op's writes (Begin/Commit split) | violation of ConditionsAtomic |
| `witnesses/StoreTwoHolders` | holder is a set; set holder adds without clearing the previous holder | violation of SingleStateSingleHolder |
| `Store` with `reach/NeverRefused.cfg` | none (reachability) | violation of NeverRefused: a refusal is reachable |
| `Store` with `reach/NeverTaken.cfg` | none (reachability) | violation of NeverTaken: an accepted take (o4 or o5) is reachable |
| `Store` with `reach/NeverRecovered.cfg` | none (reachability) | violation of NeverRecovered: a caller left unknown reaches done |

The three reach checks show that the passing `Store` run is not vacuous. Run
them from this directory:

```
java -jar tla2tools.jar -config reach/NeverRefused.cfg Store.tla
java -jar tla2tools.jar -config reach/NeverTaken.cfg Store.tla
java -jar tla2tools.jar -config reach/NeverRecovered.cfg Store.tla
```

## Results, TLC 2.19 on hetzner2, 2026-11-06

| Module | Result | States generated / distinct | Depth | Time |
|---|---|---|---|---|
| `Store` | No error has been found | 277523 / 111884 | 10 | 03s |
| `StoreNoEvent` | Invariant LogMatchesState is violated | 2 / 2 | 2 | 00s |
| `StoreReplayApplies` | Invariant IdempotentById is violated | 54 / 54 | 3 | 00s |
| `StoreStaleCheck` | Invariant ConditionsAtomic is violated | 83971 / 44096 | 9 | 02s |
| `StoreTwoHolders` | Invariant SingleStateSingleHolder is violated | 4437 / 3318 | 6 | 00s |
| reach `NeverRefused` | violated | 5 / 5 | 2 | 00s |
| reach `NeverTaken` | violated | 118 / 99 | 4 | 00s |
| reach `NeverRecovered` | violated | 54 / 54 | 3 | 00s |

The counterexamples were checked by hand:
- StoreNoEvent: o1 or o2 lands with no event, so the fold of the log is empty
  and the state is not.
- StoreReplayApplies: o4 is refused and its reply is lost. The caller's
  second Apply of the known id goes down the replay path, which logs o4 a
  second time.
- StoreStaleCheck: o4 and o7 both read r1 as up. o7 commits r1 down, then o4
  commits on its stale read: a lost update.
- StoreTwoHolders: o4 takes c1 for r1, then o6 adds r2, so c1 is held by
  {r1, r2}.

The first instance let all seven ops fail on the wire. It was still growing at
8.7M distinct states (depth 10), so it was stopped and failures were limited to
the racing ops (`Lossy`).
