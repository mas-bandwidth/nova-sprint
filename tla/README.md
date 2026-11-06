# tla/: the store model (layer 1)

`Store.tla` models the contract in `docs/LAYER-1-STORE.md` (v2 at 293f4ed6).
Each operation is one atomic step over `epoch`, `cards`, `rows`, `events` and
`applied`. In order, the step:
1. checks the op's epoch against the store's epoch;
2. checks its conditions;
3. runs its writes in order, each under its own rule;
4. either lands everything and appends an accepted event, or changes nothing
   and appends a refused event.

Epochs start at 1. `Clear` is an op with no writes and its epoch is checked
too. It opens the next epoch, carries the rows forward (same width and held
flag, state `down`, gen + 1) and leaves the cards behind. It is idempotent
by id and logged with the epoch it closed.

Cards have a kind: `producer`, `sentinel`, or a consumer (`work` or `read`)
that names its producer (`prod`, the design's `of`). A producer or sentinel
never has a holder: the holder write refuses it. A consumer has no needs.

The conditions are:
- `CardGen`, `CardState`, `HolderIs` and `CardAbsent` on a card;
- `NeedsLanded`: every need of the card is landed, read from the store in the
  step;
- `RowIs`, `RowGen` and `RowHeld` on a row.

There is no width condition. A row's width counts the consumers it holds in
`working`, never `ready`, `done` or `fail`, so a row may hold ready (queued)
consumers past its width. Width is kept by the write rules:
- **Set state into `working`** on a held card, and **set holder** on a card
  already in `working`: refused when that row already holds width other
  working cards, counted at the moment of the write. So one op with two such
  writes to a width-1 row is refused on the second, and nothing lands.
- **Put row** and **set row width**: refused below the row's working
  holders.

An op that writes a card or a row raises its gen by one, however many of its
writes touch it. A card or row the op itself adds stays at gen 0. A row's
held flag is set by its own write (`SetRowHeld`), which raises the row's gen.

The caller's view is modelled too:
- `phase` per op: `idle`, `unknown` or `done`;
- `receipt`: the receipt the caller holds;
- `lost`: the transport failures so far.

A transport failure is either a request lost before the store saw it, or a
reply lost after the store applied it. Either way the caller is left
`unknown` and recovers with `Replay`, or with `Apply` again.

## What one model step stands for in SQL

One model step = one SQL transaction from `LOCK TABLE sprint.epoch` to COMMIT
at READ COMMITTED; the lock is what makes the atomic step real. The epoch,
the conditions, the writes with their rules, and the event row are all
inside it. Without the lock, two transactions at READ COMMITTED can each
read the rows, decide, and then write. Two witnesses are exactly that
interleaving:
- **StoreStaleCheck:** the conditions are judged on an earlier read, so a1
  deals to r1 after g1 held it.
- **StoreWidthStale:** the width check is judged on an earlier read, so a1
  lands a working consumer after e1 lowered the width to 0.

With the lock, writers run one at a time and each one reads the state the
previous one committed. That serial order is the order of `events` here.

## The instance

6 cards (producers p1, p2; sentinel t1; consumers u1, u2, u3), 2 rows of
width 1, 21 fixed ops. Callers issue their ops in program order (`Pred`). A
setup caller goes first, covering each deterministic rule once:
- s1, s2 add p1, p2 (needs p1) and t1 (needs p2); s3, s4 put r1 and r2,
  width 1, up;
- s5 and s6 set a holder on the producer and on the sentinel: both refused;
- s7 readies p2 before its need landed (refused), s8 lands p1, s9 readies
  p2;
- s10 adds u1 and u2, both working, to the width-1 r2 in one op: refused on
  its second holder write;
- s11 adds u3 ready on r2, s12 moves it to r1, where it stays queued.

Then seven callers run concurrently:

| Caller | Ops | What it does |
|---|---|---|
| a | a1, a2 | a1 deals new consumer u1 working to r1, fenced on RowHeld(r1, false); a2 marks it done (it keeps r1, the slot frees) |
| b | b1, b2 | b1 adds u2 ready on r1; b2 makes it working (the work-entry check) |
| e | e1 | lowers r1's width to 0, fenced on RowGen(r1, 0) |
| g | g1 | holds r1, fenced on RowGen(r1, 0) |
| x | x1 | sets r2 up, fenced only by its epoch |
| k | k1, k2 | k1 clears epoch 1; k2 adds p1 in epoch 2 |

Transport failures happen only on a1 and k1 (`Lossy`, `MaxLoss = 1`).

Left out:
- streams, policy and the seq fence;
- the soft `after` list (it follows the needs rule; no store rule reads it);
- the card and row fields no rule reads (attempt, author, branch, base_sha,
  prior_head, head, pr, result, bounces, cost, priced, priority, brief,
  reason, score, row kind, session, flaps). They add columns, not behaviour.

## Files

- `Store.tla`, `Store.cfg`: the model and its passing run.
- `witnesses/`: fourteen reversed witnesses, each `Store.tla` with one fault.
  They are generated: `python3 make-witnesses.py` writes every witness and
  every cfg (`Store.cfg`, `witnesses/*.cfg`, `reach/*.cfg`) from `Store.tla`
  and the fault list in the script. Each fault is a set of exact text
  replacements; one that no longer matches stops the script. Edit
  `Store.tla`, rerun the script, commit both.
- `reach/`: seventeen reachability checks on `Store.tla`. Each must be
  violated, which shows the passing run is not vacuous.

In each witness cfg the target invariant is listed first: when one state
breaks several invariants, TLC names the first in the list. StoreWidthStale
leaves NoSpuriousRefusal out (its cfg says why): a stale count also refuses a
deal the live state allows, which TLC finds before the over-fill.

## Running

Run TLC on a bench host, never on the working machine. `ci-tlc.sh` is the
gate. It runs every module in parallel:
- `Store.tla` must pass (TLC exit 0);
- every `witnesses/*.tla` and every `reach/*.cfg` must report a violation
  (TLC exit 12) of the first invariant in its cfg.

It exits non-zero on any other outcome: a pass where a violation is
required, a violation of a different invariant, a parse error, a crash, or a
run that wrote no result. It uses a pinned, sha256-checked `tla2tools.jar`
(v1.8.0), gives each run its own `java.io.tmpdir`, and passes
`-noGenerateSpecTE`, so a rerun in the same tree never sees trace modules.

```
bash ci-tlc.sh
```

The helper scripts:
- `sh run-tlc.sh /path/to/tla2tools.jar [java]` runs the same set, one log
  per run in `logs/`;
- `sh run-one.sh` runs a single one;
- `sh summarize-logs.sh` prints the result lines;
- `sh show-trace.sh <name>` prints a counterexample's event ids, and
  `sh show-all-traces.sh` does it for every violated log;
- `sh last-state.sh <name>` prints the epoch, cards and rows of a
  counterexample's last state.

## Invariants

- **TypeOK**: every variable stays in its type.
- **SingleStateSingleHolder**: every card has exactly one state from the
  closed set (or is absent), at most one holder, and that holder is a row of
  this epoch.
- **ProducerNeverHeld**: no producer or sentinel has a holder.
- **ConsumersWellFormed**: a consumer names an existing producer and has no
  needs; a producer or sentinel names none.
- **WidthKept**: no row holds more `working` cards than its width.
- **IdempotentById**: no op id appears twice in the event log.
- **AppliedIsLog**: the op ids the store remembers are exactly the ids in the
  log.
- **LogMatchesState**: folding the accepted events from empty gives the
  current state. Rebuild applies each accepted event's writes and never
  rechecks a condition or a write rule. Checked as a chain (the fold by
  induction): each event starts where the one before ended, each accepted
  event's after-state is its writes applied to its before-state, and the
  last ends at the current state.
- **OpsInTheirEpoch**: every accepted op, clear included, was planned against
  the epoch the store was at when it applied.
- **ConditionsAtomic**: every accepted op's conditions held in the state
  immediately before its own writes.
- **NoChangeOnRefusal**: a refused op changed nothing.
- **NoSpuriousRefusal**: every refusal has a cause in the spec (the epoch, a
  condition, or a write rule as the design states it), so a store rule
  stricter than the design is caught.
- **GenPerOp**: each accepted op raises the gen of each card and row it
  writes that existed before it by exactly one; one it creates is at 0; a
  clear raises every carried row's gen by one.
- **ClearCarriesRows**: a clear keeps every row's width and held flag, sets
  it down, and leaves no card.
- **ReceiptMatches**: a caller that thinks it knows the outcome holds exactly
  the stored receipt.

## Expected results

| Module | Fault | Expected |
|---|---|---|
| `Store` | none | pass |
| `StoreNoEvent` | an accepted op lands without its event | LogMatchesState violated |
| `StoreReplayApplies` | Replay runs the op again and logs it again | IdempotentById violated |
| `StoreStaleCheck` | conditions judged on a snapshot read before another op's commit (no lock) | ConditionsAtomic violated |
| `StoreTwoHolders` | holder is a set; set holder adds without clearing | SingleStateSingleHolder violated |
| `StoreEpochUnfenced` | ops other than clear are not epoch-checked | OpsInTheirEpoch violated |
| `StoreWidthStale` | width check judged on a snapshot (no lock) | WidthKept violated |
| `StoreWidthUnchecked` | put row and set row width accept any width | WidthKept violated |
| `StoreWidthPerOp` | the holder write counts as at the start of the op | WidthKept violated |
| `StoreWorkEntryUnchecked` | a state write into working is not width-checked | WidthKept violated |
| `StoreProducerHolds` | the holder write refuses neither producer nor sentinel | ProducerNeverHeld violated |
| `StoreSentinelHolds` | the holder write refuses a producer but not a sentinel | ProducerNeverHeld violated |
| `StoreWidthCountsDone` | done and fail holders are counted | NoSpuriousRefusal violated |
| `StoreWidthCountsReady` | ready holders are counted (Go's live count) | NoSpuriousRefusal violated |
| `StoreRowStale` | RowGen is not checked | ConditionsAtomic violated |
| reach `NeverTaken` | none | a1 is accepted |
| reach `NeverRecovered` | none | a caller left unknown reaches done |
| reach `NeverStaleEpochRefused` | none | an old-epoch op after the clear is refused |
| reach `NeverNeedsReady` | none | s9 passes NeedsLanded |
| reach `NeverNewEpochOp` | none | k2 is accepted in epoch 2 |
| reach `NeverWidthFull` | none | a row fills to its width |
| reach `NeverWidthLowered` | none | e1 lowers r1's width |
| reach `NeverWidthRefused` | none | e1 is refused by the width rule |
| reach `NeverInOpWidthRefused` | none | s10 is refused on its second holder write |
| reach `NeverProducerRefused` | none | s5 is refused |
| reach `NeverSentinelRefused` | none | s6 is refused |
| reach `NeverHeldCarried` | none | r1's hold survives the clear |
| reach `NeverHeldRefused` | none | a1 is refused on RowHeld after g1 |
| reach `NeverQueuedPastWidth` | none | r1 holds a ready and a working consumer at width 1 |
| reach `NeverWorkEntryRefused` | none | b2 is refused by the work-entry check |
| reach `NeverRowFenced` | none | e1 is refused on a stale RowGen |
| reach `NeverSlotReused` | none | b2 goes working while the done u1 still holds r1 |

## Results, tla2tools v1.8.0 (the CI jar; it prints "TLC2 Version 2026.11.06.100558 (rev: 3ec1cc4)") on hetzner2, 2026-11-06

All 32 ran in one parallel pass with `run-tlc.sh`. `ci-tlc.sh` then passed
twice in a row in the same tree (32 ok, 0 FAIL, exit 0 both times). The
negative test (StoreNoEvent replaced by the fault-free model, and
StoreProducerHolds' cfg given WidthKept as its first invariant) failed
exactly those two and exited non-zero.

| Module | Result | States generated / distinct | Depth | Time |
|---|---|---|---|---|
| `Store` | No error has been found | 1035195 / 618638 | 24 | 01min 18s |
| `StoreEpochUnfenced` | Invariant OpsInTheirEpoch is violated | 92 / 85 | 15 | 03s |
| `StoreNoEvent` | Invariant LogMatchesState is violated | 2 / 2 | 2 | 02s |
| `StoreProducerHolds` | Invariant ProducerNeverHeld is violated | 6 / 6 | 6 | 02s |
| `StoreReplayApplies` | Invariant IdempotentById is violated | 32 / 32 | 15 | 03s |
| `StoreRowStale` | Invariant ConditionsAtomic is violated | 63 / 59 | 15 | 03s |
| `StoreSentinelHolds` | Invariant ProducerNeverHeld is violated | 7 / 7 | 7 | 02s |
| `StoreStaleCheck` | Invariant ConditionsAtomic is violated | 597 / 382 | 29 | 04s |
| `StoreTwoHolders` | Invariant SingleStateSingleHolder is violated | 13 / 13 | 13 | 03s |
| `StoreWidthCountsDone` | Invariant NoSpuriousRefusal is violated | 112 / 95 | 16 | 03s |
| `StoreWidthCountsReady` | Invariant NoSpuriousRefusal is violated | 14 / 14 | 14 | 03s |
| `StoreWidthPerOp` | Invariant WidthKept is violated | 11 / 11 | 11 | 02s |
| `StoreWidthStale` | Invariant WidthKept is violated | 544 / 342 | 29 | 04s |
| `StoreWidthUnchecked` | Invariant WidthKept is violated | 26 / 26 | 15 | 03s |
| `StoreWorkEntryUnchecked` | Invariant WidthKept is violated | 59 / 56 | 15 | 03s |
| reach `NeverHeldCarried` | violated | 74 / 69 | 15 | 03s |
| reach `NeverHeldRefused` | violated | 68 / 64 | 15 | 03s |
| reach `NeverInOpWidthRefused` | violated | 11 / 11 | 11 | 03s |
| reach `NeverNeedsReady` | violated | 10 / 10 | 10 | 03s |
| reach `NeverNewEpochOp` | violated | 93 / 86 | 15 | 03s |
| reach `NeverProducerRefused` | violated | 6 / 6 | 6 | 02s |
| reach `NeverQueuedPastWidth` | violated | 14 / 14 | 14 | 03s |
| reach `NeverRecovered` | violated | 32 / 32 | 15 | 03s |
| reach `NeverRowFenced` | violated | 72 / 67 | 15 | 03s |
| reach `NeverSentinelRefused` | violated | 7 / 7 | 7 | 02s |
| reach `NeverSlotReused` | violated | 646 / 507 | 17 | 03s |
| reach `NeverStaleEpochRefused` | violated | 86 / 80 | 15 | 03s |
| reach `NeverTaken` | violated | 14 / 14 | 14 | 03s |
| reach `NeverWidthFull` | violated | 14 / 14 | 14 | 03s |
| reach `NeverWidthLowered` | violated | 18 / 18 | 14 | 02s |
| reach `NeverWidthRefused` | violated | 26 / 26 | 15 | 03s |
| reach `NeverWorkEntryRefused` | violated | 119 / 102 | 16 | 03s |

The counterexamples were checked by hand (`show-all-traces.sh`,
`last-state.sh`); after setup:
- **StoreNoEvent:** s1 lands with no event.
- **StoreReplayApplies:** a1 is accepted, its reply is lost, and the retried
  Apply runs a1 again (refused, u1 exists) and logs it a second time.
- **StoreStaleCheck:** a1 begins with r1 not held, g1 holds r1, a1 lands.
- **StoreTwoHolders:** s12 moves u3 to r1 and it keeps r2 as well.
- **StoreEpochUnfenced:** k1 clears epoch 1, then the epoch-1 x1 is accepted
  in epoch 2.
- **StoreWidthStale:** a1 begins, e1 lowers r1's width to 0, a1 lands u1
  working on the stale width 1.
- **StoreWidthUnchecked:** a1 deals u1 working to r1, then e1 sets r1's
  width to 0.
- **StoreWidthPerOp:** s10 lands both working consumers on r2.
- **StoreWorkEntryUnchecked:** e1 sets r1's width to 0, then a1 holds u1 on
  r1 while waiting and makes it working.
- **StoreProducerHolds / StoreSentinelHolds:** s5 (producer p1) and s6
  (sentinel t1) are accepted.
- **StoreWidthCountsDone:** after a1 and a2 (u1 done on r1), e1's width 0 is
  refused.
- **StoreWidthCountsReady:** a1 is refused with only the ready u3 on r1.
- **StoreRowStale:** e1 lands, then g1 lands on RowGen(r1, 0) although e1
  raised r1's gen to 1.
