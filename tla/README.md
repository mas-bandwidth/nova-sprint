# tla/: the store model (layer 1), the card machine (layer 2) and the steps (layer 3)

Three models live here: `Store.tla` (layer 1, below), `CardMachine.tla`
(layer 2, in its own section after the store's results) and `Steps.tla`
(layer 3, last). `ci-tlc.sh` runs all three, with all their witnesses and
reach checks, in one parallel pass.

`Store.tla` models the contract in `docs/LAYER-1-STORE.md` as of round 22.
Each operation is one atomic step over `epoch`, `cards`, `rows`,
`judgements`, `cursors`, `policies`, `stopped`, `events` and `applied`. In
order, the step:
1. checks the op's epoch against the store's epoch;
2. checks its conditions;
3. runs its writes in order, each under its own rule;
4. either lands everything and appends an accepted event, or changes nothing
   and appends a refused event.

Epochs start at 1. `Clear` is an op with no writes and its epoch is checked
too. It opens the next epoch and carries the rows forward: same session and
role, state `down`, gen + 1, not held (a hold is the coordinator's decision
for one epoch; 2026-11-08). It keeps the cursors (global, as
the log's seq is, so a subscriber resumes where it was), the policies and
the sprint's stop, and leaves the cards and judgements behind (keyed by
epoch). It is idempotent by id and logged with the epoch it closed. The
event log is append-only: row triggers refuse UPDATE and DELETE on events,
and a statement trigger refuses TRUNCATE.

Policy versions are global too. Put policy adds the next version; a
version's body never changes once put and the version is never removed (a
trigger; only `active` may change, and `active` is left out of the model).

Every set write that would change nothing is refused ("row r1 is already
up", "cursor c1 is already at 2", "the sprint is already stopped"): the op
is refused, its refused event is logged, and no gen moves. In the model that
covers card state, red, row state, row held, the stop and the cursor.
Streams (set stream held), policy activation and the set-card writes on
fields no rule reads are left out, so their same-value refusals are not
modelled.

Needs are a TLA+ set, so a reorder is the same value by construction (the
store keeps them sorted without repeats). An empty name, an unknown role,
and a session without a role (or a role without a session) are `ErrInvalid`
before the store's step and are never logged, so they never reach the
model: structural.

Cards have a kind: `producer`, `sentinel`, or a consumer (`work` or `read`).
A consumer names its producer (`prod`, the design's `of`), takes its
producer's tier at add, and has no needs. A consumer's holder is set at add
and never changes: there is no holder write. Add card refuses a holder on a
producer or a sentinel, and refuses a consumer without one. A producer
carries red (round 17), set by `SetRed` (producers only) and read by
`CardRed`.

A row is sprint state only (round 22): its state, a gen, a held flag, and
the session and role that hooked it. Its kind, width, tiers, model and
harness are configuration, the roster's (nova-config, read each tick), and
never the store's: the store neither caps a row's working consumers nor
checks that a row serves a tier. The deal guards in the card package do
that, against the roster. The store takes any number of consumers of any
tier on a row that exists.

The hook: `PutRow(r, session, role)` is the sprint hook's one write, and
`PutRow(r, None, None)` unhooks. A hook by another session while one holds
the row is refused (unhook first); the same session may hook again with a
new role. Like every row write it raises the row's gen.

The sprint's stop (`stopped`) is one value for the store, as the epoch is.
`SetStopped` sets it, refused when unchanged, and a clear keeps it. The
store refuses nothing because of it: ops still land while stopped (the
reach check NeverDealtStopped shows a deal landing).

Judgements are `open`, `answered` or `expired`: put judgement opens one
(refused if it exists), and answer and expire close it. A close is refused
inside the write unless the judgement is open, so it closes once whatever
the plan carried; `JudgementOpen` still fences a plan. A cursor holds a seq
that set cursor never moves back and never moves past the last event's seq
(events are numbered from 1, refused ones included); an absent cursor reads
as 0.

The conditions are:
- `CardGen`, `CardState`, `HolderIs`, `CardAbsent` and `CardRed` on a card;
- `NeedsLanded`: every need of the card is landed, read from the store in the
  step;
- `RowIs`, `RowGen` and `RowHeld` on a row;
- `JudgementOpen` on a judgement.

An op that writes a card or a row raises its gen by one, however many of its
writes touch it. A card or row the op itself adds stays at gen 0.

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
read the rows, decide, and then write. The witness StoreStaleCheck is
exactly that interleaving: the conditions are judged on an earlier read, so
a1 deals to r1 after e1 set it down.

With the lock, writers run one at a time and each one reads the state the
previous one committed. That serial order is the order of `events` here.

## The instance

6 cards (producers p1, p2; sentinel t1; consumers u1, u2, u3), 2 rows, one
judgement j1, one cursor c1, two policy versions, two bus sessions (x, y),
36 fixed ops. Callers issue their ops in program order (`Pred`). A setup
caller goes first, covering each deterministic rule once:
- s1, s2 hook r1 and r2 by session x as workers, and set them up;
- s3 adds producer p1 with a holder, s4 sentinel t1 with a holder: both
  refused;
- s5 adds p1 (tier pro); s6 adds p2 (needs p1) and t1 (needs p2);
- s7 readies p2 before its need landed (refused), s8 lands p1, s9 readies
  p2;
- s10 adds consumer u3 on r1 and readies it;
- s11 opens judgement j1;
- s12 and s13 put policy versions 1 and 2;
- s14 sets p2 red; s15 (a consumer) and s16 (a sentinel) are refused by the
  red write; s17 sets p2 red again: refused as changing nothing;
- s18 hooks r2 by session y while x holds it: refused (unhook first);
- s19 unhooks r2; s20 hooks it by y as coordinator;
- s21 stops the sprint; s22 stops it again: refused as changing nothing.

Then the callers of one scenario run concurrently. The constant `Scenario`
picks it. The two scenarios touch disjoint tables, so splitting them keeps
each run small without hiding an interleaving.

Scenario `deal` (`Store.cfg`):

| Caller | Ops | What it does |
|---|---|---|
| a | a1, a2 | a1 deals new consumer u1 working to r1, fenced on RowIs(r1, up) and RowHeld(r1, false); a2 marks it done |
| b | b1, b2 | b1 adds u2 ready on r1; b2 makes it working: accepted beside u1, the store holds no width |
| e | e1 | sets r1 down, fenced on RowGen(r1, 0) |
| g | g1 | holds r1, fenced on RowGen(r1, 0) |
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
- streams, policy activation and the seq fence;
- op times, and so the refusal of a close timed before `opened_at`;
- the soft `after` list (it follows the needs rule; no store rule reads it);
- the fields no rule reads. On cards and rows: attempt, author, branch,
  base_sha, prior_head, head, pr, result, bounces, cost, priced, priority,
  brief, paths, done_when, prose, reason, score and flaps, and the stop's
  reason. On judgements: kind, about, question, command, group_key,
  deadline, answer, actor and the times. They add columns, not behaviour.

## Files

- `Store.tla`, `Store.cfg`, `StoreRecords.cfg`: the model and its two
  passing runs.
- `witnesses/Store*`: seventeen reversed witnesses, each `Store.tla` with one
  fault. Only their cfgs are committed (`witnesses/*.cfg`); the modules are
  generated at check time: `python3 make-witnesses.py` writes every witness
  module and every cfg (`Store.cfg`, `StoreRecords.cfg`, `witnesses/*.cfg`,
  `reach/*.cfg`) from `Store.tla` and the fault list in the script. Each
  fault is a set of exact text replacements; one that no longer matches
  stops the script. `gen-witnesses.sh DIR` runs every `make-*witnesses.py`
  in a copy of this directory and is what `ci-tlc.sh` runs first. Edit
  `Store.tla`, rerun the script, commit the cfgs.
- `reach/Never*`: twenty-eight reachability checks on `Store.tla`. Each must
  be violated, which shows the passing runs are not vacuous.

In each witness cfg the target invariant is listed first: when one state
breaks several invariants, TLC names the first in the list.

Retired witnesses:
- `StoreTwoHolders`: it modelled the holder as a set so that a set holder
  write could add a second row. With the holder fixed at add and no holder
  write, SingleStateSingleHolder holds by construction.
- `StoreServesStale`, and with round 22 StoreServesUnchecked and
  StoreTiersNarrowed: the store no longer reads tiers (ServesHeld went).
- With round 22, StoreWidthStale, StoreWidthUnchecked, StoreWidthPerOp,
  StoreWorkEntryUnchecked, StoreWidthCountsDone and StoreWidthCountsReady:
  the store no longer reads width (WidthKept went). StoreWidthInStore now
  shows that a store-side cap is a spurious refusal.

## Running

Run TLC on a bench host, never on the working machine. `ci-tlc.sh` is the
gate. It runs every module in parallel:
- `Store.tla` must pass with `Store.cfg` and with `StoreRecords.cfg` (TLC
  exit 0);
- `CardMachine.tla` must pass with each of its scenario cfgs;
- every generated `witnesses/*.tla` and every `reach/*.cfg` must report a violation
  (TLC exit 12) of the first invariant in its cfg. `reach/Card*.cfg` run on
  `CardMachine.tla`, every other reach cfg on `Store.tla`.

It exits non-zero on any other outcome: a pass where a violation is
required, a violation of a different invariant, a parse error, a crash, or a
run that wrote no result. It uses a pinned, sha256-checked `tla2tools.jar`
(v1.8.0, from this repository's `tla-tools-1.8.0` release), gives each run its own `java.io.tmpdir`, and passes
`-noGenerateSpecTE`, so a rerun in the same tree never sees trace modules.

```
bash ci-tlc.sh
TLC_SHARD=2/6 TLC_CORES=8 bash ci-tlc.sh   # one nightly shard: every sixth run, 8 cores
```

## Invariants

- **TypeOK**: every variable stays in its type.
- **SingleStateSingleHolder**: every card has exactly one state from the
  closed set (or is absent), at most one holder, and that holder is a row of
  this epoch.
- **ProducerNeverHeld**: no producer or sentinel has a holder.
- **RedOnlyOnProducers**: only a producer is ever red.
- **ConsumersWellFormed**: a consumer names an existing producer, has no
  needs, has a holder, and has its producer's tier; a producer or sentinel
  names none.
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
  stricter than the design (a width cap, say) is caught.
- **GenPerOp**: each accepted op raises the gen of each card and row it
  writes that existed before it by exactly one; one it creates is at 0; a
  clear raises every carried row's gen by one.
- **ClearCarriesRows**: a clear keeps every row's session and role, sets it
  down and not held, leaves no card or judgement, and keeps every cursor,
  the policies and the stop.
- **HookKept**: no accepted op moves a row from one session straight to
  another; it is unhooked between.
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
| `StoreEpochUnfenced` (records) | ops other than clear are not epoch-checked | OpsInTheirEpoch violated |
| `StoreProducerHolds` | add card accepts a holder on a producer or sentinel | ProducerNeverHeld violated |
| `StoreSentinelHolds` | add card refuses a holder on a producer but not a sentinel | ProducerNeverHeld violated |
| `StoreRowStale` | RowGen is not checked | ConditionsAtomic violated |
| `StoreRedOnConsumer` | set red accepts a consumer | RedOnlyOnProducers violated |
| `StoreWidthInStore` | the store caps a row at one working consumer | NoSpuriousRefusal violated |
| `StoreHookStolen` | put row lets another session hook a hooked row | HookKept violated |
| `StoreHoldCarried` | a clear carries a row's hold | ClearCarriesRows violated |
| `StoreStopDropped` | a clear resets the stop | ClearCarriesRows violated |
| `StoreJudgementTwice` (records) | answer and expire refuse only an absent judgement | JudgementClosedOnce violated |
| `StoreCursorBack` (records) | set cursor accepts a lower value | CursorMonotone violated |
| `StoreCursorPastLog` (records) | set cursor is not bounded by the log | CursorWithinLog violated |
| `StoreLogTampered` | an UPDATE on events (no append-only trigger) | LogAppendOnly violated |
| `StorePolicyRewritten` | put policy writes the latest version, not the next | PolicyImmutable violated |
| reach `NeverTaken` | none | a1 is accepted |
| reach `NeverRecovered` | none | a caller left unknown reaches done |
| reach `NeverStaleEpochRefused` | none | an old-epoch op after the clear is refused |
| reach `NeverNeedsReady` | none | s9 passes NeedsLanded |
| reach `NeverNewEpochOp` | none | k2 is accepted in epoch 2 |
| reach `NeverTwoWorking` | none | r1 holds two working consumers (u1, u2) |
| reach `NeverProducerRefused` | none | s3 is refused |
| reach `NeverSentinelRefused` | none | s4 is refused |
| reach `NeverHoldReleased` | none | k1 opens epoch 2 with g1's hold on r1 released |
| reach `NeverHeldRefused` | none | a1 is refused on RowHeld after g1 |
| reach `NeverRowDown` | none | e1 sets r1 down |
| reach `NeverRowFenced` | none | e1 is refused on a stale RowGen |
| reach `NeverTwoPolicies` | none | s13 adds policy version 2 |
| reach `NeverRed` | none | s14 sets p2 red |
| reach `NeverRedKindRefused` | none | s15 and s16 are refused by the red write |
| reach `NeverSameRedRefused` | none | s17 is refused as changing nothing |
| reach `NeverHookRefused` | none | s18 is refused: x holds r2 |
| reach `NeverRehooked` | none | s20 hooks r2 by y as coordinator |
| reach `NeverDealtStopped` | none | a1 lands while the sprint is stopped |
| reach `NeverSameStopRefused` | none | s22 is refused as changing nothing |
| reach `NeverAnswered` (records) | none | j1 is answered |
| reach `NeverJudgementFenced` (records) | none | a second fenced close of j1 is refused on JudgementOpen |
| reach `NeverCursorRefused` (records) | none | m2 is refused below c1's value |
| reach `NeverCloseRefused` (records) | none | the unfenced h2 is refused by the write after j1 closed |
| reach `NeverCursorPastRefused` (records) | none | m3 is refused past the log |
| reach `NeverCursorCarried` (records) | none | c1 keeps its value across the clear |

## Results, tla2tools v1.8.0 (the CI jar) on hetzner2, 2026-11-06 (round 22)

`ci-tlc.sh` ran twice. Run 2 (at be090aa7) gave 43 of the 44 store results
as required and one FAIL: StoreEpochUnfenced passed. Its fault no longer had
an op to land: the deal scenario's only unfenced epoch-1 op (v1, a tiers
write) went with tiers, and every other epoch-1 deal op is refused in epoch
2 by a fence or a write rule (the clear raises r1's gen and sets it down,
and leaves p2 behind, which b1's consumer names). The witness now runs the
records scenario, where the unfenced m1 can land after the clear. Run 3 (at
6f525549, a comment-only change to `Store.tla` besides that) was green: 87
ok, 0 FAIL, exit 0, the 44 store results among them. The numbers below are
from run 2's logs (`Store.tla`'s behaviour is the same at both commits);
run 3's working directory is removed by the script on exit, so the
StoreEpochUnfenced row has only its result line.
The StoreHoldCarried and NeverHoldReleased rows are from 2026-11-08, when
a clear began releasing holds; every store run was repeated then (48 ok, 0
FAIL), Store passing at 120730 / 70841 states, depth 36.

| Module | Result | States generated / distinct | Depth | Time |
|---|---|---|---|---|
| `Store` | No error has been found | 120727 / 70838 | 33 | 01min |
| `StoreRecords` | No error has been found | 74687 / 39165 | 31 | 53s |
| reach `NeverAnswered` | violated | 27 / 27 | 24 | 06s |
| reach `NeverCloseRefused` | violated | 57 / 54 | 25 | 06s |
| reach `NeverCursorCarried` | violated | 78 / 73 | 25 | 07s |
| reach `NeverCursorPastRefused` | violated | 32 / 32 | 24 | 06s |
| reach `NeverCursorRefused` | violated | 84 / 78 | 25 | 06s |
| reach `NeverDealtStopped` | violated | 24 / 24 | 24 | 05s |
| reach `NeverHoldReleased` | violated | 103 / 98 | 29 | 01s |
| reach `NeverHeldRefused` | violated | 72 / 68 | 25 | 05s |
| reach `NeverHookRefused` | violated | 19 / 19 | 19 | 06s |
| reach `NeverJudgementFenced` | violated | 58 / 55 | 25 | 05s |
| reach `NeverNeedsReady` | violated | 10 / 10 | 10 | 03s |
| reach `NeverNewEpochOp` | violated | 86 / 80 | 25 | 06s |
| reach `NeverProducerRefused` | violated | 4 / 4 | 4 | 03s |
| reach `NeverRecovered` | violated | 40 / 40 | 25 | 04s |
| reach `NeverRed` | violated | 15 / 15 | 15 | 05s |
| reach `NeverRedKindRefused` | violated | 17 / 17 | 17 | 05s |
| reach `NeverRehooked` | violated | 21 / 21 | 21 | 05s |
| reach `NeverRowDown` | violated | 28 / 28 | 24 | 06s |
| reach `NeverRowFenced` | violated | 76 / 71 | 25 | 07s |
| reach `NeverSameRedRefused` | violated | 18 / 18 | 18 | 05s |
| reach `NeverSameStopRefused` | violated | 23 / 23 | 23 | 04s |
| reach `NeverSentinelRefused` | violated | 5 / 5 | 5 | 03s |
| reach `NeverStaleEpochRefused` | violated | 80 / 75 | 25 | 05s |
| reach `NeverTaken` | violated | 24 / 24 | 24 | 04s |
| reach `NeverTwoPolicies` | violated | 14 / 14 | 14 | 04s |
| reach `NeverTwoWorking` | violated | 109 / 94 | 26 | 04s |
| `StoreCursorBack` | Invariant CursorMonotone is violated | 84 / 78 | 25 | 08s |
| `StoreCursorPastLog` | Invariant CursorWithinLog is violated | 32 / 32 | 24 | 07s |
| `StoreEpochUnfenced` | Invariant OpsInTheirEpoch is violated (run 3; see below) | | | |
| `StoreHookStolen` | Invariant HookKept is violated | 19 / 19 | 19 | 05s |
| `StoreJudgementTwice` | Invariant JudgementClosedOnce is violated | 57 / 54 | 25 | 06s |
| `StoreLogTampered` | Invariant LogAppendOnly is violated | 4 / 4 | 3 | 03s |
| `StoreNoEvent` | Invariant LogMatchesState is violated | 2 / 2 | 2 | 02s |
| `StorePolicyRewritten` | Invariant PolicyImmutable is violated | 14 / 14 | 14 | 04s |
| `StoreProducerHolds` | Invariant ProducerNeverHeld is violated | 4 / 4 | 4 | 03s |
| `StoreRedOnConsumer` | Invariant RedOnlyOnProducers is violated | 16 / 16 | 16 | 04s |
| `StoreReplayApplies` | Invariant IdempotentById is violated | 40 / 40 | 25 | 07s |
| `StoreRowStale` | Invariant ConditionsAtomic is violated | 68 / 64 | 25 | 07s |
| `StoreSentinelHolds` | Invariant ProducerNeverHeld is violated | 5 / 5 | 5 | 03s |
| `StoreStaleCheck` | Invariant ConditionsAtomic is violated | 415 / 279 | 49 | 14s |
| `StoreHoldCarried` | Invariant ClearCarriesRows is violated | 103 / 98 | 29 | 01s |
| `StoreStopDropped` | Invariant ClearCarriesRows is violated | 30 / 30 | 24 | 07s |
| `StoreWidthInStore` | Invariant NoSpuriousRefusal is violated | 109 / 94 | 26 | 09s |

The counterexamples were checked by hand against the event ids of each
last state. Every one starts with the setup (s1..s22, with s3, s4, s7,
s15..s18 and s22 refused as designed), then:
- **StoreNoEvent:** s1 lands with no event.
- **StoreLogTampered:** s1's event is updated from accepted to refused.
- **StorePolicyRewritten:** s13 overwrites version 1's body (in setup).
- **StoreProducerHolds / StoreSentinelHolds:** s3 (producer p1 held by r1)
  and s4 (sentinel t1 held by r1) are accepted.
- **StoreRedOnConsumer:** s15 sets the consumer u3 red (in setup).
- **StoreHookStolen:** s18 hooks r2 by y while x holds it (in setup).
- **StoreReplayApplies:** a1 is accepted, its reply is lost, and the retried
  Apply runs a1 again (refused, u1 exists) and logs it a second time.
- **StoreStaleCheck:** a1 begins with r1 up, e1 sets r1 down, a1 lands.
- **StoreRowStale:** e1 lands, then g1 lands on RowGen(r1, 0) although e1
  raised r1's gen to 1.
- **StoreWidthInStore:** a1 deals u1 working to r1, b1 adds u2 ready, b2 is
  refused by the store's cap: no rule of the design refuses it.
- **StoreHoldCarried:** after g1 held r1, k1 opens epoch 2 with r1 still
  held (the defect of 2026-11-08).
- **StoreStopDropped:** after s21 stopped the sprint, k1 opens epoch 2
  running.
- **StoreJudgementTwice:** h1 answers j1, then the unfenced h2 answers it
  again.
- **StoreCursorBack:** m1 sets c1 to 2, then m2 sets it to 1.
- **StoreCursorPastLog:** m3 sets c1 to 40 with 22 events in the log.
- **StoreEpochUnfenced:** run 3 named OpsInTheirEpoch; its trace was not
  kept. By the model the shortest is k1 clears, then the epoch-1 m1 sets c1
  to 2 in epoch 2.

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
consumer. Start and AskRead cut the consumer working (the deal is the take,
defect 52), guarded by `RoomOn`: the row's working consumers, less a
preempted victim, below its width (the dealer's check in layer 3). A producer carries `review`, its count of entries into
review (Finished and Red raise it); a read carries the producer's `review` at
cut, and ReadYes needs a done ok read (ReadFix a done fix read) of the
current head whose `review` equals the producer's.

The rulings modelled: a Withdraw of the working producer's work consumer
releases the producer in the same step (Preempt's Withdraw too), so
Released and HoldCard from working are kept as the tables have them but are
never enabled; Broken, Faulted, ReadFix, Released and that release write the
producer parked (waiting) when the stream is held; a work consumer reports
ok, fail or fault, a read ok, fix or fault; an ok work head is neither the
base nor the attempt's prior_head (the producer's head at Start) once a work
consumer finished at it (a prior head pushed and never finished may be
finished); AskRead
refuses a row already holding a live read of this head; Replace names an
existing other card; a sentinel takes no Start, AskRead, Edit or Rescore,
and takes SetPriority and Relink; Relink's target is a producer (a
sentinel too since round 4); there is no Take; preemption withdraws a
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
an already-read head (HoldCard and UnholdCard out of review, an attempt withdrawn
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
- Rulings 5 and 10 (wording) and 12 (the walk, the PG mirror, modules/card/README)
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
- `witnesses/Card*.tla`: forty-one reversed witnesses; `reach/Card*.cfg`:
  twenty reachability checks. Generated with every card cfg by
  `python3 make-card-witnesses.py` from `CardMachine.tla` and the fault list
  in the script, in the same shape as `make-witnesses.py`: a replacement
  that no longer matches stops the script.

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
  Released or HoldCard.
- **ParkedUnderHold**: no producer is ready in a held stream after any step.
- **NeverStranded**: every non-final producer with no live consumer has a
  move of its own enabled (not counting the coordinator's Cancel, Replace,
  HoldCard, Edit, Rescore, SetPriority), or waits on a need that can still
  move, an after-target that is merging, or (waiting, ready, review) a row
  coming back, a hold lifting, or a full row's room (the deal is the take). A ready producer at the attempt bound, or
  one with no slot left, is exempt as the model's limit.
- **ReadOnlyInReview**: a live read only in review, of the current head and
  review count.
- **ReaderNeverAuthor**: a live read's holder is never the producer's author.
- **WidthKept**: working consumers per row ≤ width.
- **DealIsTake**: no consumer is ever `ready`: a cut consumer is working,
  in its row's hand (defect 52; replaces TakeBelowWidth).
- **NeedFailedTakesDependants**: when a need has failed and the dependant is
  not final, NeedFailed is enabled.
- **SentinelShape**: a sentinel is only absent (before Add), waiting, landed
  or fail (it never has a consumer).
- **NeedsAcyclic**: the needs graph stays acyclic.
- **OrderAcyclic**: needs and after together stay acyclic.
- **RefutedNeverRedealt**: a refuted card is never dealt again until the
  coordinator acts (live 2026-11-09: one card refuted at w24, w25 and w26).
  A work consumer's fail whose reason refutes its brief (result `refuted`,
  `card.Refuted`) leaves its producer waiting with `why` held, a bounce
  spent; the producer is ready again with that consumer current only after
  an UnholdCard, which set its bounce base to its bounces.
- **ResultsByKind**: a work consumer's result is ok, fail (refuted, a fail
  refuting its brief), fault or route; a read's
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
- **OnlyHolderActs**: Report only from the consumer's holder.
- **RouteFaultWaits**: a work consumer's fault naming a missing route
  (result `route`) moves its producer to waiting with the missing-route
  reason (`why` route), never to ready (defect 48). A card with a problem
  has no state of its own (Glenn, 2026-11-07: "waiting is where stuff goes where it's having
  problems"): a producer with a problem waits, its `why` saying what.
- **HeldNeverDealt**: a producer whose `why` is held or route is waiting;
  Resolve and WaiveRed refuse it, so the deal never takes it until
  UnholdCard lifts it.
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
- **HoldNoLive**, **EditNoLive**: HoldCard and Edit only with no live consumer.
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
| `witnesses/CardFaultBounces` | life | Faulted raises bounces (a fault counted toward the hold) | FaultSpendsNothing |
| `witnesses/CardEditUnderLive` | life | Edit under a live consumer | EditNoLive |
| `witnesses/CardCutReady` | life | Start cuts its consumer ready | DealIsTake |
| `witnesses/CardDealOverWidth` | pre | Start has no room guard | WidthKept |
| `witnesses/CardRouteRedealt` | life | a route fault goes back to ready | RouteFaultWaits |
| `witnesses/CardRefutedRedealt` | life | a fail refuting its brief goes back to ready, dealt again | RefutedNeverRedealt |
| `witnesses/CardHeldDealt` | life | Resolve takes a held producer | HeldNeverDealt |
| `witnesses/CardUnholdBudgetSpent` | life | UnholdCard leaves the bounce base, so the limit holds it again | UnholdFreshBudget |
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
| `witnesses/CardMergeCutUnseen` | merge | a consumer cut earlier in the op is unseen by the next move | HoldNoLive |
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
| `reach/CardNeverHeld` | life | none | violated: a producer waits on a hold |
| `reach/CardNeverUnheldBounced` | life | none | violated: a producer held after a bounce is unheld with a fresh budget |
| `reach/CardNeverRouteHeld` | life | none | violated: a route fault leaves its producer waiting on it |
| `reach/CardNeverRefutedHeld` | life | none | violated: a fail refuting its brief leaves its producer waiting held on it |
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

## Results, the CI jar on hetzner2, 2026-11-07 (round 7, card/ at v2-card-fix4 4bb1a43b)

Every run (store and card) in one parallel pass (each JVM
at `-Xmx3g`), the passing runs on 4 workers each and the rest on 1, on a
host other users' TLC jobs were also loading, so the times are under full
contention. CardDeps was OOM-killed in that pass (exit 137, the host short
of memory) and passed rerun alone at `-Xmx8g`. Of the card runs: 7 passing
scenarios pass, 36 witnesses and 17 reach checks each report their target
invariant violated (60 runs).

| Module | Result | Generated / distinct | Depth | Time |
|---|---|---|---|---|
| `CardMachine` (life) | No error has been found | 5808819 / 1359931 | 24 | 3min 00s |
| `CardDeps` (deps) | No error has been found | 21525714 / 4031516 | 25 | 22min 30s (rerun alone) |
| `CardPreempt` (pre) | No error has been found | 5688130 / 943790 | 27 | 2min 58s |
| `CardReads` (reads) | No error has been found | 79927 / 27674 | 17 | 30s |
| `CardMerge` (merge) | No error has been found | 220392 / 41428 | 17 | 32s |
| `CardMergePre` (mergepre) | No error has been found | 7102298 / 1054188 | 26 | 2min 50s |
| `CardMergeDeps` (mergedeps) | No error has been found | 216520 / 50135 | 25 | 45s |
| `CardAuthorReads` | ReaderNeverAuthor violated | 10705 / 3788 | 7 | 29s |
| `CardDottedId` | NeverStranded violated | 395 / 374 | 3 | 18s |
| `CardEditUnderLive` | EditNoLive violated | 442 / 232 | 4 | 15s |
| `CardFailRepeatsPrior` | HeadNeverRepeats violated | 26661 / 9341 | 8 | 30s |
| `CardFaultBounces` | FaultSpendsNothing violated | 4978 / 1572 | 6 | 26s |
| `CardFaultBumpsAttempt` | FaultSpendsNothing violated | 4978 / 1572 | 6 | 22s |
| `CardHandPlanPartial` | AttemptOnlyOnStart violated | 820 / 367 | 4 | 18s |
| `CardHeldDealt` | HeldNeverDealt violated | 175 / 155 | 5 | <1s (2026-11-07, hetzner2) |
| `CardHeldStreamReady` | ParkedUnderHold violated | 10428 / 3649 | 7 | 27s |
| `CardInThisOpAlone` | InThisOpOnFailedNeed violated | 22 / 10 | 2 | 11s |
| `CardLandedMoves` | FinalNeverMoves violated | 56 / 46 | 3 | 9s |
| `CardMergeCutUnseen` | HoldNoLive violated | 40759 / 10276 | 8 | 30s |
| `CardMergeNestedHold` | ParkedUnderHold violated | 3470 / 1082 | 5 | 20s |
| `CardMergeNestedStart` | PreemptOnBlockerRow violated | 50919 / 13081 | 8 | 33s |
| `CardMergeNoRerun` | FinalNeverMoves violated | 190 / 138 | 3 | 15s |
| `CardMergeOwnWrites` | FencesOnSnapshot violated | 123 / 95 | 3 | 12s |
| `CardMergeReviewTwice` | ReviewOnEntry violated | 7632 / 2166 | 6 | 24s |
| `CardMergeSecondWrite` | MergeParksResolved violated | 7 / 5 | 2 | 9s |
| `CardNeedFailedIgnored` | NeedFailedTakesDependants violated | 6 / 4 | 2 | 13s |
| `CardNeedsCycle` | NeedsAcyclic violated | 280 / 255 | 3 | 19s |
| `CardNonHolderReports` | OnlyHolderActs violated | 2074 / 794 | 5 | 23s |
| `CardPlainWithdrawGuarded` | PlainWithdrawUnguarded violated | 17436 / 4849 | 7 | 28s |
| `CardPriorRepeated` | HeadNeverRepeats violated | 26661 / 9341 | 8 | 31s |
| `CardReadFixEmpty` | FixSaysWhat violated | 200192 / 67405 | 10 | 54s |
| `CardReadFixOnZero` | ReadFixNeedsFix violated | 10703 / 3786 | 7 | 28s |
| `CardReadReportsFail` | ResultsByKind violated | 84689 / 29482 | 9 | 42s |
| `CardReadyCounted` | TakeBelowWidth violated | 36 / 31 | 3 | 10s |
| `CardRedAdvances` | RedNeverAdvances violated | 408537 / 130809 | 11 | 1min 18s |
| `CardRelinkAfterCycle` | OrderAcyclic violated | 7380 / 3256 | 4 | 27s |
| `CardReplaceByFinal` | ReplaceByLive violated | 22 / 20 | 3 | 9s |
| `CardStaleReport` | OldAttemptNeverMoves violated | 30764 / 10902 | 8 | 34s |
| `CardTwoReadsOneRow` | OneLiveReadPerRow violated | 31941 / 11338 | 8 | 34s |
| `CardTwoWorkConsumers` | WorkingOneWorkConsumer violated | 439 / 229 | 4 | 19s |
| `CardWaiveRedWithoutRead` | WaiveRedNeedsPriorRead violated | 1719999 / 476019 | 14 | 3min 09s |
| `CardWaiveRedWithoutRed` (redw) | WaiveRedNeedsRed violated | 977451 / 438683 | 20 | 3min 02s |
| `CardWithdrawLeavesWorking` | WithdrawReleasesInOneStep violated | 439 / 229 | 4 | 16s |
| `CardYesOnZero` | ReadYesNeedsYes violated | 10703 / 3786 | 7 | 25s |
| reach `CardNeverAdded` | NeverAdded violated | 27 / 21 | 2 | 14s |
| reach `CardNeverAskReads` | NeverAskReads violated | 1333 / 666 | 7 | 16s |
| reach `CardNeverHeld` | NeverHeld violated | 164 / 150 | 5 | <1s (2026-11-07, hetzner2) |
| reach `CardNeverFaultRedealt` | NeverFaultRedealt violated | 10458 / 3662 | 7 | 24s |
| reach `CardNeverFindingCarried` | NeverFindingCarried violated | 408827 / 130955 | 11 | 1min 07s |
| reach `CardNeverFixed` | NeverFixed violated | 200192 / 67405 | 10 | 46s |
| reach `CardNeverInThisOp` | NeverInThisOp violated | 12 / 6 | 2 | 9s |
| reach `CardNeverInThisOpMerged` | NeverInThisOpMerged violated | 93 / 33 | 3 | 9s |
| reach `CardNeverLanded` | NeverLanded violated | 408529 / 130804 | 11 | 1min 06s |
| reach `CardNeverNeedFailed` | NeverNeedFailed violated | 278 / 253 | 3 | 14s |
| reach `CardNeverPreempted` | NeverPreempted violated | 9615 / 2863 | 7 | 25s |
| reach `CardNeverRed` | NeverRed violated | 408537 / 130809 | 11 | 1min 04s |
| reach `CardNeverRelinked` | NeverRelinked violated | 14 / 8 | 2 | 12s |
| reach `CardNeverSentinelNeeded` | NeverSentinelNeeded violated | 19 / 13 | 2 | 11s |
| reach `CardNeverSentinelReached` | NeverSentinelReached violated | 747709 / 229623 | 12 | 1min 31s |
| reach `CardNeverWaivedRed` | NeverWaivedRed violated | 747797 / 229675 | 12 | 1min 31s |
| reach `CardNeverYesAfterRed` | NeverYesAfterRed violated | 2227352 / 591860 | 15 | 3min 08s |

The counterexamples were checked by hand. Up to
round 4, each move the fault lets through is one card/ at 4a085774 refuses:
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
  of it says ok, HoldCard and UnholdCard take p1 out of review with no Red;
  attempt 2 (prior 1) is withdrawn with head 2, releasing p1 at head 2;
  attempt 3 (prior 2) reports ok with head 1 and Finished brings review 2
  at head 1. WaiveRed then merges on the review-1 read with no Red recorded
  (the code: "no red on review 2"; every other check passes there: HoldCard
  from review with no live consumer, the withdrawn head written by
  Released, an ok head that is neither base nor prior).
- **CardMergeCutUnseen, CardMergeNestedHold, CardMergeNoRerun,
  CardMergeOwnWrites, CardMergeNestedStart** (rounds 5 and 6): AskRead then
  HoldCard in one op with the read unseen; HoldStream then a Withdraw leaving
  p1 ready in a held stream; Cancel then a merged Rescore of the failed
  card; Resolve then Cancel fencing p1 ready against a waiting snapshot;
  a blocker Start on r2 then a preempt Withdraw of p1.1 working on r1 in
  one op.

Trace check (`make-traces.sh check` on hetzner2, this model, card/ at
4bb1a43b): card-walk-1..20 all TRACE OK (65 to 114 steps each), the
card-breaker refused at 3 of 3 as expected, and the store traces as before.
`CardMachineTrace.tla` sets the Merge bookkeeping (`op`, `snap`, `pw`, `bs`)
at rest in TraceInit and leaves it unchanged on every record (each record
is one op of one move), and `MaxOp` is read off the scenario, so the
walk's generated cfg needs no new constant. The walk records single moves
only: it never merges, uses InThisOp, adds a dotted id or gives ReadFix an
empty finding, so it cannot see the round 7 divergences below.

Round 7, checked by hand against card/ at v2-card-fix4 (b2b0b8c2; 4bb1a43b
changes no move in card/, only adds the trace emitter). Each is a move the
round 7 rulings refuse; "fix4 accepts" marks a divergence the code still has:
- **CardDottedId:** Resolve p1, Add `p1.w1` (a sentinel, no needs); p1 is
  ready and every Start would cut `p1.w1`, which the CardAbsent fence
  refuses: p1 is stranded. fix4 accepts the Add (addShape has no dot check).
- **CardInThisOpAlone:** NeedFailed{InThisOp} of p2 on its need p1, alone,
  with p1 waiting: p2 fails on a live need. fix4 accepts it (failedNeed
  returns nil for InThisOp unless the need is landed).
- **CardPlainWithdrawGuarded:** p1.1 works on r1; the blocker p2 is started
  on r2, opening an op; a plain Withdraw of p1.1 is refused by the guard.
  fix4 does refuse it (preemptGuard pairs every Withdraw with a blocker
  Start: "consumer p1.1 is on r1; blocker p2 is dealt to r2").
- **CardMergeSecondWrite:** Resolve p1 opens an op; HoldStream would write
  p1's state again and is refused. fix4 refuses Merge(Resolve, HoldStream)
  by dupWrites, the second-write refusal ruling 4 removes.
- **CardReadFixEmpty:** deal, finish, a read says fix, ReadFix with no
  finding: p1 is ready with no finding to carry. fix4 accepts it (readFix
  writes a finding only when one is given).
- **CardFailRepeatsPrior:** attempt 1 is withdrawn with head 1, attempt 2
  (prior 1) reports fail with head 1. fix4 refuses it (reportHead checks a
  given head against the prior for every result).
- **CardMergeReviewTwice:** attempt 1 reports ok opening an op, Finished in
  the same op takes review from 0 to 2.
- **CardHandPlanPartial:** Start p1 opens an op; SetPriority in it writes a
  record whose other fields are zero: attempt 1 goes back to 0.
- **reach NeverInThisOpMerged:** Cancel p1 opens an op; NeedFailed{InThisOp}
  of p2 on p1 is the next move.

- **reach:** Landed runs deal, finish, read ok, ReadYes, Landed. Fixed ends
  review → ready with bounces 1. Red goes merging → review with review 2.
  YesAfterRed then cuts a new read at review 2 and merges on it; WaivedRed
  merges at review 2 on the review-1 read of the same head. FaultRedealt:
  fault, Faulted, Start of attempt 2; FindingCarried: a read says fix, ReadFix,
  Start of attempt 2 carrying the finding. Held: HoldCard from ready. SentinelReached: p1
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
3. **Needs**: Resolve in id order: needs landed, stream not held. Never a
   function of rows, and never of paths: two producers that name one file
   both start (only a NEEDS orders starts; the overlap hold was removed
   2026-11-08).
4. **Fix**: ReadYes or ReadFix (the default verdict) once every read of the
   current review is final and one is done.
5. **Deal and reads**: blocker work, every read owed, normal work, in id
   order; a row is dealt while its working consumers are under its width,
   with the in-tick tally (the deal is the take, defect 52: the cut is
   working). The row is policy's `rows`
   answer when it is eligible for the candidate, else the default: the
   desired rank exactly (the card's tier; one up once its bounces reach
   `escalate_after`; for a read, the rank above its author's row), then
   above it, then an idle lane, most room, name. A row below the card's
   tier is never eligible. An answer that is not eligible is a `rows` miss
   (one `policy` judgement), except an offered row named while no row has
   room: the candidate waits, and that is no miss (`FullAnswer`).
   No open serving row: `unserved`; a review no
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
(rank 2). There is no `ahead` since the deal became the take.

| Scenario | Cards | Start | Environment | Constants |
|---|---|---|---|---|
| pre | b blocker (s2), p, q (s1) | all waiting, s2 held, r1 down | lift s2's hold; r2 down and up | K 2, MaxAtt 2, MaxEnv 2, MaxArrive 2, Budget 2, Pack 2 |
| pre1 | as pre | as pre | as pre | as pre, Pack 1 |
| deal | p, q (s1, free to start together), b (s2, tier 2, needs p) | all waiting | r1 down and up | K 4, MaxAtt 2, MaxBounce 1 (p only), MaxEnv 1, MaxArrive 1, Budget 2, Pack 2 |
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
| `VerdictHasItsRead` | Fix | a yes has a done ok read of the review and no fix read, a fix a done fix read; a faulted read is neither |
| `NeverDealtAbsent` | D1' (2026-11-07) | no accepted Start, read or preemption lands on a row absent from nova-bus (`bus`), whatever the store's up says |

`ReadyDealtWhenRoom` (guarantee 8's second half, LAYER-2's nothing stalls),
under weak fairness on `Snap` and `Tick` only (rows need not take or
report): a ready producer with room on an open serving row (under its
width) is eventually dealt, unless the room goes.

`FaultedReadReadAgain`, under the same fairness: a review whose done reads
all faulted (a faulted read is not a read: it is owed again and the
verdict is retry, which moves nothing) is eventually read again, or
leaves review, unless the room for a read goes.

`DealtWithinKTicks` (intent rule 33, an invariant over the counters `rdy`
and `ju`): a producer ready for `UndealtTicks` (2) ticks with an open
serving row with room is dealt, or has an open undealt judgement (`ju`).
`DealIsTake`: no consumer is ever `ready`. The model's bounds
(attempts, slots) are exempt, as in `CardMachine`.

## Expected results

| Module | Scenario | Fault | Expected result |
|---|---|---|---|
| `StepsPre` / `StepsPrePack1` / `StepsDeal` / `StepsLand` / `StepsBurst` | pre / pre1 / deal / land / burst | none | pass, liveness included |
| `witnesses/StepsDealtTwice` | pre | the deal does not skip a blocker the preemption claimed, and the preemption fires with free width | NeverDealtTwice |
| `witnesses/StepsTwoGroups` | land | the land-result step ignores claimed cards | OneGroupPerCard |
| `witnesses/StepsWidthOver` | burst | the deal checks no width | WidthKept |
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
| `witnesses/StepsTierUnchecked` | deal | the tier checked nowhere (answer and Start) | NeverBelowTier |
| `witnesses/StepsAfterIgnored` | land | the land step ignores after | AfterKept |
| `witnesses/StepsAfterHolds` | land | the land step holds on any target not landed | AfterNeverHolds |
| `witnesses/StepsPolicyStall` | deal | a failed rows answer plans nothing | PolicyFailsToDefault |
| `witnesses/StepsOldestVictim` | pre | the victim is the oldest | PreemptYoungest |
| `witnesses/StepsDealOneShort` | pre | the deal leaves one slot of width unused | ReadyDealtWhenRoom (liveness, TLC exit 13) |
| `witnesses/StepsCutReady` | pre | the deal cuts its consumer ready | DealIsTake |
| `witnesses/StepsUndealtSilent` | burst | no undealt judgement | DealtWithinKTicks |
| `witnesses/StepsLandAfterHoldsUntilLanded` | land | the after hold lasts until the earlier card lands | MergingCovered |
| `witnesses/StepsDealsWithoutPolicy` | pre | the policy gate ignored | NoDealWithoutPolicy |
| `witnesses/StepsResolveOnEvent` | deal | resolve only on a landed result's event | ResolveFromState |
| `witnesses/StepsYesOnFault` | deal | the verdict default is yes unless a read said fix | VerdictHasItsRead |
| `witnesses/StepsDealtAbsent` | deal | the deal reads the store's up as presence | NeverDealtAbsent |
| `witnesses/StepsFaultCounted` | deal | a faulted read counts as a read | FaultedReadReadAgain (liveness, TLC exit 13) |
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
| `reach/StepsNeverRed` | land | none | violated: the coordinator's fix sends a judged producer to fix (Red) |
| `reach/StepsNeverAfterHeld` | land | none | violated: a landing waits on a queued after target |
| `reach/StepsNeverCarried` | burst | none | violated: the budget carries inbox items |
| `reach/StepsNeverUndealt` | burst | none | violated: an undealt judgement is opened |
| `reach/StepsNeverLandJudged` | land | none | violated: a land judgement is opened |
| `reach/StepsNeverGated` | pre | none | violated: the policy gate opens no-policy |
| `reach/StepsNeverBothStarted` | deal | none | violated: p and q, naming one file, both started |
| `reach/StepsNeverHandLanded` | deal | none | violated: a store op lands a producer by hand |
| `reach/StepsNeverReadAgain` | deal | none | violated: a review whose read faulted is read again |
| `reach/StepsNeverAbsent` | deal | none | violated: a tick runs with an up row off the bus serving a ready card |

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
  of the model's four scenarios on `store.Fake` (`modules/steps/trace_test.go`), each
  step one of the model's actions (Snap, Tick, Take, Report, Hold, Unhold,
  Gone, Seen, EdgeResult) picked at random, each recorded with the model's
  variables projected from the code's state by the refinement mapping in
  `modules/steps/trace.go` (`StoreTLA`, `InboxTLA`, `InflightTLA`, `JudgementsTLA`;
  the mapping is in that file's comment). A record logs `w` in parts (`ps`,
  `up`, `sh`, `tko` whole and `cs` as the slots that are not `none`, which
  `StepsTrace.Parts` checks), the inbox, the in-flight requests and the
  open judgements of the model's three kinds, and a Tick's `answer`. The
  tick plans on the view as Snap left it, so a move between Snap and Tick
  races it as in the model. The cfg sets the model's bounds above anything a
  walk reaches (K 60, MaxAtt 30, MaxBounce 30, MaxEnv and MaxArrive 1000)
  and the scenario's Budget and Pack. `TestTraceFixtures` writes the fixed
  seed set (4 scenarios x 3 seeds x 150 steps) under `NOVA_TRACE_DIR`; the CI
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
retire and fail limits (HoldCard at the bounce limit, Withdraw on silence,
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
# Trace validation (binding the code to the models)

A model checks the design; trace validation checks that the code is that
design. The Go walks record every step they take, projected onto the model's
variables, and TLC checks that each recording is a behaviour of the model.
The shape is that of `StepsTrace.tla` (the Kuppe/Merz pattern): the trace is
a generated constant module `<Model>TraceLog.tla` defining
`TraceLog == << r_1, ..., r_n >>`; the trace module has one variable `l`, the
index of the last record matched; `TraceNext` takes record `l + 1` through the
action it names and requires the projected state after it; the property is
`TraceMatched == <>(l = Len(TraceLog))`. TLC exit 0 is a matched trace; exit
13 (TraceMatched violated) leaves `l = k` in the last state, so record `k + 1`
is the step the model refuses.

Each record carries only the entries that changed since the record before; an
entry not named must be unchanged, so every step still pins the whole
projected state.

## Running

On hetzner2 (never the studio), from the repository root:

    bash tla/make-traces.sh          # the walks write their traces under tla/traces/
    bash tla/make-traces.sh check    # ... and TLC checks every trace, in parallel
    bash tla/check-trace.sh Store tla/traces/store-fake-1
    bash tla/check-trace.sh CardMachine tla/traces/card-walk-3

`check-trace.sh` prints `TRACE OK steps=<n>`, or
`TRACE REFUSED at k of n: <action> (...)` followed by the model's state before
the step and record k. Each trace's cfg has an `\* expect:` line (`matched` or
`forbidden at K`); the script exits 0 when the outcome is the expected one, 1
when it is not, 2 on any other TLC failure. The CI job `trace` runs
`make-traces.sh check <group>` on every push, one job per group (store, card,
policy, edge, and the steps traces in five parts: steps-pre, steps-deal,
steps-land, steps-burst, steps-fixtures) on its own runner, with `NOVA_TRACE_PG=1` so a missing
initdb fails the job instead of skipping the PostgreSQL walk.

The walks write traces only when `NOVA_TRACE_DIR` names a directory
(`make-traces.sh` sets it); without it the test suite is unchanged. The walk
traces (`store-fake-*`, `store-pg-*`, `card-walk-*`) are regenerated, not
committed. The committed fixtures are the breakers (`store-breaker`, `card-breaker`,
`policy-breaker`), which `TestTraceRefusesDivergence` compares against what
the code writes now (regenerate with
`NOVA_TRACE_DIR=tla/traces go test -run '<test>' ./modules/store/` or `./modules/card/`).

## The walks and their seeds

- Store: `TestTraceWalk` (modules/store/trace_test.go) on the Fake, seeds 1..8, 500
  steps, with transport faults (`Fake.Drop`: a lost request is LoseRequest, a
  lost reply after commit is Apply with known=FALSE, followed by Replay or a
  second Apply); `TestFunctionalTraceWalk` on PostgreSQL, seeds 1..2, no faults.
  The generator (modules/store/trace_gen_test.go) draws ops over the fixed names
  (producers p1 p2, cards p1 p2 t1 u1 u2 u3, rows r1 r2, judgements j1 j2,
  cursor c1, bodies a b, tiers flash pro, hook sessions x y with either
  role, the stop on and off), biased toward existing entities.
- Card: `TestTraceColdWalk` runs `coldWalk` (TestColdWalk's walk) on seeds
  1..20, 800 steps each, recording each accepted op.

## The refinement mappings

`store.TraceState` (modules/store/trace.go) and `card.TraceState` (modules/card/trace.go) are
the mappings, each documented with every model variable it fills and every
abstraction. `store.TraceOp` maps an op to the model's Ops entry and refuses
what the model has no word for. In brief:

- Store: epoch, cards, rows, judgements, cursors and policies are projected;
  events, ledger, applied, receipt and lost are the model's own bookkeeping
  and checked through the record's action and its `known`/`accepted` flags.
  A card's After, Attempt, Review, Finding and non-normal Priority are not in
  Store.tla, so a walk op that sets them is refused by TraceOp; needs are a set,
  so a need named twice is refused (the code refuses it too). A consumer's
  tier is not in the model; the generator gives a consumer put beside its
  producer the producer's tier, which the code requires. PutStream is dropped
  (one stream).
- Card: producers project to ps, consumers to slots `<<p, i>>` in the order
  attempt, work before read, review, read index; heads to their index in the
  walk's head table (the base is 0); a finding to "fix" or "-"; flaps counted.
  One stream.

What the trace module adds to its model, each a lifted bound or a move outside
the machine:

- StoreTrace: none; MaxLoss is 2, every op may be lost.
- CardMachineTrace: TraceInit takes the rows as the walk's world has them (r1
  at width 2, not Init's 1); env is raised on every environment move and never
  bounded; Gone past the model's flap bound of 1 (GoneAt); RawHold, the walk's
  raw layer-3 stream hold, which parks nothing (HoldStream is the machine's).

Card moves the 20 seeds never reach: Reached, Waive, Relink, WaiveRed,
Released, multi-row AskReads, Add, HoldStream, Preempt and any InThisOp move
(those live in the cold2/3/4 walks, whose merged ops are not traced).

## The breakers

`TestTraceRefusesDivergence` (unit tier, the Fake only) holds one breaker per
package; each writes a fixture `check-trace.sh` must refuse at its last step:

- store-breaker: a store that strips RowHeld conditions accepts a deal fenced
  on `RowHeld(r1, FALSE)` after r1 was held. Refused at 4 of 4 (Apply o3).
- card-breaker: Start with its SetCardAttempt write dropped leaves p1 working
  at attempt 0. Refused at 3 of 3 (Start).

## Divergence found

- store-cursor-zero: `SetCursor{c1, 0}` on a cursor never set. The store
  accepted it (it inserted c1 at 0); Store.tla refuses it (an absent cursor
  is 0, and a write that changes nothing is refused). Ruled the code's fault
  (round 22): the store now refuses it ("cursor c1 is already at 0"), the
  fixture is `matched` (`TestTraceCursorZero`), and the walks send the write.

## Results, tla2tools v1.8.0 (the CI jar) on hetzner2, 2026-11-06 (round 22, the row's sess and role and the stop projected)

    store-fake-1..8   TRACE OK steps=497 513 512 511 520 520 485 497
    store-pg-1..2     TRACE OK steps=469 482
    card-walk-1..10   TRACE OK steps=65 105 90 107 106 104 64 94 105 58
    card-walk-11..20  TRACE OK steps=80 81 87 112 83 71 78 77 85 83
    store-breaker     refused at 4 of 4 (expected)
    card-breaker      refused at 3 of 3 (expected)
    store-cursor-zero matched (the cursor-zero ruling, above)

The whole `make-traces.sh check` run takes about 1.5 minutes on hetzner2.
