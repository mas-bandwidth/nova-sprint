# tla/: the store model (layer 1)

`Store.tla` models the contract in `docs/LAYER-1-STORE.md`. Each operation is
one atomic step over `epoch`, `cards`, `rows`, `events` and `applied`. In
order, the step:
1. checks the op's epoch against the store's epoch;
2. checks its conditions;
3. either applies every write and appends an accepted event, or changes
   nothing and appends a refused event.

`Clear` is an op with no writes. It closes the epoch and opens the next. It
carries the rows forward (same width, state `down`) and leaves the cards
behind. It is idempotent by id and logged with the epoch it closed.

Every card write raises the card's gen by one. The conditions are:
- `CardGen`, `CardState`, `HolderIs` and `CardAbsent` on a card;
- `NeedsLanded`: every need of the card is landed, read from the store in the
  step;
- `RowIs` on a row;
- `RowUnder(R, W)`: fewer than W cards have R as their holder, counted in the
  step.

The caller's view is modelled too:
- `phase` per op: `idle`, `unknown` or `done`;
- `receipt`: the receipt the caller holds;
- `lost`: the transport failures so far.

A transport failure is either a request lost before the store saw it, or a
reply lost after the store applied it. Either way the caller is left
`unknown` and recovers with `Replay`, or with `Apply` again.

## What one model step stands for in SQL

One step of `Store.tla` is one SQL transaction. It runs at `READ COMMITTED`
from `LOCK TABLE sprint.epoch` to `COMMIT`, and checks the epoch, the
conditions, the writes and the event row inside it. The lock is what makes
the atomic step real. Without it, two transactions at `READ COMMITTED` can
each read the rows, decide, and then write. Two witnesses are exactly that
interleaving:
- **StaleCheck:** every condition is judged on an earlier read, so two takes
  of one card both land.
- **WidthStale:** only the holder count is judged on an earlier read, so two
  deals overfill a width-1 row.

With the lock, writers run one at a time and each one reads the state the
previous one committed. That serial order is the order of `events` here.

## The instance

3 cards, 2 rows, 14 fixed ops. Callers issue their ops in program order
(`Pred`). A setup caller goes first:
- s1 to s3 add c1, c2, and c3 (which needs c1);
- s4 and s5 put rows r1 and r2, width 1, up.

Then six callers run concurrently:

| Caller | Ops | What it does |
|---|---|---|
| a | a1, a2 | a1 takes c1 for r1; a2 lands c1 if r1 holds it |
| b | b1, b2 | b1 takes c1 for r2 (racing a1 on the same expectation); b2 reassigns c1 to r1 if r1 has room |
| w | w1 | deals c2 to r1 (racing a1 for r1's one slot) |
| n | n1 | readies c3 once its need c1 is landed |
| e | e1 | an epoch-0 put row that may arrive after the clear |
| k | k1, k2 | k1 clears epoch 0; k2 adds c2 in epoch 1 |

Transport failures happen only on a1 and k1 (`Lossy`, `MaxLoss = 1`).

Left out:
- streams, policy and the seq fence;
- the card and row fields that no condition reads (attempt, branch, head, pr,
  reason, score, kind, session, flaps). They add columns, not behaviour.

There is no heartbeat: rows have no seen field.

`witnesses/` holds six reversed witnesses. Each is a copy of `Store.tla` with
one deliberate fault, and TLC must report a violation for each. In each
witness `.cfg` the target invariant is listed first: when one state breaks
several invariants, TLC names the first one in the list. `reach/` holds six
reachability checks on `Store.tla`. Each must be violated, which shows the
passing run is not vacuous.

## Running

Run TLC on a bench host, never on the working machine. From this directory:

```
java -jar tla2tools.jar -workers 4 -config Store.cfg Store.tla
java -jar tla2tools.jar -config reach/NeverTaken.cfg Store.tla
java -jar tla2tools.jar -config reach/NeverRecovered.cfg Store.tla
java -jar tla2tools.jar -config reach/NeverStaleEpochRefused.cfg Store.tla
java -jar tla2tools.jar -config reach/NeverNeedsReady.cfg Store.tla
java -jar tla2tools.jar -config reach/NeverNewEpochOp.cfg Store.tla
java -jar tla2tools.jar -config reach/NeverWidthFull.cfg Store.tla
```

From `witnesses/`:

```
java -jar tla2tools.jar -config StoreNoEvent.cfg StoreNoEvent.tla
java -jar tla2tools.jar -config StoreReplayApplies.cfg StoreReplayApplies.tla
java -jar tla2tools.jar -config StoreStaleCheck.cfg StoreStaleCheck.tla
java -jar tla2tools.jar -config StoreTwoHolders.cfg StoreTwoHolders.tla
java -jar tla2tools.jar -config StoreEpochUnfenced.cfg StoreEpochUnfenced.tla
java -jar tla2tools.jar -config StoreWidthStale.cfg StoreWidthStale.tla
```

The helper scripts:
- `sh run-tlc.sh /path/to/tla2tools.jar [java]` runs all thirteen in parallel
  and writes `logs/<name>.log`. Each run gets its own `java.io.tmpdir`,
  because parallel runs that share `/tmp` race while unpacking the standard
  modules.
- `sh run-one.sh` runs a single one.
- `sh summarize-logs.sh` prints the result lines.
- `sh show-trace.sh <name>` prints a counterexample's steps and its event log.

## Invariants

- **TypeOK**: every variable stays in its type.
- **SingleStateSingleHolder**: every card has exactly one state from the
  closed set (or is absent), at most one holder, and that holder is a row of
  this epoch. This is structural in `Store.tla` and stated anyway.
- **WidthKept**: no row has more cards holding it than its width.
- **IdempotentById**: no op id appears twice in the event log.
- **AppliedIsLog**: the op ids the store remembers are exactly the ids in the
  event log.
- **LogMatchesState**: folding the accepted events from empty, with a clear
  applied the same way the store applies it, gives the current state. Each
  event records the epoch it ran in. It is checked as a chain, which is the
  fold by induction:
  - each event starts where the one before it ended;
  - each accepted event's after-state is its writes applied to its
    before-state;
  - the last event ends at the current state.
- **OpsInTheirEpoch**: every accepted op, clear included, was planned against
  the epoch the store was at when it applied. So after a clear, a delayed op
  planned against the old epoch is refused.
- **ConditionsAtomic**: every accepted op's conditions held against the state
  immediately before its own writes, so no update is lost.
- **NoChangeOnRefusal**: a refused op's before- and after-states are equal,
  and both are the state immediately before it.
- **ReceiptMatches**: a caller that thinks it knows the outcome holds exactly
  the stored receipt. Unknown is never reported as unchanged.

## Expected results

| Module | Fault | Expected result |
|---|---|---|
| `Store` | none | pass, no violation |
| `witnesses/StoreNoEvent` | an accepted op's writes land without appending its event | violation of LogMatchesState |
| `witnesses/StoreReplayApplies` | Replay (and Apply of a known id) runs the op again and logs it again | violation of IdempotentById |
| `witnesses/StoreStaleCheck` | the epoch and conditions are checked against a snapshot read before another op's writes (no lock) | violation of ConditionsAtomic |
| `witnesses/StoreTwoHolders` | holder is a set; set holder adds without clearing the previous holder | violation of SingleStateSingleHolder |
| `witnesses/StoreEpochUnfenced` | ops other than clear are not checked against the store's epoch | violation of OpsInTheirEpoch |
| `witnesses/StoreWidthStale` | RowUnder is checked against a holder count read before another deal (no lock) | violation of WidthKept |
| `Store` + `reach/NeverTaken.cfg` | none | violation: a take (a1 or b1) is accepted |
| `Store` + `reach/NeverRecovered.cfg` | none | violation: a caller left unknown reaches done |
| `Store` + `reach/NeverStaleEpochRefused.cfg` | none | violation: an old-epoch op arriving after the clear is refused |
| `Store` + `reach/NeverNeedsReady.cfg` | none | violation: n1 passes NeedsLanded |
| `Store` + `reach/NeverNewEpochOp.cfg` | none | violation: an op planned against epoch 1 is accepted |
| `Store` + `reach/NeverWidthFull.cfg` | none | violation: a row fills to its width |

## Results, TLC 2.19 on hetzner2, 2026-11-06

All thirteen ran in one parallel pass.

| Module | Result | States generated / distinct | Depth | Time |
|---|---|---|---|---|
| `Store` | No error has been found | 1035188 / 618631 | 17 | 22s |
| `StoreNoEvent` | Invariant LogMatchesState is violated | 2 / 2 | 2 | 00s |
| `StoreReplayApplies` | Invariant IdempotentById is violated | 25 / 25 | 8 | 00s |
| `StoreStaleCheck` | Invariant ConditionsAtomic is violated | 461 / 270 | 15 | 00s |
| `StoreTwoHolders` | Invariant SingleStateSingleHolder is violated | 45 / 42 | 8 | 00s |
| `StoreEpochUnfenced` | Invariant OpsInTheirEpoch is violated | 85 / 78 | 8 | 00s |
| `StoreWidthStale` | Invariant WidthKept is violated | 524 / 321 | 15 | 00s |
| reach `NeverTaken` | violated | 7 / 7 | 7 | 00s |
| reach `NeverRecovered` | violated | 25 / 25 | 8 | 00s |
| reach `NeverStaleEpochRefused` | violated | 79 / 73 | 8 | 00s |
| reach `NeverNeedsReady` | violated | 106 / 89 | 9 | 00s |
| reach `NeverNewEpochOp` | violated | 86 / 79 | 8 | 00s |
| reach `NeverWidthFull` | violated | 7 / 7 | 7 | 00s |

The counterexamples were checked by hand (`show-trace.sh`):
- **StoreNoEvent:** s1 lands with no event, so the log ends at empty and the
  state does not.
- **StoreReplayApplies:** a1 is accepted and its reply is lost. The caller's
  second Apply of the known id runs a1 again, which is refused and logged a
  second time.
- **StoreStaleCheck:** after setup, a1 and b1 both begin, both commit and are
  both accepted, so c1 is taken twice.
- **StoreTwoHolders:** b1 takes c1 for r2, then b2 adds r1, so c1 is held by
  {r1, r2}.
- **StoreEpochUnfenced:** k1 clears epoch 0, then the epoch-0 e1 is accepted
  in epoch 1.
- **StoreWidthStale:** a1 and w1 both begin on the width-1 row r1 with a
  count of 0, and both land.
