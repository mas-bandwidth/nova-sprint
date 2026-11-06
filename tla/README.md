# tla/: the store model (layer 1)

`Store.tla` models the contract in `docs/LAYER-1-STORE.md` (v2 at 77efc985;
the layer 1 text is unchanged at 25ab8e9d). Each operation is one atomic
step over `epoch`, `cards`, `rows`, `judgements`, `cursors`, `events` and
`applied`. In order, the step:
1. checks the op's epoch against the store's epoch;
2. checks its conditions;
3. runs its writes in order, each under its own rule;
4. either lands everything and appends an accepted event, or changes nothing
   and appends a refused event.

Epochs start at 1. `Clear` is an op with no writes and its epoch is checked
too. It opens the next epoch and carries the rows forward: same width, held
flag and tiers, state `down`, gen + 1. It leaves the cards, judgements and
cursors behind (all three are keyed by epoch). It is idempotent by id and
logged with the epoch it closed.

Cards have a kind: `producer`, `sentinel`, or a consumer (`work` or `read`).
A consumer names its producer (`prod`, the design's `of`), takes its
producer's tier at add, and has no needs. A consumer's holder is set at add
and never changes: there is no holder write. Add card refuses a holder on a
producer or a sentinel, and refuses a consumer without one.

Rows carry a width, a gen, a held flag, and the set of tiers they serve.
Judgements are `open`, `answered` or `expired`: put judgement opens one, and
answer and expire close it. A cursor holds a sequence number that set cursor
never moves back.

The conditions are:
- `CardGen`, `CardState`, `HolderIs` and `CardAbsent` on a card;
- `NeedsLanded`: every need of the card is landed, read from the store in the
  step;
- `RowIs`, `RowGen`, `RowHeld` and `RowServes` (the row serves tier T) on a
  row;
- `JudgementOpen` on a judgement: the fence that makes a close happen once.

There is no width condition. A row's width counts the consumers it holds in
`working`, never `ready`, `done` or `fail`, so a row may hold ready (queued)
consumers past its width. Width is kept by the write rules:
- **Set state into `working`** on a held card: refused when the holder row
  already holds width other working cards, counted at the moment of the
  write. So one op with two such writes to a width-1 row is refused on the
  second, and nothing lands.
- **Put row** and **set row width**: refused below the row's working
  holders.

An op that writes a card or a row raises its gen by one, however many of its
writes touch it. A card or row the op itself adds stays at gen 0. A row's
held flag and its tiers are set by their own writes (`SetRowHeld`,
`SetRowTiers`), which raise the row's gen.

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
read the rows, decide, and then write. Three witnesses are exactly that
interleaving:
- **StoreStaleCheck:** the conditions are judged on an earlier read, so a1
  deals to r1 after g1 held it.
- **StoreWidthStale:** the width check is judged on an earlier read, so a1
  lands a working consumer after e1 lowered the width to 0.
- **StoreServesStale:** RowServes is judged on an earlier read, so a1 deals
  a pro consumer to r1 after v1 made r1 serve flash only.

With the lock, writers run one at a time and each one reads the state the
previous one committed. That serial order is the order of `events` here.

## The instance

6 cards (producers p1, p2; sentinel t1; consumers u1, u2, u3), 2 rows of
width 1, one judgement j1, one cursor c1, 26 fixed ops. Callers issue their
ops in program order (`Pred`). A setup caller goes first, covering each
deterministic rule once:
- s1, s2 put r1 and r2, width 1, up, serving pro;
- s3 adds producer p1 with a holder, s4 sentinel t1 with a holder: both
  refused;
- s5 adds p1 (tier pro); s6 adds p2 (needs p1) and t1 (needs p2);
- s7 readies p2 before its need landed (refused), s8 lands p1, s9 readies
  p2;
- s10 adds u1 and u2 on the width-1 r2 and makes both working, in one op:
  refused on its second work entry;
- s11 adds u3 on r1 and readies it, where it stays queued;
- s12 opens judgement j1.

Then the callers of one scenario run concurrently. The constant `Scenario`
picks it. The two scenarios touch disjoint tables, so splitting them keeps
each run small without hiding an interleaving.

Scenario `deal` (`Store.cfg`):

| Caller | Ops | What it does |
|---|---|---|
| a | a1, a2 | a1 deals new consumer u1 working to r1, fenced on RowIs(r1, up), RowHeld(r1, false) and RowServes(r1, pro); a2 marks it done (it keeps r1, the slot frees) |
| b | b1, b2 | b1 adds u2 ready on r1; b2 makes it working (the work-entry check) |
| e | e1 | lowers r1's width to 0, fenced on RowGen(r1, 0) |
| g | g1 | holds r1, fenced on RowGen(r1, 0) |
| v | v1 | sets r1 to serve flash only, fenced only by its epoch |
| k | k1, k2 | k1 clears epoch 1; k2 adds p1 in epoch 2 |

Transport failures happen only on a1 and k1 (`Lossy`, `MaxLoss = 1`).

Scenario `records` (`StoreRecords.cfg`):

| Caller | Ops | What it does |
|---|---|---|
| h | h1 | answers j1, fenced on JudgementOpen(j1) |
| h' | h2 | answers j1 again, same fence |
| y | y1 | expires j1, same fence |
| m | m1 | sets c1 to 2 |
| m' | m2 | sets c1 to 1 |

Left out:
- streams, policy and the seq fence;
- the soft `after` list (it follows the needs rule; no store rule reads it);
- the fields no rule reads. On cards and rows: attempt, author, branch,
  base_sha, prior_head, head, pr, result, bounces, cost, priced, priority,
  brief, paths, done_when, prose, reason, score, model, harness, row kind,
  session, flaps. On judgements: kind, about, question, command, group_key,
  deadline, answer, actor and the times. They add columns, not behaviour.

## Files

- `Store.tla`, `Store.cfg`, `StoreRecords.cfg`: the model and its two
  passing runs.
- `witnesses/`: sixteen reversed witnesses, each `Store.tla` with one fault.
  They are generated: `python3 make-witnesses.py` writes every witness and
  every cfg (`Store.cfg`, `StoreRecords.cfg`, `witnesses/*.cfg`,
  `reach/*.cfg`) from `Store.tla` and the fault list in the script. Each
  fault is a set of exact text replacements; one that no longer matches
  stops the script. Edit `Store.tla`, rerun the script, commit both.
- `reach/`: twenty-one reachability checks on `Store.tla`. Each must be
  violated, which shows the passing runs are not vacuous.

In each witness cfg the target invariant is listed first: when one state
breaks several invariants, TLC names the first in the list. StoreWidthStale
leaves NoSpuriousRefusal out (its cfg says why): a stale count also refuses a
deal the live state allows, which TLC finds before the over-fill.

`StoreTwoHolders` is retired. It modelled the holder as a set so that a set
holder write could add a second row. With the holder fixed at add and no
holder write, there is no write that could give a card a second holder:
SingleStateSingleHolder holds by construction. A second consumer taking the
same slot is not a holder fault but a width one, and is covered by
StoreWorkEntryUnchecked, StoreWidthPerOp and StoreWidthStale.

## Running

Run TLC on a bench host, never on the working machine. `ci-tlc.sh` is the
gate. It runs every module in parallel:
- `Store.tla` must pass with `Store.cfg` and with `StoreRecords.cfg` (TLC
  exit 0);
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
- **ConsumersWellFormed**: a consumer names an existing producer, has no
  needs, has a holder, and has its producer's tier; a producer or sentinel
  names none.
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
- **ServesHeld**: an accepted op whose plan carried RowServes(R, T) had R
  serving T at that moment. This is a named special case of
  ConditionsAtomic, so a stale tier read is reported as such.
- **NoChangeOnRefusal**: a refused op changed nothing.
- **NoSpuriousRefusal**: every refusal has a cause in the spec (the epoch, a
  condition, or a write rule as the design states it), so a store rule
  stricter than the design is caught.
- **GenPerOp**: each accepted op raises the gen of each card and row it
  writes that existed before it by exactly one; one it creates is at 0; a
  clear raises every carried row's gen by one.
- **ClearCarriesRows**: a clear keeps every row's width, held flag and
  tiers, sets it down, and leaves no card, judgement or cursor.
- **JudgementClosedOnce**: within an epoch, at most one accepted op answers
  or expires a given judgement: no second answer, and never both answered
  and expired.
- **CursorMonotone**: no accepted op moves a cursor back.
- **ReceiptMatches**: a caller that thinks it knows the outcome holds exactly
  the stored receipt.

## Expected results

| Module | Fault | Expected |
|---|---|---|
| `Store` (deal) | none | pass |
| `Store` (records) | none | pass |
| `StoreNoEvent` | an accepted op lands without its event | LogMatchesState violated |
| `StoreReplayApplies` | Replay runs the op again and logs it again | IdempotentById violated |
| `StoreStaleCheck` | conditions judged on a snapshot read before another op's commit (no lock) | ConditionsAtomic violated |
| `StoreEpochUnfenced` | ops other than clear are not epoch-checked | OpsInTheirEpoch violated |
| `StoreWidthStale` | width check judged on a snapshot (no lock) | WidthKept violated |
| `StoreWidthUnchecked` | put row and set row width accept any width | WidthKept violated |
| `StoreWidthPerOp` | the work-entry check counts as at the start of the op | WidthKept violated |
| `StoreWorkEntryUnchecked` | a state write into working is not width-checked | WidthKept violated |
| `StoreProducerHolds` | add card accepts a holder on a producer or sentinel | ProducerNeverHeld violated |
| `StoreSentinelHolds` | add card refuses a holder on a producer but not a sentinel | ProducerNeverHeld violated |
| `StoreWidthCountsDone` | done and fail holders are counted | NoSpuriousRefusal violated |
| `StoreWidthCountsReady` | ready holders are counted | NoSpuriousRefusal violated |
| `StoreRowStale` | RowGen is not checked | ConditionsAtomic violated |
| `StoreServesStale` | RowServes judged on a snapshot (no lock) | ServesHeld violated |
| `StoreJudgementTwice` | JudgementOpen is not checked | JudgementClosedOnce violated |
| `StoreCursorBack` | set cursor accepts a lower value | CursorMonotone violated |
| reach `NeverTaken` | none | a1 is accepted |
| reach `NeverRecovered` | none | a caller left unknown reaches done |
| reach `NeverStaleEpochRefused` | none | an old-epoch op after the clear is refused |
| reach `NeverNeedsReady` | none | s9 passes NeedsLanded |
| reach `NeverNewEpochOp` | none | k2 is accepted in epoch 2 |
| reach `NeverWidthFull` | none | a row fills to its width |
| reach `NeverWidthLowered` | none | e1 lowers r1's width |
| reach `NeverWidthRefused` | none | e1 is refused by the width rule |
| reach `NeverInOpWidthRefused` | none | s10 is refused on its second work entry |
| reach `NeverProducerRefused` | none | s3 is refused |
| reach `NeverSentinelRefused` | none | s4 is refused |
| reach `NeverHeldCarried` | none | r1's hold survives the clear |
| reach `NeverHeldRefused` | none | a1 is refused on RowHeld after g1 |
| reach `NeverServesRefused` | none | a1 is refused on RowServes after v1 |
| reach `NeverQueuedPastWidth` | none | r1 holds a ready and a working consumer at width 1 |
| reach `NeverWorkEntryRefused` | none | b2 is refused by the work-entry check |
| reach `NeverRowFenced` | none | e1 is refused on a stale RowGen |
| reach `NeverSlotReused` | none | b2 goes working while the done u1 still holds r1 |
| reach `NeverAnswered` (records) | none | j1 is answered |
| reach `NeverJudgementFenced` (records) | none | a second close of j1 is refused on JudgementOpen |
| reach `NeverCursorRefused` (records) | none | m2 is refused below c1's value |

## Results, tla2tools v1.8.0 (the CI jar; it prints "TLC2 Version 2026.11.06.100558 (rev: 3ec1cc4)") on hetzner2, 2026-11-06

All 39 ran in one parallel pass with `run-tlc.sh`. The numbers below are
from `summarize-logs.sh` on those logs. `ci-tlc.sh` then passed
twice in a row in the same tree (39 ok, 0 FAIL, exit 0 both times). The
negative test (StoreNoEvent replaced by the fault-free model, and
StoreProducerHolds' cfg given WidthKept as its first invariant) failed
exactly those two and exited non-zero.

| Module | Result | States generated / distinct | Depth | Time |
|---|---|---|---|---|
| `Store` | No error has been found | 1035195 / 618638 | 24 | 01min 44s |
| `StoreRecords` | No error has been found | 458 / 338 | 18 | 05s |
| `StoreCursorBack` | Invariant CursorMonotone is violated | 34 / 34 | 15 | 05s |
| `StoreEpochUnfenced` | Invariant OpsInTheirEpoch is violated | 92 / 85 | 15 | 05s |
| `StoreJudgementTwice` | Invariant JudgementClosedOnce is violated | 19 / 19 | 15 | 04s |
| `StoreNoEvent` | Invariant LogMatchesState is violated | 2 / 2 | 2 | 03s |
| `StoreProducerHolds` | Invariant ProducerNeverHeld is violated | 4 / 4 | 4 | 03s |
| `StoreReplayApplies` | Invariant IdempotentById is violated | 32 / 32 | 15 | 05s |
| `StoreRowStale` | Invariant ConditionsAtomic is violated | 63 / 59 | 15 | 05s |
| `StoreSentinelHolds` | Invariant ProducerNeverHeld is violated | 5 / 5 | 5 | 04s |
| `StoreServesStale` | Invariant ServesHeld is violated | 642 / 415 | 29 | 06s |
| `StoreStaleCheck` | Invariant ConditionsAtomic is violated | 597 / 382 | 29 | 06s |
| `StoreWidthCountsDone` | Invariant NoSpuriousRefusal is violated | 112 / 95 | 16 | 05s |
| `StoreWidthCountsReady` | Invariant NoSpuriousRefusal is violated | 14 / 14 | 14 | 05s |
| `StoreWidthPerOp` | Invariant WidthKept is violated | 11 / 11 | 11 | 05s |
| `StoreWidthStale` | Invariant WidthKept is violated | 544 / 342 | 29 | 06s |
| `StoreWidthUnchecked` | Invariant WidthKept is violated | 26 / 26 | 15 | 05s |
| `StoreWorkEntryUnchecked` | Invariant WidthKept is violated | 11 / 11 | 11 | 03s |
| reach `NeverAnswered` | violated | 14 / 14 | 14 | 05s |
| reach `NeverCursorRefused` | violated | 34 / 34 | 15 | 05s |
| reach `NeverHeldCarried` | violated | 74 / 69 | 15 | 05s |
| reach `NeverHeldRefused` | violated | 68 / 64 | 15 | 05s |
| reach `NeverInOpWidthRefused` | violated | 11 / 11 | 11 | 05s |
| reach `NeverJudgementFenced` | violated | 19 / 19 | 15 | 05s |
| reach `NeverNeedsReady` | violated | 10 / 10 | 10 | 05s |
| reach `NeverNewEpochOp` | violated | 93 / 86 | 15 | 05s |
| reach `NeverProducerRefused` | violated | 4 / 4 | 4 | 04s |
| reach `NeverQueuedPastWidth` | violated | 14 / 14 | 14 | 04s |
| reach `NeverRecovered` | violated | 32 / 32 | 15 | 05s |
| reach `NeverRowFenced` | violated | 72 / 67 | 15 | 05s |
| reach `NeverSentinelRefused` | violated | 5 / 5 | 5 | 03s |
| reach `NeverServesRefused` | violated | 77 / 72 | 15 | 04s |
| reach `NeverSlotReused` | violated | 646 / 507 | 17 | 05s |
| reach `NeverStaleEpochRefused` | violated | 86 / 80 | 15 | 05s |
| reach `NeverTaken` | violated | 14 / 14 | 14 | 04s |
| reach `NeverWidthFull` | violated | 14 / 14 | 14 | 05s |
| reach `NeverWidthLowered` | violated | 18 / 18 | 14 | 05s |
| reach `NeverWidthRefused` | violated | 26 / 26 | 15 | 05s |
| reach `NeverWorkEntryRefused` | violated | 119 / 102 | 16 | 04s |

The counterexamples were checked by hand with `show-all-traces.sh`. After
setup:
- **StoreNoEvent:** s1 lands with no event.
- **StoreReplayApplies:** a1 is accepted, its reply is lost, and the retried
  Apply runs a1 again (refused, u1 exists) and logs it a second time.
- **StoreStaleCheck:** a1 begins with r1 not held, g1 holds r1, a1 lands.
- **StoreEpochUnfenced:** k1 clears epoch 1, then the epoch-1 v1 is accepted
  in epoch 2.
- **StoreWidthStale:** a1 begins, e1 lowers r1's width to 0, a1 lands u1
  working on the stale width 1.
- **StoreWidthUnchecked:** a1 deals u1 working to r1, then e1 sets r1's
  width to 0.
- **StoreWidthPerOp / StoreWorkEntryUnchecked:** s10 (in setup) lands both
  working consumers on r2.
- **StoreProducerHolds / StoreSentinelHolds:** s3 (producer p1 held by r1)
  and s4 (sentinel t1 held by r1) are accepted.
- **StoreWidthCountsDone:** after a1 and a2 (u1 done on r1), e1's width 0 is
  refused.
- **StoreWidthCountsReady:** a1 is refused with only the ready u3 on r1.
- **StoreRowStale:** e1 lands, then g1 lands on RowGen(r1, 0) although e1
  raised r1's gen to 1.
- **StoreServesStale:** a1 begins while r1 serves pro, v1 sets r1 to flash,
  a1 lands.
- **StoreJudgementTwice:** h1 answers j1, then h2 answers it again.
- **StoreCursorBack:** m1 sets c1 to 2, then m2 sets it to 1.
