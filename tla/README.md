# tla/: the store model (layer 1) and the card machine (layer 2)

Two models live here: `Store.tla` (layer 1, below) and `CardMachine.tla`
(layer 2, in its own section after the store's results). `ci-tlc.sh` runs
both, with all their witnesses and reach checks, in one parallel pass.

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
too. It opens the next epoch and carries the rows forward: same held flag,
session and role, state `down`, gen + 1. It keeps the cursors (global, as
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
- `witnesses/Store*`: sixteen reversed witnesses, each `Store.tla` with one
  fault. They are generated: `python3 make-witnesses.py` writes every
  witness and every cfg (`Store.cfg`, `StoreRecords.cfg`,
  `witnesses/*.cfg`, `reach/*.cfg`) from `Store.tla` and the fault list in
  the script. Each fault is a set of exact text replacements; one that no
  longer matches stops the script. Edit `Store.tla`, rerun the script,
  commit both.
- `reach/Never*`: twenty-six reachability checks on `Store.tla`. Each must
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
- **ClearCarriesRows**: a clear keeps every row's held flag, session and
  role, sets it down, leaves no card or judgement, and keeps every cursor,
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
| reach `NeverHeldCarried` | none | r1's hold survives the clear |
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
| reach `NeverHeldCarried` | violated | 77 / 72 | 25 | 05s |
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
`make-traces.sh check` on every push, with `NOVA_TRACE_PG=1` so a missing
initdb fails the job instead of skipping the PostgreSQL walk.

The walks write traces only when `NOVA_TRACE_DIR` names a directory
(`make-traces.sh` sets it); without it the test suite is unchanged. The walk
traces (`store-fake-*`, `store-pg-*`, `card-walk-*`) are regenerated, not
committed. The committed fixtures are `store-breaker`, `card-breaker` and
`store-cursor-zero`; `TestTraceRefusesDivergence` and `TestTraceCursorZero`
compare them against what the code writes now (regenerate with
`NOVA_TRACE_DIR=tla/traces go test -run '<test>' ./store/` or `./card/`).

## The walks and their seeds

- Store: `TestTraceWalk` (store/trace_test.go) on the Fake, seeds 1..8, 500
  steps, with transport faults (`Fake.Drop`: a lost request is LoseRequest, a
  lost reply after commit is Apply with known=FALSE, followed by Replay or a
  second Apply); `TestFunctionalTraceWalk` on PostgreSQL, seeds 1..2, no faults.
  The generator (store/trace_gen_test.go) draws ops over the fixed names
  (producers p1 p2, cards p1 p2 t1 u1 u2 u3, rows r1 r2, judgements j1 j2,
  cursor c1, bodies a b, tiers flash pro, hook sessions x y with either
  role, the stop on and off), biased toward existing entities.
- Card: `TestTraceColdWalk` runs `coldWalk` (TestColdWalk's walk) on seeds
  1..20, 800 steps each, recording each accepted op.

## The refinement mappings

`store.TraceState` (store/trace.go) and `card.TraceState` (card/trace.go) are
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
