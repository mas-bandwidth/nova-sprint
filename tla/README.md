# tla/: the store model (layer 1) and the card machine (layer 2)

Two models live here: `Store.tla` (layer 1, below) and `CardMachine.tla`
(layer 2, in its own section after the store's results). `ci-tlc.sh` runs
both, with all their witnesses and reach checks, in one parallel pass.

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

Rounds 5 and 6 (card/ at v2-card-fix4 b2b0b8c2, and the ruling after the
fourth cold read): Merge composes moves. An op is an ordered list of up to
`MaxOp` moves, nested merges flattened first, each planned against what
the earlier ones left: in the model the moves of an op are consecutive
steps, `op` counts the moves of the open op and `snap` is the state it
started from. A consumer cut earlier in the op is live for the next move;
the stream, rows and cards are the folded ones; a move's conditions are
the first-read value of each field, minus the fields an earlier move of the
op wrote, and the store checks them against `snap` (FencesOnSnapshot). A
sequence legal one by one is one op (Red then WaiveRed included). The
preempt guard runs on the whole op for Withdraws flagged preempt
(`PreWithdraw`): with a blocker's Start in the same op, the withdrawn
consumer is working on the row that Start names (PreemptOnBlockerRow; `pw`
and `bs` keep the rows of the op's preempt Withdraws and blocker Starts).
`MaxOp` is read off the scenario (merge 3, mergepre and mergedeps 2, every
other 1), so a cfg that names none has ops of one move.

Round 7 (the rulings after the fifth cold read,
`BRIEF-V2-CARD-ROUND7-draft-2026-11-07.md`), modelled ahead of the code
(card/ at v2-card-fix4 b2b0b8c2 has not taken them; see the divergences
below):
- Ruling 1, ids without dots: cards and consumers share one namespace,
  fenced absent on every Add and every cut (Start, AskRead, AskReads), and
  consumer ids are the code's (`<p>.w<attempt>`, `<p>.r<review>.<n>`,
  `ConsName`). deps carries the id `p1.w1`, which Add refuses for its dot;
  without that refusal it is added before p1 starts and p1 can never cut
  its first work consumer (CardDottedId, NeverStranded). IdsDistinct states
  the namespace. The 104-byte cap is a value check with no state.
- Ruling 2, InThisOp: legal only as a later move of an op whose earlier
  move failed the need, checked on the folded need state (`InThisOpLater`,
  `InThisOpFolded`); alone it is refused (CardInThisOpAlone,
  InThisOpOnFailedNeed). With `MaxOp` 1 the op is the one step it was
  (Cancel and the dependant's move together). "Drops it" is read as the
  earlier move writing the need to fail (Cancel or Replace).
- Rulings 3 and 8, Preempt as an event field, on the flattened list: the
  guard reads only Withdraws flagged preempt (`PreWithdraw`); a plain
  Withdraw (`PlainRows` is empty) merges with a blocker's Start on another
  row (PlainWithdrawUnguarded, CardPlainWithdrawGuarded); a nested Start
  never skips it (CardMergeNestedStart).
- Rulings 4 and 11, composition: each move reads the folded state, the last
  write wins, and nothing refuses a second write (MergeParksResolved:
  HoldStream later in an open op is always enabled, so Merge(Resolve,
  HoldStream) parks what the Resolve readied; CardMergeSecondWrite). The
  order-independence test for ReportPlan and row-hold pairs is the code's:
  no guard of Report or the producer moves reads a row's hold.
- Ruling 6: Start copies `finding` only (reason is not modelled); a fail or
  fault report may omit the head, and a head given differs from the base
  and the prior head for every result (CardFailRepeatsPrior); ReadFix with
  an empty finding is refused, so ReadFix always writes one (FixSaysWhat,
  CardReadFixEmpty). The reason-only Edit of a parked producer is a refusal
  on `reason`, which no other guard reads, so it is not modelled (Edit is a
  stutter here).
- Ruling 7, `seen` is the records read before any write (CardMergeOwnWrites,
  FencesOnSnapshot) and the review counter is folded once: it rises by one
  on Finished and Red and on nothing else (ReviewOnEntry,
  CardMergeReviewTwice: Report then Finished in one op raising it by two).
- Ruling 9, hand-built plans: a move's record is the real one with its
  fields written; a merged move writing a partial record (missing fields at
  their zero values) resets what it did not name (CardHandPlanPartial:
  Start then SetPriority in one op resets the attempt, AttemptOnlyOnStart).
- Rulings 5 and 10 (wording) and 12 (the walk, the PG mirror, card/README)
  are the code's.

`last` is a step monitor. `StepRule` reads the state before and after each
step and records the step rules it broke (`last.bad`). It is shared by every
action and part of none, so a fault in an action is caught there; it keeps
only four event names (NeedFailed, Preempt, AskReads, InThisOp, for their
reach checks), so states
are not split by the move that reached them.

## The instance: seven scenarios

| cfg | Scenario | producers | needs | K | heads | attempts | env moves |
|---|---|---|---|---|---|---|---|
| `CardMachine.cfg` | `life` | p1, sentinel s | s needs p1 | 3 | 2 | 2 | 2 |
| `CardDeps.cfg` | `deps` | p1, p2, sentinel s, sentinels a and p1.w1 (absent until Add; Add refuses p1.w1) | p2 needs p1, s needs p2 | 2 | 1 | 1 | 1 |
| `CardPreempt.cfg` | `pre` | p1, p2 | none; p2 `after` p1 | 2 | 1 | 1 | 2 |
| `CardReads.cfg` | `reads` | p1; rows r1, r2, r3 | none | 3 | 1 | 1 | 1 |
| `CardMerge.cfg` | `merge` (life, ops of 3 moves) | p1, sentinel s | s needs p1 | 2 | 1 | 1 | 1 |
| `CardMergePre.cfg` | `mergepre` (pre, ops of 2) | p1, p2 | none; p2 `after` p1 | 2 | 1 | 1 | 1 |
| `CardMergeDeps.cfg` | `mergedeps` (deps, ops of 2) | as deps | as deps | 2 | 1 | 1 | 0 |
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

- `CardMachine.tla` and its seven passing cfgs.
- `witnesses/Card*.tla`: thirty-six reversed witnesses; `reach/Card*.cfg`:
  seventeen reachability checks. Generated with every card cfg by
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
- **IdsDistinct**: no existing card's id is a cut consumer's id (one
  namespace, round 7).
- **PreemptOnBlockerRow**: an op's preempt Withdraws are of consumers on the
  row its blocker Starts name.
- **PlainWithdrawUnguarded**: in an open op, a plain Withdraw of any working
  consumer is enabled (the guard reads only the Preempt flag; round 7).
- **MergeParksResolved**: in an open op, with the stream not held and an
  environment move left, HoldStream is enabled (no second-write refusal;
  round 7).

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
- **FencesOnSnapshot**: in an op, a move's conditions on a card's state are
  those of the snapshot, minus states an earlier move of the op wrote.
- **ReviewOnEntry**: review rises by one on Finished and Red of that
  producer, and on nothing else (round 7).
- **FixSaysWhat**: ReadFix writes a finding (round 7).
- **InThisOpOnFailedNeed**: after an InThisOp move, its need is fail (round
  7).

## Expected results

| Module | Scenario | Fault | Expected result |
|---|---|---|---|
| `CardMachine` / `CardDeps` / `CardPreempt` / `CardReads` / `CardMerge` / `CardMergePre` / `CardMergeDeps` | life / deps / pre / reads / merge / mergepre / mergedeps | none | pass |
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
| `witnesses/CardMergeCutUnseen` | merge | a consumer cut earlier in the op is unseen by the next move | BlockNoLive |
| `witnesses/CardMergeNestedHold` | merge | a release reads the stream's hold from the snapshot | ParkedUnderHold |
| `witnesses/CardMergeNoRerun` | merge | a merged move is checked on the snapshot, not the fold | FinalNeverMoves |
| `witnesses/CardMergeOwnWrites` | merge | a move fences a state an earlier move wrote | FencesOnSnapshot |
| `witnesses/CardMergeNestedStart` | mergepre | the preempt guard skips a nested Start | PreemptOnBlockerRow |
| `witnesses/CardDottedId` | deps | Add takes an id with a dot | NeverStranded |
| `witnesses/CardInThisOpAlone` | mergedeps | InThisOp alone, the need not failed in the op | InThisOpOnFailedNeed |
| `witnesses/CardPlainWithdrawGuarded` | mergepre | the preempt guard on every Withdraw | PlainWithdrawUnguarded |
| `witnesses/CardMergeSecondWrite` | merge | a second write to a field refused | MergeParksResolved |
| `witnesses/CardReadFixEmpty` | life | ReadFix with an empty finding | FixSaysWhat |
| `witnesses/CardFailRepeatsPrior` | life | a fail report names the prior head | HeadNeverRepeats |
| `witnesses/CardMergeReviewTwice` | merge | Report then Finished in one op raises review by two | ReviewOnEntry |
| `witnesses/CardHandPlanPartial` | merge | a merged move writes a partial record | AttemptOnlyOnStart |
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
| `reach/CardNeverInThisOpMerged` | mergedeps | none | violated: a need fails and a later move of the op moves its dependant |

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
half here. The reasons ("held: ...", and so the reason-only Edit of a
parked producer), the refusal wording and Add's id length cap are value
checks with no state of their own; round 7 removes Merge's second-write
refusal (MergeParksResolved).
