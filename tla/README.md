# tla/: the store model (layer 1), the card machine (layer 2) and the steps (layer 3)

Three models live here: `Store.tla` (layer 1, below), `CardMachine.tla`
(layer 2, in its own section after the store's results) and `Steps.tla`
(layer 3, last). `ci-tlc.sh` runs all three, with all their witnesses and
reach checks, in one parallel pass.

`Store.tla` models the contract in `docs/LAYER-1-STORE.md` (v2 at 3c9b5155).
Each operation is one atomic step over `epoch`, `cards`, `rows`,
`judgements`, `cursors`, `policies`, `events` and `applied`. In order, the step:
1. checks the op's epoch against the store's epoch;
2. checks its conditions;
3. runs its writes in order, each under its own rule;
4. either lands everything and appends an accepted event, or changes nothing
   and appends a refused event.

Epochs start at 1. `Clear` is an op with no writes and its epoch is checked
too. It opens the next epoch and carries the rows forward: same width, held
flag and tiers, state `down`, gen + 1. It keeps the cursors (global, as the
log's seq is, so a subscriber resumes where it was) and leaves the cards and
judgements behind (keyed by epoch). It is idempotent by id and logged with
the epoch it closed. The event log is append-only: row triggers refuse
UPDATE and DELETE on events, and a statement trigger refuses TRUNCATE.

Policy versions are global too. Put policy adds the next version; a
version's body never changes once put and the version is never removed (a
trigger; only `active` may change, and `active` is left out of the model).

Every set write that would change nothing is refused ("row r1 is already
up", "cursor c1 is already at 2"): the op is refused, its refused event is
logged, and no gen moves. In the model that covers every set write it has:
card state, row width, row state, row held, row tiers and cursor. Streams
(set stream held), policy activation and the set-card writes on fields no
rule reads are left out, so their same-value refusals are not modelled.

Tiers and needs are TLA+ sets, so a reorder is the same value by
construction (the store keeps them sorted without repeats), and a tiers
write equal to the row's set is refused like any other same-value write. An
empty name is `ErrInvalid` before the store's step and is never logged, so
it never reaches the model: structural.

Cards have a kind: `producer`, `sentinel`, or a consumer (`work` or `read`).
A consumer names its producer (`prod`, the design's `of`), takes its
producer's tier at add, and has no needs. A consumer's holder is set at add
and never changes: there is no holder write. Add card refuses a holder on a
producer or a sentinel, and refuses a consumer without one.

Rows carry a width, a gen, a held flag, and the set of tiers they serve.
Tier serving is kept by the writes, with no condition: put card refuses a
consumer whose holder row does not serve its tier, as does a move into
`ready` or `working`, and put row and set row tiers refuse to drop a tier
that a live (`ready` or `working`) consumer of the row needs. A waiting
consumer does not hold its tier, so a row may drop it, and the consumer's
move into ready is then refused.

Judgements are `open`, `answered` or `expired`: put judgement opens one (refused
if it exists), and answer and expire close it. A close is refused inside the
write unless the judgement is open, so it closes once whatever the plan
carried; `JudgementOpen` still fences a plan. A cursor holds a seq that set
cursor never moves back and never moves past the last event's seq (events
are numbered from 1, refused ones included); an absent cursor reads as 0.

The conditions are:
- `CardGen`, `CardState`, `HolderIs` and `CardAbsent` on a card;
- `NeedsLanded`: every need of the card is landed, read from the store in the
  step;
- `RowIs`, `RowGen` and `RowHeld` on a row;
- `JudgementOpen` on a judgement.

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

`ledger` is a ghost: every event as it was committed. The store appends to
`events` and `ledger` together and never changes `events` otherwise, so
LogAppendOnly (`events = ledger`) holds by construction here; the witness
StoreLogTampered adds the UPDATE the trigger refuses. TRUNCATE (on events and
on policy) has no action in the model: the statement triggers make it
structural, as UPDATE and DELETE are.

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

7 cards (producers p1, p2; sentinel t1; consumers u1, u2, u3, u4), 2 rows
of width 1, one judgement j1, one cursor c1, two policy versions, 35
fixed ops. Callers issue their
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
- s12 opens judgement j1;
- s13 adds consumer u4 (pro) waiting on r2; s14 makes r2 serve flash only
  (accepted: u4 is not live); s15 readies u4: refused, r2 does not serve
  pro; s16 adds u1 on r2: refused by put card for the same reason;
- s17 sets r2's width to 1, which it already is: refused as changing
  nothing;
- s18 and s19 put policy versions 1 and 2;
- s20 sets r2's tiers to the set it serves: refused as changing nothing.

Then the callers of one scenario run concurrently. The constant `Scenario`
picks it. The two scenarios touch disjoint tables, so splitting them keeps
each run small without hiding an interleaving.

Scenario `deal` (`Store.cfg`):

| Caller | Ops | What it does |
|---|---|---|
| a | a1, a2 | a1 deals new consumer u1 working to r1, fenced on RowIs(r1, up) and RowHeld(r1, false); a2 marks it done (it keeps r1, the slot frees) |
| b | b1, b2 | b1 adds u2 ready on r1; b2 makes it working (the work-entry check) |
| e | e1 | lowers r1's width to 0, fenced on RowGen(r1, 0) |
| g | g1 | holds r1, fenced on RowGen(r1, 0) |
| v | v1 | sets r1 to serve flash only, fenced only by its epoch: refused in epoch 1, where the pro consumer u3 is ready on r1 |
| k | k1, k2 | k1 clears epoch 1; k2 adds p1 in epoch 2 |

Transport failures happen only on a1 and k1 (`Lossy`, `MaxLoss = 1`).

Scenario `records` (`StoreRecords.cfg`):

| Caller | Ops | What it does |
|---|---|---|
| h | h1 | answers j1, fenced on JudgementOpen(j1) |
| h' | h2 | answers j1 with no fence: the write itself refuses a second close |
| y | y1 | expires j1, fenced on JudgementOpen(j1) |
| m | m1 | sets c1 to 2 |
| m' | m2 | sets c1 to 1 |
| m'' | m3 | sets c1 to 40, past any log this instance writes |
| k | k1 | clears epoch 1 (c1 is kept) |

Left out:
- streams, policy and the seq fence;
- op times, and so the refusal of a close timed before `opened_at`;
- the soft `after` list (it follows the needs rule; no store rule reads it);
- the fields no rule reads. On cards and rows: attempt, author, branch,
  base_sha, prior_head, head, pr, result, bounces, cost, priced, priority,
  brief, paths, done_when, prose, reason, score, model, harness, row kind,
  session, flaps. On judgements: kind, about, question, command, group_key,
  deadline, answer, actor and the times. They add columns, not behaviour.

## Files

- `Store.tla`, `Store.cfg`, `StoreRecords.cfg`: the model and its two
  passing runs.
- `witnesses/`: twenty reversed witnesses, each `Store.tla` with one fault.
  They are generated: `python3 make-witnesses.py` writes every witness and
  every cfg (`Store.cfg`, `StoreRecords.cfg`, `witnesses/*.cfg`,
  `reach/*.cfg`) from `Store.tla` and the fault list in the script. Each
  fault is a set of exact text replacements; one that no longer matches
  stops the script. Edit `Store.tla`, rerun the script, commit both.
- `reach/`: twenty-nine reachability checks on `Store.tla`. Each must be
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

`StoreServesStale` is retired with the RowServes condition: serving is now
read by the writes inside the transaction, and the tier witnesses are
StoreServesUnchecked and StoreTiersNarrowed.

## Running

Run TLC on a bench host, never on the working machine. `ci-tlc.sh` is the
gate. It runs every module in parallel:
- `Store.tla` must pass with `Store.cfg` and with `StoreRecords.cfg` (TLC
  exit 0);
- `CardMachine.tla` must pass with `CardMachine.cfg`, `CardDeps.cfg` and
  `CardPreempt.cfg` (its three scenarios);
- every `witnesses/*.tla` and every `reach/*.cfg` must report a violation
  (TLC exit 12) of the first invariant in its cfg. `reach/Card*.cfg` run on
  `CardMachine.tla`, every other reach cfg on `Store.tla`.

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
- **ServesHeld**: in every state, every live (`ready` or `working`)
  consumer's row serves its tier.
- **NoChangeOnRefusal**: a refused op changed nothing.
- **NoSpuriousRefusal**: every refusal has a cause in the spec (the epoch, a
  condition, or a write rule as the design states it), so a store rule
  stricter than the design is caught.
- **GenPerOp**: each accepted op raises the gen of each card and row it
  writes that existed before it by exactly one; one it creates is at 0; a
  clear raises every carried row's gen by one.
- **ClearCarriesRows**: a clear keeps every row's width, held flag and
  tiers, sets it down, leaves no card or judgement, and keeps every cursor.
- **JudgementClosedOnce**: within an epoch, at most one accepted op answers
  or expires a given judgement: no second answer, and never both answered
  and expired.
- **CursorMonotone**: no accepted op, a clear included, moves a cursor back.
- **CursorWithinLog**: no cursor is past the seq of the last event.
- **LogAppendOnly**: the events table is exactly what was committed, in
  order (`events = ledger`): no event was updated or deleted.
- **PolicyImmutable**: a policy version, once put, keeps its body; no op
  (a clear included) changes or removes it.
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
| `StoreServesUnchecked` | no serving check at put card or on the move into ready/working | ServesHeld violated |
| `StoreTiersNarrowed` | put row and set row tiers may drop a tier a live consumer needs | ServesHeld violated |
| `StoreJudgementTwice` | answer and expire refuse only an absent judgement | JudgementClosedOnce violated |
| `StoreCursorBack` | set cursor accepts a lower value | CursorMonotone violated |
| `StoreCursorPastLog` | set cursor is not bounded by the log | CursorWithinLog violated |
| `StoreLogTampered` | an UPDATE on events (no append-only trigger) | LogAppendOnly violated |
| `StorePolicyRewritten` | put policy writes the latest version, not the next | PolicyImmutable violated |
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
| reach `NeverReadyServesRefused` | none | s15 is refused: r2 no longer serves u4's tier |
| reach `NeverAddServesRefused` | none | s16 is refused by put card: r2 does not serve pro |
| reach `NeverTiersRefused` | none | v1 is refused: the ready u3 needs pro on r1 |
| reach `NeverSameValueRefused` | none | s17 is refused as changing nothing |
| reach `NeverTwoPolicies` | none | s19 adds policy version 2 |
| reach `NeverSameTiersRefused` | none | s20 is refused as changing nothing |
| reach `NeverQueuedPastWidth` | none | r1 holds a ready and a working consumer at width 1 |
| reach `NeverWorkEntryRefused` | none | b2 is refused by the work-entry check |
| reach `NeverRowFenced` | none | e1 is refused on a stale RowGen |
| reach `NeverSlotReused` | none | b2 goes working while the done u1 still holds r1 |
| reach `NeverAnswered` (records) | none | j1 is answered |
| reach `NeverJudgementFenced` (records) | none | a second fenced close of j1 is refused on JudgementOpen |
| reach `NeverCursorRefused` (records) | none | m2 is refused below c1's value |
| reach `NeverCloseRefused` (records) | none | the unfenced h2 is refused by the write after j1 closed |
| reach `NeverCursorPastRefused` (records) | none | m3 is refused past the log |
| reach `NeverCursorCarried` (records) | none | c1 keeps its value across the clear |

## Results, tla2tools v1.8.0 (the CI jar; it prints "TLC2 Version 2026.11.06.100558 (rev: 3ec1cc4)") on hetzner2, 2026-11-06

All 51 ran in one parallel pass with `run-tlc.sh`. The numbers below are
from `summarize-logs.sh` on those logs. `ci-tlc.sh` then passed twice in a
row in the same tree (51 ok, 0 FAIL, exit 0 both times). The negative
test (StoreNoEvent replaced by the fault-free model, and
StoreProducerHolds' cfg given WidthKept as its first invariant) failed
exactly those two and exited non-zero.

| Module | Result | States generated / distinct | Depth | Time |
|---|---|---|---|---|
| `Store` | No error has been found | 1035203 / 618646 | 32 | 04min 48s |
| `StoreRecords` | No error has been found | 74685 / 39163 | 29 | 31s |
| `StoreCursorBack` | Invariant CursorMonotone is violated | 82 / 76 | 23 | 10s |
| `StoreCursorPastLog` | Invariant CursorWithinLog is violated | 30 / 30 | 22 | 08s |
| `StoreEpochUnfenced` | Invariant OpsInTheirEpoch is violated | 100 / 93 | 23 | 10s |
| `StoreJudgementTwice` | Invariant JudgementClosedOnce is violated | 55 / 52 | 23 | 09s |
| `StoreLogTampered` | Invariant LogAppendOnly is violated | 4 / 4 | 3 | 05s |
| `StoreNoEvent` | Invariant LogMatchesState is violated | 2 / 2 | 2 | 04s |
| `StorePolicyRewritten` | Invariant PolicyImmutable is violated | 20 / 20 | 20 | 08s |
| `StoreProducerHolds` | Invariant ProducerNeverHeld is violated | 4 / 4 | 4 | 06s |
| `StoreReplayApplies` | Invariant IdempotentById is violated | 40 / 40 | 23 | 09s |
| `StoreRowStale` | Invariant ConditionsAtomic is violated | 71 / 67 | 23 | 10s |
| `StoreSentinelHolds` | Invariant ProducerNeverHeld is violated | 5 / 5 | 5 | 06s |
| `StoreServesUnchecked` | Invariant ServesHeld is violated | 16 / 16 | 16 | 08s |
| `StoreStaleCheck` | Invariant ConditionsAtomic is violated | 613 / 393 | 45 | 11s |
| `StoreTiersNarrowed` | Invariant ServesHeld is violated | 28 / 28 | 22 | 09s |
| `StoreWidthCountsDone` | Invariant NoSpuriousRefusal is violated | 120 / 103 | 24 | 10s |
| `StoreWidthCountsReady` | Invariant NoSpuriousRefusal is violated | 22 / 22 | 22 | 09s |
| `StoreWidthPerOp` | Invariant WidthKept is violated | 11 / 11 | 11 | 07s |
| `StoreWidthStale` | Invariant WidthKept is violated | 560 / 353 | 45 | 11s |
| `StoreWidthUnchecked` | Invariant WidthKept is violated | 34 / 34 | 23 | 10s |
| `StoreWorkEntryUnchecked` | Invariant WidthKept is violated | 11 / 11 | 11 | 07s |
| reach `NeverAddServesRefused` | violated | 17 / 17 | 17 | 08s |
| reach `NeverAnswered` | violated | 25 / 25 | 22 | 08s |
| reach `NeverCloseRefused` | violated | 55 / 52 | 23 | 09s |
| reach `NeverCursorCarried` | violated | 76 / 71 | 23 | 09s |
| reach `NeverCursorPastRefused` | violated | 30 / 30 | 22 | 08s |
| reach `NeverCursorRefused` | violated | 82 / 76 | 23 | 09s |
| reach `NeverHeldCarried` | violated | 82 / 77 | 23 | 09s |
| reach `NeverHeldRefused` | violated | 76 / 72 | 23 | 10s |
| reach `NeverInOpWidthRefused` | violated | 11 / 11 | 11 | 07s |
| reach `NeverJudgementFenced` | violated | 56 / 53 | 23 | 09s |
| reach `NeverNeedsReady` | violated | 10 / 10 | 10 | 08s |
| reach `NeverNewEpochOp` | violated | 101 / 94 | 23 | 09s |
| reach `NeverProducerRefused` | violated | 4 / 4 | 4 | 05s |
| reach `NeverQueuedPastWidth` | violated | 22 / 22 | 22 | 09s |
| reach `NeverReadyServesRefused` | violated | 16 / 16 | 16 | 09s |
| reach `NeverRecovered` | violated | 40 / 40 | 23 | 09s |
| reach `NeverRowFenced` | violated | 80 / 75 | 23 | 08s |
| reach `NeverSameTiersRefused` | violated | 21 / 21 | 21 | 09s |
| reach `NeverSameValueRefused` | violated | 18 / 18 | 18 | 09s |
| reach `NeverSentinelRefused` | violated | 5 / 5 | 5 | 06s |
| reach `NeverSlotReused` | violated | 654 / 515 | 25 | 10s |
| reach `NeverStaleEpochRefused` | violated | 94 / 88 | 23 | 10s |
| reach `NeverTaken` | violated | 22 / 22 | 22 | 09s |
| reach `NeverTiersRefused` | violated | 28 / 28 | 22 | 09s |
| reach `NeverTwoPolicies` | violated | 20 / 20 | 20 | 09s |
| reach `NeverWidthFull` | violated | 22 / 22 | 22 | 08s |
| reach `NeverWidthLowered` | violated | 26 / 26 | 22 | 09s |
| reach `NeverWidthRefused` | violated | 34 / 34 | 23 | 09s |
| reach `NeverWorkEntryRefused` | violated | 127 / 110 | 24 | 09s |

The counterexamples were checked by hand with `show-all-traces.sh`. After
setup:
- **StoreNoEvent:** s1 lands with no event.
- **StoreLogTampered:** s1's event is updated from accepted to refused.
- **StorePolicyRewritten:** s19 overwrites version 1's body (in setup).
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
- **StoreServesUnchecked:** s15 (in setup) readies u4 on r2, which serves
  flash only.
- **StoreTiersNarrowed:** v1 makes r1 serve flash only while the pro u3 is
  ready there.
- **StoreJudgementTwice:** h1 answers j1, then the unfenced h2 answers it
  again.
- **StoreCursorBack:** m1 sets c1 to 2, then m2 sets it to 1.
- **StoreCursorPastLog:** m3 sets c1 to 40 with 16 events in the log.

# The card machine (layer 2)

`CardMachine.tla` models `docs/LAYER-2-CARD-MACHINE.md` (v2 at 2b0fe77c,
with its sections "Rulings from the first cold read of layer 2", "Rulings
from the card model", "Rulings from the second cold read of layer 2" and,
at v2 3c9d3b89, "Rulings from the third cold read of layer 2 and the card
model"):
producer and consumer cards, every event of the producer, consumer and row
tables as an action, Add, AskReads, WaiveRed, UnholdStream, blocker
preemption, the stream's hold with Park in the same op, hard needs with
Waive and Relink, soft `after` (an order edge only; see round 4), and the
sentinel. An action's
guard is the design's from-state plus its conditions;
an event that does not match is not enabled (the Go code refuses it with a
value, which changes nothing). The model is the reachable state space of
accepted moves.

Consumers are slots: producer p's i-th cut consumer is `<<p, i>>`. Start and
AskRead cut the next slot, so a producer is never in `working` without its
consumer. Width is the store's write guard on Take: working consumers only,
never ready ones. A producer carries `review`, its count of entries into
review (Finished and Red raise it); a read carries the producer's `review` at
cut, and ReadYes needs a done ok read (ReadFix a done fix read) of the
current head whose `review` equals the producer's.

The rulings modelled: a Withdraw of the working producer's work consumer
releases the producer in the same step (Preempt's Withdraw too), so
Released and Block from working are kept as the tables have them but are
never enabled; Broken, Faulted, ReadFix, Released and that release write the
producer parked (waiting) when the stream is held; a work consumer reports
ok, fail or fault, a read ok, fix or fault; an ok work head is neither the
base nor the attempt's prior_head (the producer's head at Start); AskRead
refuses a row already holding a live read of this head; Replace names an
existing other card; a sentinel takes no Start, AskRead, Edit or Rescore,
and takes SetPriority and Relink; Relink's target is a producer (a
sentinel too since round 4); Take is
refused on a down row and allowed on a held one; preemption withdraws a
working consumer only and counts open rows (up, not held); Add puts a card
with needs and after (producers or sentinels: a sentinel may be a need),
refusing a cycle through either.

From the second cold read: AskReads cuts reads on several rows in one step,
each checked as AskRead checks it, into distinct consecutive slots;
WaiveRed moves a producer from review to merging with no new read when a
done ok read of the previous review at the same head exists, with no live
consumer; Start copies the producer's finding to the new work consumer;
Replace names a non-final producer or sentinel; UnholdStream is an event.

Round 4 (the third cold read, card/ at v2-card-fix3 4a085774): a Red is
recorded on the producer as a flag `red` (the producers-only card column
`red`; the code holds it in `pr` until the store has the column): Red sets
it, Finished, Start and Preempt's Start clear it, and WaiveRed requires it.
The prior-read check alone does not imply it: a Finished can come back to
an already-read head (Block and Unblock out of review, an attempt withdrawn
with another head, an attempt reporting the old head), and only the red
check refuses WaiveRed there (CardWaiveRedWithoutRed). `finding` is written
only by ReadFix: Broken and Faulted keep it (the code writes a return's
reason to `reason`, never `finding`). Relink takes a sentinel target as Add
does. Landed has no after check: a landing never holding on a soft after is
layer 3's landing step, so AfterNeverHolds and AfterKept are gone and
`after` is read only by the cycle checks. Merge is sequential composition:
the model's steps are already one plan each, so a merge is consecutive
steps and nothing changes; Preempt stays the one step (Withdraw and Start
on two cards) the code's plan is. HoldStream parks exactly the ready set
it lists; the model parks the whole ready set, the list layer 3 owes.
A preempt Withdraw requires the consumer working, as Preempt already did.
The reasons ("held: ...") are not modelled.

`last` is a step monitor. `StepRule` reads the state before and after each
step and records the step rules it broke (`last.bad`). It is shared by every
action and part of none, so a fault in an action is caught there; it keeps
only four event names (NeedFailed, Preempt, AskReads, InThisOp, for their
reach checks), so states
are not split by the move that reached them.

## The instance: four scenarios

| cfg | Scenario | producers | needs | K | heads | attempts | env moves |
|---|---|---|---|---|---|---|---|
| `CardMachine.cfg` | `life` | p1, sentinel s | s needs p1 | 3 | 2 | 2 | 2 |
| `CardDeps.cfg` | `deps` | p1, p2, sentinel s, sentinel a (absent until Add) | p2 needs p1, s needs p2 | 2 | 1 | 1 | 1 |
| `CardPreempt.cfg` | `pre` | p1, p2 | none; p2 `after` p1 | 2 | 1 | 1 | 2 |
| `CardReads.cfg` | `reads` | p1; rows r1, r2, r3 | none | 3 | 1 | 1 | 1 |
| `witnesses/CardWaiveRedWithoutRed.cfg` only | `redw` (Scenario life) | p1, sentinel s | s needs p1 | 5 | 2 | 3 | 0 |

- `life` carries the whole lifecycle: a fix, a Red with a yes after it, a
  fault re-dealt, the sentinel reached.
- `deps` is the brief's pair (c2 needs c1) plus the sentinel: NeedFailed,
  Waive, Relink, their InThisOp forms and the cycle check, and Add of a
  second sentinel. One environment move: with two, Add's choices took it
  past 6M states.
- `reads` has a third row, so AskReads has two rows that are not the author
  (with the third row in `deps` that run passed 24M states unfinished).
- A Red is a CI result, so it spends the environment budget; as a free move,
  Red and WaiveRed looped.
- `redw` is `life` sized for one witness: WaiveRed without its red check
  needs a third attempt (see round 4); it is never a passing run.
- `pre` exists because a blocker can only preempt the consumer of another
  producer live at the same time, which the `deps` pair never is (p2 waits
  for p1 to land). Its environment budget goes to SetPriority, the row table
  and the stream's hold; Edit and Rescore are checked in the other two.

Bounds that are the model's, not the design's: K consumer slots per
producer; attempt stops at the attempts column (Start is not enabled past
it); bounces stop at 1 (a work consumer does not report fail, nor a read
fix, at it, so a reported consumer is never stuck); flaps stop at 1; scores
are 0..1; widths 1..2; priority is normal or blocker. The environment and
coordinator moves (Add, Rescore, SetPriority, Edit, Seen, Gone, Width, Hold,
Unhold, the stream's hold) share a budget per behaviour; every
card and consumer move is unbounded but for the slots. Rows start up, width
1, not held. Head 0 is "no head" and the base commit, so an ok work head
always differs from base_sha.

Left out, because no guard reads them: branch, base_sha, reason, finding,
brief, prose, paths, done_when, cost, priced. Edit writes only prose, paths
and done_when, so in the model it moves nothing but the monitor.

The model is matched to card/ at v2-card-fix3 4a085774. Of the data
columns, these are read by a transition and so are model state: head,
prior_head (an ok or pushed work head, reported or withdrawn, is refused
when it equals the base or the prior head, and a ready work consumer
withdraws with none), review, author, red, finding (as "fix" or none:
ReadFix writes it, Start copies it; in the code it is the reader's text and
written only when non-empty and changed). These
are not: reason, brief, cost, priced (written, read by no guard); prose,
paths and done_when are read only by Edit's own "changes nothing" refusal,
which moves no other state, so Edit is a stutter here. HoldStream's AtSeq
fence is the atomic step itself: the model parks exactly the ready set of
the state it steps from, which is what a plan fenced at the snapshot's seq
can apply. NeedFailed, Waive and Relink {InThisOp} are one step together
with the Cancel of the need. Relink names a producer or a sentinel and walks
needs and after for a cycle, so the two cycle witnesses fault only the
cycle check.

## Files

- `CardMachine.tla` and its four passing cfgs.
- `witnesses/Card*.tla`: twenty-four reversed witnesses; `reach/Card*.cfg`:
  sixteen reachability checks. Generated with every card cfg by
  `python3 make-card-witnesses.py` from `CardMachine.tla` and the fault list
  in the script, in the same shape as `make-witnesses.py`: a replacement
  that no longer matches stops the script.
- `card-trace.py`: `python3 -I card-trace.py logs/<name>.log` prints a card
  counterexample one line per state (TLC names every step `<Next>`, so the
  move is read off the diff).

## Invariants

State invariants:
- **TypeOK**, **SlotsDense** (the cut slots are 1..ncut).
- **ProducerNeverHeld**: the holder lives on the consumer only (structural).
- **WorkingOneWorkConsumer**: at most one live work consumer; a live one is
  the current attempt's and its producer is working; a working producer has
  its current attempt's work consumer, never withdrawn (the invariant as the
  tables have it, per the card model's ruling).
- **WorkingLeavesInOneStep**: a working producer with no live consumer (its
  consumer reported) has a leaving move enabled: Finished, Broken, Faulted,
  Released or Block.
- **ParkedUnderHold**: no producer is ready in a held stream after any step.
- **NeverStranded**: every non-final producer with no live consumer has a
  move of its own enabled (not counting the coordinator's Cancel, Replace,
  Block, Edit, Rescore, SetPriority), or waits on a need that can still
  move, an after-target that is merging, or (waiting, ready, review) a row
  coming back or a hold lifting. A ready producer at the attempt bound, or
  one with no slot left, is exempt as the model's limit.
- **ReadOnlyInReview**: a live read only in review, of the current head and
  review count.
- **ReaderNeverAuthor**: a live read's holder is never the producer's author.
- **WidthKept**: working consumers per row ≤ width.
- **TakeBelowWidth**: a ready consumer can always take while its row is up
  (held or not) and its working count is below width (ready ones are not
  counted).
- **NeedFailedTakesDependants**: when a need has failed and the dependant is
  not final, NeedFailed is enabled.
- **SentinelShape**: a sentinel is only absent (before Add), waiting, landed
  or fail (it never has a consumer).
- **NeedsAcyclic**: the needs graph stays acyclic.
- **OrderAcyclic**: needs and after together stay acyclic.
- **ResultsByKind**: a work consumer's result is ok, fail or fault; a read's
  ok, fix or fault.
- **OneLiveReadPerRow**: at most one live read of a head per row.

Step invariants (each: the last step kept its rule):
- **FinalNeverMoves**: no landed/fail producer and no done/fail consumer
  changes.
- **AttemptOnlyOnStart**: attempt rises by one on Start (and Preempt's Start)
  of that producer, and on nothing else.
- **RedNeverAdvances**: a Red step never ends in merging or landed.
- **FixKeepsScore**: ReadFix keeps the score.
- **HolderNeverChanges**: a cut consumer's holder never changes.
- **OldAttemptNeverMoves**: Finished, Broken, Faulted and Released accept only
  the work consumer of the current attempt.
- **OnlyHolderActs**: Take and Report only from the consumer's holder.
- **ReadYesNeedsYes**: ReadYes only with a done ok read of the current head
  whose review equals the producer's.
- **ReadFixNeedsFix**: ReadFix only with a done fix read of the same kind.
- **WithdrawReleasesInOneStep**: a Withdraw (or Preempt) of the working
  producer's work consumer leaves the producer ready, or waiting under a
  hold, in that same step.
- **WaiveRedNeedsPriorRead**: WaiveRed only with a done ok read of the
  previous review at the current head.
- **FixCarriesFailure**: Broken and Faulted keep the producer's finding (a
  fault never writes it), and Start (and Preempt's Start) copies the finding
  to the new work consumer.
- **ReplaceByLive**: Replace names a non-final card other than itself.
- **HeadNeverRepeats**: a work consumer's reported or withdrawn head is
  never its prior head.
- **FaultSpendsNothing**: Faulted keeps attempt and bounces.
- **BlockNoLive**, **EditNoLive**: Block and Edit only with no live consumer.
- **SentinelLandsByReached**: a sentinel lands only by Reached, needs landed.
- **WaiveRedNeedsRed**: WaiveRed only with the Red recorded.

## Expected results

| Module | Scenario | Fault | Expected result |
|---|---|---|---|
| `CardMachine` / `CardDeps` / `CardPreempt` / `CardReads` | life / deps / pre / reads | none | pass |
| `witnesses/CardLandedMoves` | life | Rescore has no final check | FinalNeverMoves |
| `witnesses/CardAuthorReads` | life | AskRead does not refuse the author | ReaderNeverAuthor |
| `witnesses/CardRedAdvances` | life | Red moves merging to landed | RedNeverAdvances |
| `witnesses/CardNeedFailedIgnored` | deps | NeedFailed reads the dependant's state for the need's | NeedFailedTakesDependants |
| `witnesses/CardNonHolderReports` | life | Report does not check the holder | OnlyHolderActs |
| `witnesses/CardTwoWorkConsumers` | life | Start accepted from working | WorkingOneWorkConsumer |
| `witnesses/CardStaleReport` | life | Faulted accepts any work consumer of p | OldAttemptNeverMoves |
| `witnesses/CardYesOnZero` | life | ReadYes without the yes-read check | ReadYesNeedsYes |
| `witnesses/CardFaultBumpsAttempt` | life | Faulted raises attempt | FaultSpendsNothing |
| `witnesses/CardFaultBounces` | life | Faulted raises bounces (a fault counted toward Block) | FaultSpendsNothing |
| `witnesses/CardEditUnderLive` | life | Edit under a live consumer | EditNoLive |
| `witnesses/CardReadyCounted` | life | Take counts ready consumers against width | TakeBelowWidth |
| `witnesses/CardNeedsCycle` | deps | Relink without the cycle check | NeedsAcyclic |
| `witnesses/CardWithdrawLeavesWorking` | life | Withdraw does not release the working producer | WithdrawReleasesInOneStep |
| `witnesses/CardReadFixOnZero` | life | ReadFix without the fix-read check | ReadFixNeedsFix |
| `witnesses/CardHeldStreamReady` | life | Broken returns to ready under a held stream | ParkedUnderHold |
| `witnesses/CardWaiveRedWithoutRead` | life | WaiveRed without the prior-read check | WaiveRedNeedsPriorRead |
| `witnesses/CardRelinkAfterCycle` | deps | Relink walks needs only | OrderAcyclic |
| `witnesses/CardReadReportsFail` | life | a read may report fail | ResultsByKind |
| `witnesses/CardReplaceByFinal` | life | Replace by a final card | ReplaceByLive |
| `witnesses/CardTwoReadsOneRow` | life | AskRead takes a row holding a live read of the head | OneLiveReadPerRow |
| `witnesses/CardPriorRepeated` | life | an ok report repeats the prior head | HeadNeverRepeats |
| `witnesses/CardWaiveRedWithoutRed` | redw | WaiveRed without the red check | WaiveRedNeedsRed |
| `reach/CardNeverLanded` | life | none | violated: a producer lands |
| `reach/CardNeverFixed` | life | none | violated: a review ends in ReadFix |
| `reach/CardNeverRed` | life | none | violated: merging goes red |
| `reach/CardNeverYesAfterRed` | life | none | violated: a yes after a Red, on a new read |
| `reach/CardNeverFaultRedealt` | life | none | violated: a faulted producer is started again |
| `reach/CardNeverBlocked` | life | none | violated: a producer is blocked |
| `reach/CardNeverSentinelReached` | life | none | violated: the sentinel lands on its landed need |
| `reach/CardNeverNeedFailed` | deps | none | violated: NeedFailed takes a dependant |
| `reach/CardNeverRelinked` | deps | none | violated: a failed need is relinked |
| `reach/CardNeverAdded` | deps | none | violated: Add puts a sentinel with a need and an after |
| `reach/CardNeverSentinelNeeded` | deps | none | violated: a sentinel is a need |
| `reach/CardNeverAskReads` | reads | none | violated: AskReads cuts two reads in one step |
| `reach/CardNeverInThisOp` | deps | none | violated: a need fails and its dependant moves in one op |
| `reach/CardNeverWaivedRed` | life | none | violated: WaiveRed merges after a Red on the earlier read |
| `reach/CardNeverFindingCarried` | life | none | violated: a fix attempt carries the reader's finding |
| `reach/CardNeverPreempted` | pre | none | violated: a blocker preempts a live consumer |

## Results, the CI jar on hetzner2, 2026-11-06 (d93ceeae, round 4)

Every run (store and card) in one parallel pass with `run-tlc.sh`, the
passing runs on 4 workers each and the rest on 1, so the times are under
full contention. `ci-tlc.sh` then passed: 94 runs, 94 ok, exit 0 (a first
pass lost CardDeps to "Cannot allocate memory" while another user's TLC
jobs held about 30 GB; rerun clean).

| Module | Result | Generated / distinct | Depth | Time |
|---|---|---|---|---|
| `CardMachine` (life) | No error has been found | 5808819 / 1359931 | 24 | 2min 4s |
| `CardDeps` (deps) | No error has been found | 21525714 / 4031516 | 25 | 9min 36s |
| `CardPreempt` (pre) | No error has been found | 5687866 / 943758 | 27 | 2min 0s |
| `CardReads` (reads) | No error has been found | 79927 / 27674 | 17 | 22s |
| `CardLandedMoves` | FinalNeverMoves violated | 56 / 46 | 3 | 9s |
| `CardAuthorReads` | ReaderNeverAuthor violated | 10705 / 3788 | 7 | 20s |
| `CardRedAdvances` | RedNeverAdvances violated | 408537 / 130809 | 11 | 59s |
| `CardNeedFailedIgnored` | NeedFailedTakesDependants violated | 6 / 4 | 2 | 8s |
| `CardNonHolderReports` | OnlyHolderActs violated | 2074 / 794 | 5 | 17s |
| `CardTwoWorkConsumers` | WorkingOneWorkConsumer violated | 439 / 229 | 4 | 10s |
| `CardStaleReport` | OldAttemptNeverMoves violated | 30764 / 10902 | 8 | 23s |
| `CardYesOnZero` | ReadYesNeedsYes violated | 10703 / 3786 | 7 | 21s |
| `CardFaultBumpsAttempt` | FaultSpendsNothing violated | 4978 / 1572 | 6 | 19s |
| `CardFaultBounces` | FaultSpendsNothing violated | 4978 / 1572 | 6 | 19s |
| `CardEditUnderLive` | EditNoLive violated | 442 / 232 | 4 | 9s |
| `CardReadyCounted` | TakeBelowWidth violated | 36 / 31 | 3 | 8s |
| `CardNeedsCycle` | NeedsAcyclic violated | 280 / 255 | 3 | 16s |
| `CardWithdrawLeavesWorking` | WithdrawReleasesInOneStep violated | 439 / 229 | 4 | 14s |
| `CardReadFixOnZero` | ReadFixNeedsFix violated | 10703 / 3786 | 7 | 21s |
| `CardHeldStreamReady` | ParkedUnderHold violated | 10428 / 3649 | 7 | 20s |
| `CardWaiveRedWithoutRead` | WaiveRedNeedsPriorRead violated | 1719999 / 476019 | 14 | 2min 13s |
| `CardRelinkAfterCycle` | OrderAcyclic violated | 7380 / 3256 | 4 | 20s |
| `CardReadReportsFail` | ResultsByKind violated | 84689 / 29482 | 9 | 28s |
| `CardReplaceByFinal` | ReplaceByLive violated | 22 / 20 | 3 | 7s |
| `CardTwoReadsOneRow` | OneLiveReadPerRow violated | 31941 / 11338 | 8 | 23s |
| `CardPriorRepeated` | HeadNeverRepeats violated | 26661 / 9341 | 8 | 23s |
| `CardWaiveRedWithoutRed` (redw) | WaiveRedNeedsRed violated | 977451 / 438683 | 20 | 2min 6s |
| reach `CardNeverLanded` | NeverLanded violated | 408529 / 130804 | 11 | 51s |
| reach `CardNeverFixed` | NeverFixed violated | 200192 / 67405 | 10 | 34s |
| reach `CardNeverRed` | NeverRed violated | 408537 / 130809 | 11 | 51s |
| reach `CardNeverYesAfterRed` | NeverYesAfterRed violated | 2227352 / 591860 | 15 | 2min 15s |
| reach `CardNeverWaivedRed` | NeverWaivedRed violated | 747797 / 229675 | 12 | 1min 8s |
| reach `CardNeverFaultRedealt` | NeverFaultRedealt violated | 10458 / 3662 | 7 | 18s |
| reach `CardNeverFindingCarried` | NeverFindingCarried violated | 408827 / 130955 | 11 | 51s |
| reach `CardNeverBlocked` | NeverBlocked violated | 35 / 30 | 3 | 8s |
| reach `CardNeverSentinelReached` | NeverSentinelReached violated | 747709 / 229623 | 12 | 1min 9s |
| reach `CardNeverNeedFailed` | NeverNeedFailed violated | 278 / 253 | 3 | 9s |
| reach `CardNeverInThisOp` | NeverInThisOp violated | 12 / 6 | 2 | 8s |
| reach `CardNeverRelinked` | NeverRelinked violated | 14 / 8 | 2 | 8s |
| reach `CardNeverAdded` | NeverAdded violated | 27 / 21 | 2 | 8s |
| reach `CardNeverSentinelNeeded` | NeverSentinelNeeded violated | 19 / 13 | 2 | 9s |
| reach `CardNeverAskReads` | NeverAskReads violated | 1333 / 666 | 7 | 11s |
| reach `CardNeverPreempted` | NeverPreempted violated | 26847 / 7616 | 8 | 20s |

The counterexamples were checked by hand with `card-trace.py`, and each
move the fault lets through is one card/ at 4a085774 refuses:
- **CardLandedMoves:** p1 is cancelled (fail), then Rescore changes its score.
- **CardAuthorReads:** r2 produces head 1; a read of it is cut to r2.
- **CardRedAdvances:** deal, finish, read ok, ReadYes; then Red lands it.
- **CardNeedFailedIgnored:** p1 fails; NeedFailed is not enabled for p2.
- **CardNonHolderReports:** p1.1 is held by r2 and reported by another row.
- **CardTwoWorkConsumers:** Start again from working: two ready work
  consumers of p1.
- **CardStaleReport:** attempt 1 reports fault, Faulted, attempt 2 is cut;
  attempt 1's report moves p1 to ready again.
- **CardYesOnZero:** Finished, then ReadYes with no read cut.
- **CardFaultBumpsAttempt / CardFaultBounces:** a fault report; Faulted takes
  attempt to 2 / bounces to 1.
- **CardEditUnderLive:** Edit while p1.1 is ready on r2.
- **CardReadyCounted:** p1.1 is ready on r2 with nothing working, and Take is
  refused (it counts itself).
- **CardNeedsCycle:** p1 fails; p2's need p1 is relinked to s, which needs p2.
- **CardWithdrawLeavesWorking:** p1.1 (ready) is withdrawn and p1 stays
  working.
- **CardReadFixOnZero:** Finished, then ReadFix with no read cut.
- **CardHeldStreamReady:** p1.1 reports fail, the stream is held (p1 is
  working, not parked), Broken returns p1 to ready in the held stream.
- **CardWaiveRedWithoutRead:** Finished, read ok, ReadYes, Red, WaiveRed (on
  the review-1 read), Red again (review 3), then WaiveRed: the only ok read
  is of review 1 (the code: "no done ok read of review 2").
- **CardRelinkAfterCycle:** p1 fails; a is added after s; p2's need p1 is
  relinked to a: p2 needs a, a after s, s needs p2.
- **CardReadReportsFail:** a read of head 1 on r1 reports fail.
- **CardReplaceByFinal:** s is cancelled; p1 is replaced by s.
- **CardTwoReadsOneRow:** two live reads of head 1 cut to r1.
- **CardPriorRepeated:** attempt 1 is withdrawn with head 1, attempt 2 is
  cut with prior 1 and reports ok with head 1.
- **CardWaiveRedWithoutRed:** attempt 1 finishes head 1 (review 1), a read
  of it says ok, Block and Unblock take p1 out of review with no Red;
  attempt 2 (prior 1) is withdrawn with head 2, releasing p1 at head 2;
  attempt 3 (prior 2) reports ok with head 1 and Finished brings review 2
  at head 1. WaiveRed then merges on the review-1 read with no Red recorded
  (the code: "no red on review 2"; every other check passes there: Block
  from review with no live consumer, the withdrawn head written by
  Released, an ok head that is neither base nor prior).
- **reach:** Landed runs deal, finish, read ok, ReadYes, Landed. Fixed ends
  review → ready with bounces 1. Red goes merging → review with review 2.
  YesAfterRed then cuts a new read at review 2 and merges on it; WaivedRed
  merges at review 2 on the review-1 read of the same head. FaultRedealt:
  fault, Faulted, Start of attempt 2; FindingCarried: a read says fix, ReadFix,
  Start of attempt 2 carrying the finding. Blocked: Block from ready. SentinelReached: p1
  lands, then Reached. NeedFailed: p1 fails, NeedFailed takes p2; InThisOp:
  p1's Cancel and p2's NeedFailed in one step. Relinked: p2 fails, s's need
  is relinked to p1. Added: a with a need and an after; SentinelNeeded: a
  added needing p1, relinked to s. AskReads: reads on r1 and r3 (r2 is the
  author) in slots 2 and 3, one step. Preempted: p1.1 works on r2, r1 goes
  down, the blocker p2 withdraws p1.1 (releasing p1) and is dealt to r2 in
  one step.

Not modelled: the liveness obligation "nothing stalls", which the card
model's rulings make layer 3's (with the tick); NeverStranded is its state
half here. The reasons ("held: ..."), Merge's two-writes-to-one-field
refusal and Add's id length cap are value checks with no state of their own.

# The steps (layer 3)

`Steps.tla` models the tick of `docs/LAYER-3-STEPS.md` (v2 at 21798cc0),
with the inputs as LAYER-6 and the build rules have them: reports and takes
are the rows' own store ops, and the inbox holds edge results only. The Go
in `steps/` cites it by the invariant names below.

The state is the store `w` (producers, consumer slots, rows up or down, each
stream's hold, and the take order of working consumers), the view `snap`,
`inbox` (edge results: a landing outcome, landed or red with its CI, and a
runner's stop ack), `inflight` (edge requests: id `<<epoch, kind, card,
sha>>`, stream, sent), `js` (open judgements, kind and subject), `split` (the
groups of the last tick's refused ops of two groups or more) and a monitor
`last` of the last tick.

`Snap` copies the store to the view (the fold). Between `Snap` and `Tick`
the store moves under the tick: a row's `Report` (its verb's own op: a work
consumer's report moves its producer in the same plan), the coordinator's
hold (with Park of the stream's ready producers in the same op) and its
lifting, a row's Gone and Seen. `Take` comes only between ticks: a take
changes nothing a tick fences. `EdgeResult` delivers an in-flight request's
result once or twice (layer 4 may redeliver).

`Tick(a)` is one atomic action. From the view alone, in the design's order:
1. **Preemption**: a ready blocker with every open serving row at width
   (working consumers only) is one group: Withdraw of the victim on the
   chosen row (releasing its producer), the blocker's Start there, and a
   stop request for the victim's runner. The row is policy's `rows` answer
   when it is a preemptable row, else the first by name; the victim is the
   youngest working consumer on it by take, the engine's fixed rule
   (LAYER-5's boundary, LESSONS 5.12; the model's first draft asked policy
   for a `victim`, dropped when layer 3 was built against layer 5's
   question set). No open serving row: an `unserved` judgement for the tier.
2. **Land results**, in arrival order: Landed or Red for a producer still
   merging at the request's head; an ack, a stale or a repeated result is
   consumed with no move.
3. **Needs**: Resolve in id order: needs landed, stream not held, no
   producer of the stream overlapping its paths that is started (working,
   review, merging) or ready, counting those resolved earlier in the tick.
   Never a function of rows.
4. **Fix**: ReadYes or ReadFix (the default verdict) once every read of the
   current review is final and one is done.
5. **Deal and reads**: blocker work, every read owed, normal work, in id
   order; a row is dealt while ready plus working is under width plus
   `ahead` (2 x width), with the in-tick tally. The row is policy's `rows`
   answer when it is eligible for the candidate, else the default: the
   desired rank exactly (the card's tier; one up once its bounces reach
   `escalate_after`; for a read, the rank above its author's row), then
   above it, then an idle lane, most room, name. A row below the card's
   tier is never eligible. No open serving row: `unserved`; a review no
   row may read: `cannot-read`.
6. **Land requests**: per stream with none in flight, the first merging
   producer none of whose `after` targets is merging (a hold never touches
   landing).
7. **Judgements**: `unserved`, `cannot-read`, and one `policy` judgement
   per question whose answer failed for a subject; `unserved` and
   `cannot-read` expire when the cause clears in the view.

A card claimed by an earlier step is skipped by the later ones. Then the
order (blocker groups, never cut; land results; needs; fix; the deal), the
budget (cards touched; the first non-blocker group always goes, so a group
larger than the budget is not starved; stop at the first group that does
not fit, so the inbox is consumed as a prefix), the packing (up to `Pack`
groups per op; a group touching a card of a `split` group is an op of its
own), and the apply: each op in order is accepted whole if every record its
groups read is as the plan expects it (the view with this tick's accepted
ops applied) and every move passes layer 2's guard on the store as it is,
else refused whole. A stop request is dispatched only with its accepted op
(its After); a land request has an empty After. An inbox item is consumed
when admitted and its op accepted (or it has no move); a refused op's items
are put back in place; the rest carry, in arrival order.

The oracle `a` is the asker: when a question has subjects, policy answers
in domain (either row preferred for every candidate) or fails ("failed": no
answer in the tick's policy time, an error, out of range, or a row not
offered, which is one path in the engine).

The per-card moves are layer 2's, written at the grain the steps read: each
guard (`Legal`) and write (`ApplyG`) is the `CardMachine.tla` action of the
same name. `CardMachine.tla` cannot be INSTANCEd here: its card sets are
fixed by its `Scenario` constant (`Prods` is `{p1}` or `{p1, p2}`), and
preemption of the youngest of two working consumers needs three producers.
Heads are attempt numbers (an ok report of attempt n pushes head n).

## The instance: four scenarios

Rows: r1 width 1 serves tier 1 (rank 1); r2 width 2 serves tiers 1 and 2
(rank 2); `ahead` is 2 x width.

| Scenario | Cards | Start | Environment | Constants |
|---|---|---|---|---|
| pre | b blocker (s2), p, q (s1, disjoint paths) | all waiting, s2 held, r1 down | lift s2's hold; r2 down and up | K 2, MaxAtt 2, MaxEnv 2, MaxArrive 2, Budget 2, Pack 2 |
| pre1 | as pre | as pre | as pre | as pre, Pack 1 |
| deal | p, q (s1, overlapping), b (s2, tier 2, needs p) | all waiting | r1 down and up | K 4, MaxAtt 2, MaxBounce 1 (p only), MaxEnv 1, MaxArrive 1, Budget 2, Pack 2 |
| land | p (s1), q (s2, after p) | both merging at head 1 | hold and lift s1 | K 2, MaxAtt 1, MaxEnv 2, MaxArrive 3, Budget 2, Pack 2 |
| burst | as land | as land | none | K 1, MaxAtt 1, MaxArrive 4, Budget 1, Pack 1 |

Red results come only in land and burst; work and read reports are ok but
in deal, where p may fail and a read of p may say fix. A fault report is a
fail that spends no bounce, so no scenario needs it.

## Invariants and the liveness property

Named as the design names its guarantees. State invariants:

| Invariant | Design | Says |
|---|---|---|
| `NeverDealtTwice` | guarantee 1 | at most one live work consumer per producer, of its current attempt, only while it works |
| `WidthKept` | guarantee 2 | working consumers on a row never exceed its width |
| `ReadyNeverFlipsWithLoad` | guarantee 8, LESSONS 2.20, 3.2 | the ready rule answers the same for the store and for the store with no consumers and every row down |
| `NeverOverlapStarted` | Sentinels and needs (LESSONS 2.20) | two producers of one stream with overlapping paths are never ready or started together |
| `NeverBelowTier` | Escalation | every live consumer is on a row that serves its card's tier (no tier is ever written) |
| `CarryFIFO` | guarantee 7, list item 6 | the inbox is in arrival order and nothing that arrived is lost |
| `EdgeAfterAccepted` | Plan.Edges | an in-flight stop names a withdrawn consumer; an in-flight land a producer merging at that head |

Tick invariants, each a check of the last tick computed from the view, the
store before and after and the plan (`TickChecks`), apart from the planning
operators where the check is about them:

| Invariant | Design | Says |
|---|---|---|
| `OneGroupPerCard` | list item 4, guarantee 1 | no card is in two of the tick's groups |
| `GroupWhole` | list item 4, guarantee 5 | each group's cards are all as applied or all unchanged |
| `NeverHalfApplied` | guarantee 5 | each op's groups are all applied or none |
| `RefusalChangesNothing` | tick step 7 | a refused op changes none of its cards |
| `RefusedOnlyByRace` | list item 5 | an op is refused only when a record it read moved between the fold and the tick |
| `BudgetKept` | tick step 3, list item 6 | past the first non-blocker group, the cards touched stay within the budget |
| `BlockerPreemptsInOneTick` | guarantee 4 | a ready blocker with every open serving row at width has a preemption group in the tick, or no open serving row and an `unserved` judgement |
| `PreemptYoungest` | Preemption | the victim is the youngest working consumer on the row by take |
| `PolicyFailsToDefault` | guarantee 10 | a failed rows answer deals exactly what no policy deals, and every failed question has its `policy` judgement |
| `AfterKept` | guarantee 3, ENFORCED L2-U19 | a land request never goes out for a producer whose `after` target is merging |
| `AfterNeverHolds` | guarantee 3, ENFORCED L2-U18 | a stream with none in flight and a merging producer whose `after` targets are not merging gets a land request |

`ReadyDealtWhenRoom` (guarantee 8's second half, LAYER-2's nothing stalls),
under weak fairness on `Snap` and `Tick` only (rows need not take or
report): a ready producer with room on an open serving row (under width
plus `ahead`) is eventually dealt, unless the room goes. The model's bounds
(attempts, slots) are exempt, as in `CardMachine`.

## Expected results

| Module | Scenario | Fault | Expected result |
|---|---|---|---|
| `StepsPre` / `StepsPrePack1` / `StepsDeal` / `StepsLand` / `StepsBurst` | pre / pre1 / deal / land / burst | none | pass, liveness included |
| `witnesses/StepsDealtTwice` | pre | the deal does not skip a blocker the preemption claimed | NeverDealtTwice |
| `witnesses/StepsTwoGroups` | land | the land-result step ignores claimed cards | OneGroupPerCard |
| `witnesses/StepsWidthOver` | pre | Take checks width plus ahead | WidthKept |
| `witnesses/StepsBlockerQueued` | pre | preemption counts queue room as free width | BlockerPreemptsInOneTick |
| `witnesses/StepsHalfWrites` | pre | a refused preemption's Withdraw half is applied | GroupWhole |
| `witnesses/StepsPartialOp` | pre | a refused op applies its unraced groups | NeverHalfApplied |
| `witnesses/StepsRefusedApplied` | pre | a refused op is applied anyway | RefusalChangesNothing |
| `witnesses/StepsSelfRefusal` | deal | the deal does not pre-check that a row is up | RefusedOnlyByRace |
| `witnesses/StepsFenceOnView` | pre1 | ops fenced on the view, not the view plus the tick's accepted ops | RefusedOnlyByRace |
| `witnesses/StepsBudgetIgnored` | burst | no budget | BudgetKept |
| `witnesses/StepsCarryDropped` | burst | the carry drops what the budget left | CarryFIFO |
| `witnesses/StepsCarryNewestFirst` | burst | the carry is put back newest first | CarryFIFO |
| `witnesses/StepsStopEarly` | pre | stop requests from refused ops too | EdgeAfterAccepted |
| `witnesses/StepsReadyOnRoom` | deal | Resolve requires room on a row | ReadyNeverFlipsWithLoad |
| `witnesses/StepsNoOverlapCheck` | deal | Resolve without the overlap check | NeverOverlapStarted |
| `witnesses/StepsTierUnchecked` | deal | the tier checked nowhere (answer and Start) | NeverBelowTier |
| `witnesses/StepsAfterIgnored` | land | the land step ignores after | AfterKept |
| `witnesses/StepsAfterHolds` | land | the land step holds on any target not landed | AfterNeverHolds |
| `witnesses/StepsPolicyStall` | deal | a failed rows answer plans nothing | PolicyFailsToDefault |
| `witnesses/StepsOldestVictim` | pre | the victim is the oldest | PreemptYoungest |
| `witnesses/StepsReadyCountedAgainstWidth` | pre | the deal counts ready against width | ReadyDealtWhenRoom (liveness, TLC exit 13) |
| `reach/StepsNeverPreempted` | pre | none | violated: a blocker preempts |
| `reach/StepsNeverUnserved` | pre | none | violated: unserved is opened |
| `reach/StepsNeverRefused` | pre | none | violated: an op is refused by a race |
| `reach/StepsNeverSplit` | pre | none | violated: a group of a refused op is an op of its own |
| `reach/StepsNeverStopAcked` | pre | none | violated: a stop ack is consumed |
| `reach/StepsNeverPolicyJudged` | pre | none | violated: a policy judgement is opened |
| `reach/StepsNeverResolved` | deal | none | violated: Resolve |
| `reach/StepsNeverDealt` | deal | none | violated: Start |
| `reach/StepsNeverReadAsked` | deal | none | violated: a read is cut |
| `reach/StepsNeverMerged` | deal | none | violated: ReadYes |
| `reach/StepsNeverFixed` | deal | none | violated: a producer back from a bounce |
| `reach/StepsNeverEscalated` | deal | none | violated: a bounced producer dealt one rank up |
| `reach/StepsNeverNeedReleased` | deal | none | violated: a need lands and its dependant resolves |
| `reach/StepsNeverCannotRead` | deal | none | violated: cannot-read is opened |
| `reach/StepsNeverLandRequested` | land | none | violated: a land request |
| `reach/StepsNeverLanded` | land | none | violated: Landed |
| `reach/StepsNeverRed` | land | none | violated: Red |
| `reach/StepsNeverAfterHeld` | land | none | violated: a landing waits on a queued after target |
| `reach/StepsNeverCarried` | burst | none | violated: the budget carries inbox items |

`python3 make-steps-witnesses.py` writes every Steps witness and cfg (the
five passing cfgs, `witnesses/Steps*`, `reach/Steps*.cfg`) from `Steps.tla`
and the fault list in the script, as the other two generators do.
`ci-tlc.sh` runs them with the rest: a liveness witness's cfg lists its
PROPERTY first and must end in TLC exit 13 with "Temporal property
ReadyDealtWhenRoom was violated" and no invariant violated.

## Results

The CI jar on hetzner2, 2026-11-07, after the two changes layer 3's build
made to the model (the `victim` oracle dropped; rows fenced by generation,
`rg`): `ci-tlc.sh` ran every Steps row: 5 passing cfgs pass (StepsPre,
StepsPrePack1 and StepsDeal rerun alone with 8 workers after the OOM killer
took them in the full parallel run of 168 JVMs; StepsLand and StepsBurst in
the run), all 21 witnesses violated as required, all 19 reach checks
violated, all 25 trace fixtures as their cfgs expect (13 of the model's own,
12 of the walk's). The table below is the model child's run before those
two changes (vision, 2026-11-07 UTC, 4 workers for the passing runs, 1 for
witnesses and reach checks); state counts move a little with `rg`.

| Run | Result | Generated / distinct | Depth | Time |
|---|---|---|---|---|
| StepsPre | pass | 55,234 / 15,302 | 26 | 2m24s |
| StepsPrePack1 | pass | 55,188 / 15,284 | 26 | 2m28s |
| StepsDeal | pass | 27,019 / 11,006 | 48 | 47s |
| StepsLand | pass | 16,821 / 7,125 | 38 | 23s |
| StepsBurst | pass | 3,796 / 1,923 | 28 | 9s |
| StepsDealtTwice | NeverDealtTwice | 3,224 / 1,332 | 14 | 20s |
| StepsTwoGroups | OneGroupPerCard | 63 / 38 | 6 | 1s |
| StepsWidthOver | WidthKept | 2,459 / 1,088 | 13 | 14s |
| StepsBlockerQueued | BlockerPreemptsInOneTick | 3,224 / 1,332 | 14 | 20s |
| StepsHalfWrites | GroupWhole | 4,231 / 1,680 | 15 | 26s |
| StepsPartialOp | NeverHalfApplied | 146 / 96 | 7 | 5s |
| StepsRefusedApplied | RefusalChangesNothing | 58 / 41 | 6 | 2s |
| StepsSelfRefusal | RefusedOnlyByRace | 28 / 25 | 6 | 1s |
| StepsFenceOnView | RefusedOnlyByRace | 5 / 5 | 3 | 1s |
| StepsBudgetIgnored | BudgetKept | 444 / 255 | 13 | 5s |
| StepsCarryDropped | CarryFIFO | 1,524 / 903 | 18 | 9s |
| StepsCarryNewestFirst | CarryFIFO | 1,526 / 905 | 18 | 9s |
| StepsStopEarly | EdgeAfterAccepted | 4,231 / 1,680 | 15 | 26s |
| StepsReadyOnRoom | ReadyNeverFlipsWithLoad | initial state | 1 | 1s |
| StepsNoOverlapCheck | NeverOverlapStarted | 4 / 4 | 3 | 1s |
| StepsTierUnchecked | NeverBelowTier | 1,755 / 986 | 22 | 8s |
| StepsAfterIgnored | AfterKept | 4 / 4 | 3 | 1s |
| StepsAfterHolds | AfterNeverHolds | 152 / 82 | 8 | 3s |
| StepsPolicyStall | PolicyFailsToDefault | 13 / 11 | 5 | 1s |
| StepsOldestVictim | PreemptYoungest | 3,226 / 1,333 | 14 | 22s |
| StepsReadyCountedAgainstWidth | ReadyDealtWhenRoom (exit 13) | 8,052 / 2,965 | 19 | 1m04s |

All 19 reach checks are violated, in 1s to 30s each (StepsNeverStopAcked the
longest, 6,384 generated at depth 17).

## The counterexamples, checked by hand

Each trace was read state by state; the move the fault lets through is one
the correct model never makes (the passing runs show that).
- **StepsDealtTwice:** p and q resolve, are dealt to r2 (r1 is down) over
  two ticks and taken; s2's hold is lifted, b resolves. The next tick
  preempts p1 for b on r2 and, the claim ignored, also deals b a plain
  Start in the same op: b at attempt 2 with two ready work consumers.
- **StepsTwoGroups:** p's land request goes out, its red result arrives
  twice; one tick plans Red for p from both items (the second should have
  been consumed with no move): review raised twice.
- **StepsWidthOver:** p is dealt and taken on r2; s2 is lifted and b, then
  q, are dealt to r2 while p works (r2 has free width, so no preemption);
  the takes of q and b both pass: three working on a row of width 2.
- **StepsBlockerQueued:** p and q work on r2, b becomes ready with no free
  width; the faulted step sees queue room and skips preemption, and the
  deal queues b as a ready consumer behind two working ones.
- **StepsHalfWrites:** b is due; the tick plans preempting p1, but p1
  reports ok between the fold and the tick, so the op is refused; the
  fault still writes p1 withdrawn (fail over its done report) without b's
  Start.
- **StepsPartialOp:** s2 lifted, b and p resolve; r2 goes down between the
  fold and the tick, whose first op is b's Start on r2 with q's Resolve; the
  op is refused on r2's fence and the fault applies q's Resolve alone.
- **StepsRefusedApplied:** r2 goes down between the fold and the tick; the
  tick's Start of p on r2 is refused, and applied anyway: p working, its
  consumer cut on a down row.
- **StepsSelfRefusal:** r1 goes down; the tick plans p's Start on r1 (the
  default's first choice for a tier-1 card) without the up check, and the
  store refuses it with nothing having moved since the fold.
- **StepsFenceOnView:** the first tick resolves p and q, one op each (pack
  1); q's Resolve reads its stream's producers, p's record was written by
  the tick's own first op, and the fence on the view refuses it with no
  race.
- **StepsBudgetIgnored:** p is red, re-read and settled while q's red
  result waits; one tick admits p's ReadYes and q's Red with a budget of 1.
- **StepsCarryDropped:** three results wait (p's red, q's red twice after
  a redelivery); the tick takes p's and drops the rest: two items lost.
- **StepsCarryNewestFirst:** the same inbox; the carry is put back as
  stamps 4, 3.
- **StepsStopEarly:** as StepsHalfWrites, p1 reports between the fold and
  the tick and the preemption op is refused, but the stop request for p1's
  runner goes out: in flight for a consumer that is done, not withdrawn.
- **StepsReadyOnRoom:** the initial state: p is resolvable, and the
  faulted rule says ready with rows up and not ready with them down.
- **StepsNoOverlapCheck:** the first tick resolves p and q, whose paths
  overlap: both ready.
- **StepsTierUnchecked:** p is dealt to r1, finishes, is read on r2, merges
  and lands; b (tier 2) and q resolve, and policy's answer r1 deals b to r1,
  which serves tier 1 only.
- **StepsAfterIgnored:** the first tick sends both p and q to land, though
  q is after p and p is merging.
- **StepsAfterHolds:** p's result is red (review again, not queued), q is
  merging after p; the next tick sends nothing for s2.
- **StepsPolicyStall:** p is ready, the rows answer fails, and the tick
  deals nothing (the default deals p to r1).
- **StepsOldestVictim:** p1 then q1 taken on r2, b due, and the tick
  withdraws p1, the older.
- **StepsReadyCountedAgainstWidth:** p and q are dealt to r2 and b
  preempts p1; p is ready again with r2 at 2 of 6 (width plus ahead), but
  the faulted deal sees 2 of 2. The lasso is the tick looping on that
  state with q1 and b1 working forever (rows are not fair): p never dealt.
- **reach:** Preempted: p1 and q1 working on r2, b lifted and due, p1 or q1
  withdrawn and b dealt in one op, p released to ready. Unserved: r2 goes
  down with b due. Refused: r2 down between fold and tick. Split: a refused
  two-group op's groups each alone next tick. StopAcked: the preemption's
  stop request answered and consumed. PolicyJudged: a failed answer at the
  first deal. Resolved, Dealt, ReadAsked, Merged: p's path through deal.
  Fixed: p's read says fix or its work fails, back to ready with a bounce.
  Escalated: p, bounced once, dealt to r2 (rank 2). NeedReleased: p lands,
  b leaves waiting. CannotRead: r1 down while p (author r2) owes a read.
  LandRequested, Landed, Red: the first land request and its two results.
  AfterHeld: q held while p merges. Carried: two results, budget 1.

## Trace validation: the code bound to the model (the lock gate since 2026-11-07)

`StepsTrace.tla` constrains the spec's next-state relation to a recorded
trace (the Kuppe, Merz et al. pattern): `TraceLog` is a sequence of records,
each an action name with its arguments and the model's variables after it,
and `TraceNext` takes record `l + 1` only by the action it names, landing
exactly on its logged values. A trace the model allows reaches the end
(`TraceMatched`, TLC exit 0); a step the model forbids leaves no successor,
and the counterexample's last `l` names the record before the refused one.
`tla/check-trace.sh [traces/<fixture>...]` runs it on each fixture and
prints `ok <fixture>: matched whole, N records` or `FAIL <fixture>: the
model refuses record K of N:` with that record's action and arguments.

Two kinds of fixture, both under `traces/`:

- **The walk's** (`steps-walk-<scenario>-<seed>/`): the Go steps driving one
  of the model's four scenarios on `store.Fake` (`steps/trace_test.go`), each
  step one of the model's actions (Snap, Tick, Take, Report, Hold, Unhold,
  Gone, Seen, EdgeResult) picked at random, each recorded with the model's
  variables projected from the code's state by the refinement mapping in
  `steps/trace.go` (`StoreTLA`, `InboxTLA`, `InflightTLA`, `JudgementsTLA`;
  the mapping is in that file's comment). A record logs `w` in parts (`ps`,
  `up`, `sh`, `tko` whole and `cs` as the slots that are not `none`, which
  `StepsTrace.Parts` checks), the inbox, the in-flight requests and the
  open judgements of the model's three kinds, and a Tick's `answer`. The
  tick plans on the view as Snap left it, so a move between Snap and Tick
  races it as in the model. The cfg sets the model's bounds above anything a
  walk reaches (K 60, MaxAtt 30, MaxBounce 30, MaxEnv and MaxArrive 1000)
  and the scenario's Budget and Pack. `TestTraceFixtures` writes the fixed
  seed set (4 scenarios x 3 seeds x 150 steps) under `NOVA_TRACES`; the CI
  job `trace` writes it every push and runs `check-trace.sh` on it.
  `TestTraceWalk` runs 100 seeds x 2,000 steps per scenario for depth (at
  least 40% of the picked moves legal while a producer is not final) and
  that no op is refused without a race.
- **The model's own** (`steps-ok-*`, `steps-bad-*`), `make-steps-traces.py`
  from TLC logs: a reach check's counterexample is a behaviour of the model
  and must match whole; a state-invariant witness's counterexample is a
  behaviour of the faulted model whose last step the real model forbids
  (`expect: forbidden at K`). They stand for the walk's bugs: the same fault
  in `steps/` makes the same trace.

What the trace check found while layer 3 was built (each a divergence
between code and model that no unit test saw): the first walk fixtures were
refused at a Tick because the Go walk folded the view after every outside
move, so the tick planned on fresh state where the model's planned on the
snapshot; the walk now keeps the tick's view as Snap left it (the code
itself was right: layer 6 folds only at the tick's start). The model's
`victim` oracle went when layer 3 was built against layer 5's question set,
which has none (the youngest is the engine's rule).

## Not modelled, and what the model found in the design

Not modelled: holds' Withdraw of untaken consumers after the grace, the
retire and fail limits (Block at the bounce limit, Withdraw on silence,
Gone, flap), sentinels and `landed-sentinel`, drops (NeedFailed over a
closure, Waive, Relink), backed-up landing groups and the rungs, `conflict`
and `base-red`, the alarms (`stall`, `idle`, `land-rate`), judgement
deadlines and expiry defaults, the per-card request verbs and their actor
rule, op ids (an op is its body here; distinct bodies in one tick follow
from OneGroupPerCard), and the `Park` step: the hold parks in its own op and
every return to ready under a hold writes waiting (CardMachine `Back`), so a
ready producer the hold's op missed does not arise; the step is a defence.
The engine judgement for a group refused on fresh state is not modelled
either: RefusedOnlyByRace shows no op is refused but by a race when the
steps pre-check every store rule, so that path is a defence for a defect
(StepsSelfRefusal shows what takes it). Guarantee 6 (a stale report) is the
report verb's under LAYER-6 and `CardMachine`'s `OldAttemptNeverMoves`.

What the model found, each with the design line:
1. **The fence must be on the gens the plan predicts.** Tick step 7: "Apply
   the ops in order, each fenced on the gens its groups read." Read as the
   view's gens, an op whose group read a card an earlier op of the same
   tick wrote is refused by the tick itself (StepsFenceOnView: a Resolve
   reads its stream's producers). The model fences on the view with the
   tick's accepted ops applied.
2. **A group can be larger than the budget.** Tick step 3: the budget is a
   count of cards touched (a Start touches two, a preemption four), with
   no rule for a group over it; step 4's "A group larger than the packing
   size is an op of its own" cannot apply, since `pack` counts groups. The
   model always admits the first non-blocker group of a tick.
3. **A refused op's inbox items.** Step 5: "Only the inbox carries ...
   every other move is derived again from the store". A land result whose
   op was refused cannot be derived again; the model puts it back in place
   (CarryFIFO holds with it).
4. **The order list omits needs and fix.** Step 3 lists blocker groups,
   reports and takes, landing groups, drops and holds, limits, judgements,
   the deal; Resolve and ReadYes/ReadFix groups are not placed. The model
   puts them after landing groups, before the deal.
5. **Guarantee 4 says "no room", the step says "no free width".**
   Guarantee 4: "A ready blocker with no room is dealt by preemption";
   Preemption: "with no free width on an open serving row". Room elsewhere
   is width plus `ahead`; read that way the blocker is queued behind full
   rows (StepsBlockerQueued). The model takes free width (working under
   width).
6. **Inputs: LAYER-3 against LAYER-6.** LAYER-3 Reports: "Takes ride the
   same inbox", step 5 and guarantee 7 carry reports and takes; LAYER-6
   section 2: "Reports are ordinary ops ... Nothing reaches run over the
   bus". The model follows LAYER-6 (the build rules: layer 6 wins).
7. **Apply and carry: LAYER-3 against LAYER-6.** LAYER-6 tick steps 5 and
   6: "Apply the merged plan as one op", "the plans fenced on that card are
   put back, and the rest is applied again", "a plan past the budget is
   carried"; LAYER-3 steps 4, 5 and 7 pack into ops, split a refused op by
   group next tick, and carry only the inbox. The model follows LAYER-3.
8. **Edge request ids.** The brief's `<<epoch, kind, card, sha>>` and
   Land's "Request ids hash stream, heads and base" both give the second
   landing of one head after a red the id of the first (with the base
   unchanged), so a late redelivery of the first result reads as the
   second's. The model does not redeliver after a result is consumed, so
   it does not show this.
9. **Escalation's fallback.** "a work consumer at the card's own tier
   first" does not say what follows when no row of that rank has room; the
   model falls back to higher ranks, then lower ones that still serve the
   tier.
10. **CardMachine is not composable.** Its card sets are fixed by
    `Scenario`; a `Prods` and `Rows` constant there would let this model
    INSTANCE its guards and invariants instead of restating them.
