# tla/: the TLA+ models

Every state machine in nova-sprint has a TLA+ model here, checked with TLC, and the Go code is bound to each model by trace checks (see "Trace validation").

| Layer | Module | Models | Design document |
|---|---|---|---|
| 1 | `Store.tla` | the store: epochs, cards, rows, judgements, cursors, policies, the stop, the event log, replay | `docs/LAYER-1-STORE.md` |
| 2 | `CardMachine.tla` | every producer, consumer and row event of the card machine | `docs/LAYER-2-CARD-MACHINE.md` |
| 3 | `Steps.tla` | the tick: preemption, land results, needs, fix, deal, land requests, judgements, ops and their fences | `docs/LAYER-3-STEPS.md` |
| 4 | `LandEdge.tla`, `Runner.tla` | the landing ladder; the worker on one bench | `docs/LAYER-4-EDGES.md`, `docs/LANDING.md` |
| 5 | `Policy.tla` | policy versions, activation and the per-tick fallback | `docs/LAYER-5-POLICY.md` |
| 6 | `Run.tla`, `RunClear.tla`, `Setup*.tla` | the run (chooser, tick, pusher), its clear, and resumable setup | `docs/LAYER-6-RUN.md`, `docs/SETUP.md` |

Each module is checked three ways:

- **Passing runs** (`<Module>.cfg` and the scenario cfgs beside it): TLC finds no error. TLC exit 0.
- **Reversed witnesses** (`witnesses/`, and `witnesses/layer6/`): the module with one fault put in. TLC must report that fault's target invariant violated (exit 12; exit 13 for a liveness property). A witness shows the invariant can fail.
- **Reach checks** (`reach/*.cfg`): an invariant that says "this state is never reached". TLC must report it violated, which shows the passing runs are not vacuous.

In a witness cfg the target is listed first; when one state breaks several invariants TLC names the first in the list, and the gate requires it to be the target.

## Running

Run TLC on a bench host, never on the working machine.

```sh
bash tla/ci-tlc.sh
```

`ci-tlc.sh` is the gate. It downloads a pinned, sha256-checked `tla2tools.jar` (v1.8.0, from this repository's `tla-tools-1.8.0` release) to `$TLA_CACHE` (default `~/.cache/nova-sprint`), then runs every module in parallel, one TLC invocation each:

- every passing run must exit 0;
- every witness and reach check must report a violation of the first `INVARIANT` or `PROPERTY` in its cfg (a `PROPERTY` target must end in exit 13 with "Temporal property <target> was violated" and no invariant violated);
- any other outcome (a pass where a violation is required, a violation of another invariant, a parse error, a crash, a run that wrote no result) fails the gate.

`TLC_SHARD=i/n` runs only every n-th run, starting at the i-th (1-based); `TLC_CORES` caps every run's `-workers` and the sum of the live runs' workers. The nightly runs six shards, each on half its host's cores:

```sh
TLC_SHARD=2/6 TLC_CORES=8 bash tla/ci-tlc.sh
```

`TLC_JOBS` bounds concurrent runs (default: the cores), `TLC_HEAP` each JVM's heap (default `2g`, twice that for multi-worker runs), `DRY_RUN=1` prints the commands and runs nothing. Each run gets its own `java.io.tmpdir` and `-noGenerateSpecTE`, so a rerun in the same tree never sees trace modules.

Smaller tools:

- `sh run-tlc.sh /path/to/tla2tools.jar [java]` runs the layer 1 to 3 set with one log per run in `logs/`; `sh run-one.sh JAR JAVA DIR MODULE WORKERS [CFG] [LOG]` runs one module; `sh summarize-logs.sh` prints each log's result lines.
- `sh show-trace.sh <log>` prints a counterexample's violated invariant, its actions and its events; `sh show-all-traces.sh` does it for every violated log; `sh last-state.sh <log>` prints the epoch, cards and rows of its last state.
- `python3 -I card-trace.py logs/<log>.log` prints a card-machine counterexample one line per state (TLC names every step `<Next>`, so the move is read off the diff).
- `python3 run-layer6.py` runs a layer 6 manifest (`*-runs.json`); see `LAYER-6-MODELS.md`. `CHECK-LAYER6-TRACES.md` describes the layer 6 trace gate.

## Generated files

The witnesses and the cfgs are generated from the modules. Each witness is the module with its name changed, a header naming the fault and a few exact text replacements; a replacement that does not match the expected number of times stops the script, so a change to a module that a fault no longer fits fails at generation instead of producing a witness that tests nothing. Edit the module, rerun the generator and commit both.

| Generator | Writes |
|---|---|
| `make-witnesses.py` | `Store.cfg`, `StoreRecords.cfg`, `witnesses/Store*`, `reach/` store checks |
| `make-card-witnesses.py` | the seven `Card*.cfg` passing runs, `witnesses/Card*`, `reach/Card*.cfg` |
| `make-steps-witnesses.py` | the five `Steps*.cfg` passing runs, `witnesses/Steps*`, `reach/Steps*.cfg` |
| `make-policy-witnesses.py` | `witnesses/Policy*`, `reach/Policy*.cfg` |
| `make-landedge-witnesses.py` | `witnesses/LandEdge*`, `reach/LandEdge*.cfg` |
| `make-run-witnesses.py`, `make-run-stop-witnesses.py`, `make-run-clear-witnesses.py` | the Run and RunClear cfgs and `witnesses/layer6/` |
| `SetupChoices-generate.py`, `SetupHandoff-generate.py` | the Setup witnesses beside their models |
| `make-steps-traces.py` | the model's own trace fixtures (`traces/steps-ok-*`, `traces/steps-bad-*`) from TLC logs |

The Runner witnesses (`witnesses/Runner*`) are written by hand.

# Layer 1: the store

`Store.tla` models `docs/LAYER-1-STORE.md`. Each operation is one atomic step over `epoch`, `cards`, `rows`, `judgements`, `cursors`, `policies`, `stopped`, `events` and `applied`. In order, the step:

1. checks the op's epoch against the store's epoch;
2. checks its conditions;
3. runs its writes in order, each under its own rule;
4. either lands everything and appends an accepted event, or changes nothing and appends a refused event.

Epochs start at 1. `Clear` is an op with no writes and its epoch is checked too. It opens the next epoch and carries the rows forward: same held flag, session and role, state `down`, gen + 1. It keeps the cursors (global, as the log's seq is, so a subscriber resumes where it was), the policies and the sprint's stop, and leaves the cards and judgements behind (they are keyed by epoch). It is idempotent by id and logged with the epoch it closed. The event log is append-only: row triggers refuse UPDATE and DELETE on events and a statement trigger refuses TRUNCATE.

Policy versions are global. Put policy adds the next version; a version's body never changes once put and the version is never removed (`active` may change and is left out of the model).

Every set write that would change nothing is refused ("row r1 is already up", "cursor c1 is already at 2", "the sprint is already stopped"): the op is refused, its refused event is logged, and no gen moves. The model covers this for card state, red, row state, row held, the stop and the cursor.

Needs are a TLA+ set, so a reorder is the same value by construction (the store keeps them sorted without repeats). An empty name, an unknown role, and a session without a role (or a role without a session) are `ErrInvalid` before the store's step and are never logged, so they never reach the model.

Cards have a kind: `producer`, `sentinel`, or a consumer (`work` or `read`). A consumer names its producer (`prod`, the design's `of`), takes its producer's tier at add, and has no needs. A consumer's holder is set at add and never changes. Add card refuses a holder on a producer or a sentinel and refuses a consumer without one. A producer carries red, set by `SetRed` (producers only) and read by `CardRed`.

A row is sprint state only: its state, a gen, a held flag, and the session and role that hooked it. Its kind, width, tiers, model and harness are configuration (the roster, read each tick), never the store's: the store neither caps a row's working consumers nor checks that a row serves a tier. The deal guards in the card package do that, against the roster. The store takes any number of consumers of any tier on a row that exists.

`PutRow(r, session, role)` is the sprint hook's one write, and `PutRow(r, None, None)` unhooks. A hook by another session while one holds the row is refused (unhook first); the same session may hook again with a new role. Like every row write it raises the row's gen.

The sprint's stop (`stopped`) is one value for the store, as the epoch is. `SetStopped` sets it (refused when unchanged) and a clear keeps it. A fresh store begins stopped with the reason "never started", so nothing a tick plans lands before the first start (`FreshMovesNothing`). The store refuses nothing because of the stop except an op fenced on Running (the run's tick ops).

Judgements are `open`, `answered` or `expired`: put judgement opens one (refused if it exists), and answer and expire close it. A close is refused inside the write unless the judgement is open, so it closes once whatever the plan carried; `JudgementOpen` still fences a plan. A cursor holds a seq that set cursor never moves back and never moves past the last event's seq (events are numbered from 1, refused ones included); an absent cursor reads as 0.

The conditions are `CardGen`, `CardState`, `HolderIs`, `CardAbsent` and `CardRed` on a card; `NeedsLanded` (every need of the card is landed, read from the store in the step); `RowIs`, `RowGen` and `RowHeld` on a row; and `JudgementOpen` on a judgement. An op that writes a card or a row raises its gen by one, however many of its writes touch it. A card or row the op itself adds stays at gen 0.

`ledger` is a ghost: every event as it was committed. The store appends to `events` and `ledger` together and never changes `events` otherwise, so `LogAppendOnly` (`events = ledger`) holds by construction; the witness `StoreLogTampered` adds the UPDATE the trigger refuses. TRUNCATE has no action in the model.

The caller's view is modelled too: `phase` per op (`idle`, `unknown` or `done`), `receipt` (the receipt the caller holds) and `lost` (the transport failures so far). A transport failure is either a request lost before the store saw it, or a reply lost after the store applied it. Either way the caller is left `unknown` and recovers with `Replay`, or with `Apply` again.

## What one model step stands for in SQL

One model step is one SQL transaction from `LOCK TABLE sprint.epoch` to COMMIT at READ COMMITTED; the lock is what makes the atomic step real. The epoch, the conditions, the writes with their rules and the event row are all inside it. Without the lock, two transactions at READ COMMITTED can each read the rows, decide, and then write; the witness `StoreStaleCheck` is that interleaving. With the lock, writers run one at a time and each reads the state the previous one committed. That serial order is the order of `events`.

## The instance

6 cards (producers p1, p2; sentinel t1; consumers u1, u2, u3), 2 rows, one judgement j1, one cursor c1, two policy versions, two bus sessions (x, y), 36 fixed ops. Callers issue their ops in program order (`Pred`). A setup caller goes first, covering each deterministic rule once: s1 and s2 hook r1 and r2 by session x as workers; s3 (a producer with a holder) and s4 (a sentinel with a holder) are refused; s5 adds p1 (tier pro); s6 adds p2 (needs p1) and t1 (needs p2); s7 readies p2 before its need landed (refused), s8 lands p1, s9 readies p2; s10 adds consumer u3 on r1 and readies it; s11 opens judgement j1; s12 and s13 put policy versions 1 and 2; s14 sets p2 red, s15 (a consumer) and s16 (a sentinel) are refused by the red write, s17 sets p2 red again (refused as changing nothing); s18 hooks r2 by session y while x holds it (refused); s19 unhooks r2 and s20 hooks it by y as coordinator; s21 stops the sprint and s22 stops it again (refused as changing nothing). s23, fenced on Running as a run tick op is, is refused while the sprint is stopped, and n1 is a tick op on the fresh store, refused because the sprint was never started.

The callers of one scenario then run concurrently; the constant `Scenario` picks it. The two scenarios touch disjoint tables, so splitting them keeps each run small without hiding an interleaving.

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

Left out: streams, policy activation and the seq fence; op times (so the refusal of a close timed before `opened_at`); the soft `after` list (it follows the needs rule; no store rule reads it); and the fields no rule reads. On cards and rows those are attempt, author, branch, base_sha, prior_head, head, pr, result, bounces, cost, priced, priority, brief, paths, done_when, prose, reason, score, flaps and the stop's reason. On judgements they are kind, about, question, command, group_key, deadline, answer, actor and the times. They add columns, not behaviour.

## Invariants

- **TypeOK**: every variable stays in its type.
- **SingleStateSingleHolder**: every card has exactly one state from the closed set (or is absent), at most one holder, and that holder is a row of this epoch.
- **ProducerNeverHeld**: no producer or sentinel has a holder.
- **RedOnlyOnProducers**: only a producer is ever red.
- **ConsumersWellFormed**: a consumer names an existing producer, has no needs, has a holder, and has its producer's tier; a producer or sentinel names none.
- **IdempotentById**: no op id appears twice in the event log.
- **AppliedIsLog**: the op ids the store remembers are exactly the ids in the log.
- **LogMatchesState**: folding the accepted events from empty gives the current state. Rebuild applies each accepted event's writes and never rechecks a condition or a write rule. Checked as a chain (the fold by induction): each event starts where the one before ended, each accepted event's after-state is its writes applied to its before-state, and the last ends at the current state.
- **OpsInTheirEpoch**: every accepted op, clear included, was planned against the epoch the store was at when it applied.
- **ConditionsAtomic**: every accepted op's conditions held in the state immediately before its own writes.
- **NoChangeOnRefusal**: a refused op changed nothing.
- **NoSpuriousRefusal**: every refusal has a cause in the spec (the epoch, a condition, or a write rule as the design states it), so a store rule stricter than the design (a width cap, say) is caught.
- **GenPerOp**: each accepted op raises the gen of each card and row it writes that existed before it by exactly one; one it creates is at 0; a clear raises every carried row's gen by one.
- **ClearCarriesRows**: a clear keeps every row's held flag, session and role, sets it down, leaves no card or judgement, and keeps every cursor, the policies and the stop.
- **HookKept**: no accepted op moves a row from one session straight to another; it is unhooked between.
- **JudgementClosedOnce**: within an epoch, at most one accepted op answers or expires a given judgement.
- **CursorMonotone**: no accepted op, a clear included, moves a cursor back.
- **CursorWithinLog**: no cursor is past the seq of the last event.
- **LogAppendOnly**: the events table is exactly what was committed, in order: no event was updated or deleted.
- **PolicyImmutable**: a policy version, once put, keeps its body; no op changes or removes it.
- **ReceiptMatches**: a caller that thinks it knows the outcome holds exactly the stored receipt.
- **FreshMovesNothing**: nothing a tick plans lands before the sprint's first start.

## Expected results

| Module | Fault | Violated |
|---|---|---|
| `Store` (deal), `Store` (records) | none | pass |
| `StoreNoEvent` | an accepted op lands without its event | LogMatchesState |
| `StoreReplayApplies` | Replay runs the op again and logs it again | IdempotentById |
| `StoreStaleCheck` | conditions judged on a snapshot read before another op's commit (no lock) | ConditionsAtomic |
| `StoreEpochUnfenced` (records) | ops other than clear are not epoch-checked | OpsInTheirEpoch |
| `StoreProducerHolds` | add card accepts a holder on a producer or sentinel | ProducerNeverHeld |
| `StoreSentinelHolds` | add card refuses a holder on a producer but not a sentinel | ProducerNeverHeld |
| `StoreRowStale` | RowGen is not checked | ConditionsAtomic |
| `StoreRedOnConsumer` | set red accepts a consumer | RedOnlyOnProducers |
| `StoreWidthInStore` | the store caps a row at one working consumer | NoSpuriousRefusal |
| `StoreHookStolen` | put row lets another session hook a hooked row | HookKept |
| `StoreStopDropped` | a clear resets the stop | ClearCarriesRows |
| `StoreFreshRunning` | a fresh store begins running | FreshMovesNothing |
| `StoreJudgementTwice` (records) | answer and expire refuse only an absent judgement | JudgementClosedOnce |
| `StoreCursorBack` (records) | set cursor accepts a lower value | CursorMonotone |
| `StoreCursorPastLog` (records) | set cursor is not bounded by the log | CursorWithinLog |
| `StoreLogTampered` | an UPDATE on events (no append-only trigger) | LogAppendOnly |
| `StorePolicyRewritten` | put policy writes the latest version, not the next | PolicyImmutable |

The `reach/` checks on `Store.tla` show these are reachable: a1 is accepted (`NeverTaken`); a caller left unknown reaches done (`NeverRecovered`); an old-epoch op after the clear is refused (`NeverStaleEpochRefused`); s9 passes NeedsLanded (`NeverNeedsReady`); k2 is accepted in epoch 2 (`NeverNewEpochOp`); r1 holds two working consumers (`NeverTwoWorking`); s3 and s4 are refused (`NeverProducerRefused`, `NeverSentinelRefused`); r1's hold survives the clear (`NeverHeldCarried`); a1 is refused on RowHeld after g1 (`NeverHeldRefused`); e1 sets r1 down (`NeverRowDown`) and is refused on a stale RowGen (`NeverRowFenced`); s13 adds policy version 2 (`NeverTwoPolicies`); s14 sets p2 red (`NeverRed`), s15 and s16 are refused (`NeverRedKindRefused`), s17 is refused (`NeverSameRedRefused`); s18 is refused (`NeverHookRefused`) and s20 hooks r2 by y as coordinator (`NeverRehooked`); a1 lands while the sprint is stopped (`NeverDealtStopped`); s22 is refused (`NeverSameStopRefused`); n1 is refused (`NeverFreshRefused`); and in `records`: j1 is answered (`NeverAnswered`), a second fenced close is refused on JudgementOpen (`NeverJudgementFenced`), the unfenced h2 is refused by the write (`NeverCloseRefused`), m2 is refused below c1's value (`NeverCursorRefused`), m3 is refused past the log (`NeverCursorPastRefused`) and c1 keeps its value across the clear (`NeverCursorCarried`).

# Layer 2: the card machine

`CardMachine.tla` models `docs/LAYER-2-CARD-MACHINE.md`: producer and consumer cards, every event of the producer, consumer and row tables as an action, Add, AskReads, WaiveRed, UnholdStream, blocker preemption, the stream's hold with Park in the same op, hard needs with Waive and Relink, soft `after` (an order edge, read only by the cycle checks), and the sentinel. An action's guard is the design's from-state plus its conditions; an event that does not match is not enabled (the Go code refuses it with a value, which changes nothing). The model is the reachable state space of accepted moves.

Consumers are slots: producer p's i-th cut consumer is `<<p, i>>`. Start and AskRead cut the next slot, so a producer is never in `working` without its consumer. The deal is the take: Start and AskRead cut the consumer `working`, in its row's hand, guarded by `RoomOn` (the row's working consumers, less a preempted victim, below its width), and there is no Take. A producer carries `review`, its count of entries into review (Finished and Red raise it); a read carries the producer's `review` at cut, and ReadYes needs a done ok read (ReadFix a done fix read) of the current head whose `review` equals the producer's.

Rules modelled:

- A Withdraw of the working producer's work consumer releases the producer in the same step (Preempt's Withdraw too), so Released and Block from working are kept as the tables have them but are never enabled. Broken, Faulted, ReadFix, Released and that release write the producer parked (waiting) when the stream is held.
- A work consumer reports ok, fail or fault; a read ok, fix or fault. An ok work head is neither the base nor the attempt's prior_head (the producer's head at Start). A work consumer's fault naming a missing route (result `route`) blocks its producer on the first fault instead of a re-deal.
- AskRead refuses a sentinel, the author and a row already holding a live read of this head. AskReads cuts reads on several rows in one step, each checked as AskRead checks it, into distinct consecutive slots.
- Replace names a non-final producer or sentinel other than itself. A sentinel takes no Start, AskRead, Edit or Rescore, and takes SetPriority and Relink. Relink's target is a producer or a sentinel.
- Add puts a card with needs and after (producers or sentinels), refusing a cycle through either. Ids contain no dot: cards and consumers share one namespace, fenced absent on every Add and every cut, and consumer ids are the code's (`<p>.w<attempt>`, `<p>.r<review>.<n>`, `ConsName`). `deps` carries the id `p1.w1`, which Add refuses for its dot; without that refusal it is added before p1 starts and p1 can never cut its first work consumer (`CardDottedId`, `NeverStranded`).
- A Red is recorded on the producer as a flag `red`: Red sets it; Finished, Start and Preempt's Start clear it; WaiveRed requires it. The prior-read check alone does not imply it: a Finished can come back to an already-read head, and only the red check refuses WaiveRed there (`CardWaiveRedWithoutRed`). WaiveRed moves a producer from review to merging with no new read when a done ok read of the previous review at the same head exists, with no live consumer.
- `finding` is written only by ReadFix (always with a non-empty finding); Broken and Faulted keep it; Start copies it to the new work consumer.
- Landed has no after check: a landing never holding on a soft after is layer 3's landing step.
- Merge composes moves. An op is an ordered list of up to `MaxOp` moves, nested merges flattened first, each planned against what the earlier ones left. In the model the moves of an op are consecutive steps: `op` counts the moves of the open op and `snap` is the state it started from. A consumer cut earlier in the op is live for the next move; the stream, rows and cards are the folded ones; a move's conditions are the first-read value of each field, minus the fields an earlier move of the op wrote, and the store checks them against `snap` (`FencesOnSnapshot`). A sequence legal one by one is one op, the last write winning (`MergeParksResolved`). The preempt guard runs on the whole op for Withdraws flagged preempt: with a blocker's Start in the same op, the withdrawn consumer is working on the row that Start names (`PreemptOnBlockerRow`); a plain Withdraw never triggers it (`PlainWithdrawUnguarded`). `InThisOp` moves are legal only as a later move of an op whose earlier move failed the need (`InThisOpOnFailedNeed`). The review counter is folded once per op (`ReviewOnEntry`) and a move's record is the real one with its fields written, never a partial one (`AttemptOnlyOnStart`). `MaxOp` is read off the scenario (merge 3, mergepre and mergedeps 2, every other 1).
- `last` is a step monitor. `StepRule` reads the state before and after each step and records the step rules it broke (`last.bad`); it is shared by every action and part of none, so a fault in an action is caught there. It keeps only a few event names for the reach checks, so states are not split by the move that reached them.

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
| `witnesses/CardWaiveRedWithoutRed.cfg` only | `redw` (Scenario life) | p1, sentinel s | s needs p1 | 6 | 2 | 3 | 0 |

- `life` carries the whole lifecycle: a fix, a Red with a yes after it, a fault re-dealt, the sentinel reached.
- `deps` covers a dependent pair plus the sentinel: NeedFailed, Waive, Relink, their InThisOp forms, the cycle check, and Add of a second sentinel. It has one environment move: with two, Add's choices pass 6M states.
- `reads` has a third row, so AskReads has two rows that are not the author.
- A Red is a CI result, so it spends the environment budget; as a free move, Red and WaiveRed loop.
- `redw` is `life` sized for one witness: WaiveRed without its red check needs a third attempt. It is never a passing run.
- `pre` exists because a blocker can only preempt the consumer of another producer live at the same time, which the `deps` pair never is (p2 waits for p1 to land). Its environment budget goes to SetPriority, the row table and the stream's hold; Edit and Rescore are checked in the other scenarios.

Bounds that are the model's, not the design's: K consumer slots per producer; attempt stops at the attempts column (Start is not enabled past it); bounces stop at `MaxBounce` (a work consumer does not report fail, nor a read fix, at it, so a reported consumer is never stuck); flaps stop at 1; scores are 0..1; widths 1..2; priority is normal or blocker. The environment and coordinator moves (Add, Rescore, SetPriority, Edit, Seen, Gone, Width, Hold, Unhold, the stream's hold) share a budget per behaviour; every card and consumer move is unbounded but for the slots. Rows start up, width 1, not held. Head 0 is "no head" and the base commit, so an ok work head always differs from base_sha.

Left out, because no guard reads them: branch, base_sha, reason, brief, prose, paths, done_when, cost, priced. Edit writes only prose, paths and done_when, so in the model it moves nothing but the monitor. Of the data columns, these are read by a transition and so are model state: head, prior_head, review, author, red, and finding (as "fix" or none). HoldStream's AtSeq fence is the atomic step itself: the model parks exactly the ready set of the state it steps from, which is what a plan fenced at the snapshot's seq can apply. NeedFailed, Waive and Relink `{InThisOp}` are one step together with the Cancel of the need.

## Invariants

State invariants:

- **TypeOK**, **SlotsDense** (the cut slots are 1..ncut).
- **ProducerNeverHeld**: the holder lives on the consumer only (structural).
- **WorkingOneWorkConsumer**: at most one live work consumer; a live one is the current attempt's and its producer is working; a working producer has its current attempt's work consumer, never withdrawn.
- **WorkingLeavesInOneStep**: a working producer with no live consumer (its consumer reported) has a leaving move enabled: Finished, Broken, Faulted, Released or Block.
- **ParkedUnderHold**: no producer is ready in a held stream after any step.
- **NeverStranded**: every non-final producer with no live consumer has a move of its own enabled (not counting the coordinator's Cancel, Replace, Block, Edit, Rescore, SetPriority), or waits on a need that can still move, an after-target that is merging, or (waiting, ready, review) a row coming back, a hold lifting, or a full row's room. A ready producer at the attempt bound, or one with no slot left, is exempt as the model's limit.
- **ReadOnlyInReview**: a live read only in review, of the current head and review count.
- **ReaderNeverAuthor**: a live read's holder is never the producer's author.
- **WidthKept**: working consumers per row never exceed its width.
- **DealIsTake**: no consumer is ever `ready`: a cut consumer is working, in its row's hand.
- **NeedFailedTakesDependants**: when a need has failed and the dependant is not final, NeedFailed is enabled.
- **SentinelShape**: a sentinel is only absent (before Add), waiting, landed or fail (it never has a consumer).
- **NeedsAcyclic**, **OrderAcyclic**: the needs graph, and needs and after together, stay acyclic.
- **ResultsByKind**: a work consumer's result is ok, fail or fault; a read's ok, fix or fault.
- **OneLiveReadPerRow**: at most one live read of a head per row.
- **IdsDistinct**: no existing card's id is a cut consumer's id.
- **PreemptOnBlockerRow**: an op's preempt Withdraws are of consumers on the row its blocker Starts name.
- **PlainWithdrawUnguarded**: in an open op, a plain Withdraw of any working consumer is enabled.
- **MergeParksResolved**: in an open op, with the stream not held and an environment move left, HoldStream is enabled.

Step invariants (each: the last step kept its rule):

- **FinalNeverMoves**: no landed/fail producer and no done/fail consumer changes.
- **AttemptOnlyOnStart**: attempt rises by one on Start (and Preempt's Start) of that producer, and on nothing else.
- **RedNeverAdvances**: a Red step never ends in merging or landed.
- **FixKeepsScore**: ReadFix keeps the score.
- **HolderNeverChanges**: a cut consumer's holder never changes.
- **OldAttemptNeverMoves**: Finished, Broken, Faulted and Released accept only the work consumer of the current attempt.
- **OnlyHolderActs**: Report only from the consumer's holder.
- **RouteFaultBlocks**: a work consumer's fault naming a missing route moves its producer to blocked, never to ready.
- **ReadYesNeedsYes**, **ReadFixNeedsFix**: ReadYes only with a done ok read, ReadFix only with a done fix read, of the current head and review.
- **WithdrawReleasesInOneStep**: a Withdraw (or Preempt) of the working producer's work consumer leaves the producer ready, or waiting under a hold, in that same step.
- **WaiveRedNeedsPriorRead**, **WaiveRedNeedsRed**: WaiveRed only with a done ok read of the previous review at the current head, and only with the Red recorded.
- **FixCarriesFailure**: Broken and Faulted keep the producer's finding, and Start (and Preempt's Start) copies it to the new work consumer.
- **ReplaceByLive**: Replace names a non-final card other than itself.
- **HeadNeverRepeats**: a work consumer's reported or withdrawn head is never its prior head.
- **FaultSpendsNothing**: Faulted keeps attempt and bounces.
- **BlockNoLive**, **EditNoLive**: Block and Edit only with no live consumer.
- **SentinelLandsByReached**: a sentinel lands only by Reached, needs landed.
- **FencesOnSnapshot**: in an op, a move's conditions on a card's state are those of the snapshot, minus states an earlier move of the op wrote.
- **ReviewOnEntry**: review rises by one on Finished and Red of that producer, and on nothing else.
- **FixSaysWhat**: ReadFix writes a finding.
- **InThisOpOnFailedNeed**: after an InThisOp move, its need is fail.

## Expected results

Passing runs: `CardMachine`, `CardDeps`, `CardPreempt`, `CardReads`, `CardMerge`, `CardMergePre`, `CardMergeDeps`, on their scenarios.

| Witness (`witnesses/`) | Scenario | Fault | Violated |
|---|---|---|---|
| `CardLandedMoves` | life | Rescore has no final check | FinalNeverMoves |
| `CardAuthorReads` | life | AskRead does not refuse the author | ReaderNeverAuthor |
| `CardRedAdvances` | life | Red moves merging to landed | RedNeverAdvances |
| `CardNeedFailedIgnored` | deps | NeedFailed reads the dependant's state for the need's | NeedFailedTakesDependants |
| `CardNonHolderReports` | life | Report does not check the holder | OnlyHolderActs |
| `CardTwoWorkConsumers` | life | Start accepted from working | WorkingOneWorkConsumer |
| `CardStaleReport` | life | Faulted accepts any work consumer of p | OldAttemptNeverMoves |
| `CardYesOnZero` | life | ReadYes without the yes-read check | ReadYesNeedsYes |
| `CardFaultBumpsAttempt` | life | Faulted raises attempt | FaultSpendsNothing |
| `CardFaultBounces` | life | Faulted raises bounces | FaultSpendsNothing |
| `CardEditUnderLive` | life | Edit under a live consumer | EditNoLive |
| `CardCutReady` | life | Start cuts its consumer ready | DealIsTake |
| `CardDealOverWidth` | pre | Start has no room guard | WidthKept |
| `CardRouteRedealt` | life | a route fault goes back to ready | RouteFaultBlocks |
| `CardNeedsCycle` | deps | Relink without the cycle check | NeedsAcyclic |
| `CardWithdrawLeavesWorking` | life | Withdraw does not release the working producer | WithdrawReleasesInOneStep |
| `CardReadFixOnZero` | life | ReadFix without the fix-read check | ReadFixNeedsFix |
| `CardHeldStreamReady` | life | Broken returns to ready under a held stream | ParkedUnderHold |
| `CardWaiveRedWithoutRead` | life | WaiveRed without the prior-read check | WaiveRedNeedsPriorRead |
| `CardRelinkAfterCycle` | deps | Relink walks needs only | OrderAcyclic |
| `CardReadReportsFail` | life | a read may report fail | ResultsByKind |
| `CardReplaceByFinal` | life | Replace by a final card | ReplaceByLive |
| `CardTwoReadsOneRow` | life | AskRead takes a row holding a live read of the head | OneLiveReadPerRow |
| `CardPriorRepeated` | life | an ok report repeats the prior head | HeadNeverRepeats |
| `CardWaiveRedWithoutRed` | redw | WaiveRed without the red check | WaiveRedNeedsRed |
| `CardMergeCutUnseen` | merge | a consumer cut earlier in the op is unseen by the next move | BlockNoLive |
| `CardMergeNestedHold` | merge | a release reads the stream's hold from the snapshot | ParkedUnderHold |
| `CardMergeNoRerun` | merge | a merged move is checked on the snapshot, not the fold | FinalNeverMoves |
| `CardMergeOwnWrites` | merge | a move fences a state an earlier move wrote | FencesOnSnapshot |
| `CardMergeNestedStart` | mergepre | the preempt guard skips a nested Start | PreemptOnBlockerRow |
| `CardDottedId` | deps | Add takes an id with a dot | NeverStranded |
| `CardInThisOpAlone` | mergedeps | InThisOp alone, the need not failed in the op | InThisOpOnFailedNeed |
| `CardPlainWithdrawGuarded` | mergepre | the preempt guard on every Withdraw | PlainWithdrawUnguarded |
| `CardMergeSecondWrite` | merge | a second write to a field refused | MergeParksResolved |
| `CardReadFixEmpty` | life | ReadFix with an empty finding | FixSaysWhat |
| `CardFailRepeatsPrior` | life | a fail report names the prior head | HeadNeverRepeats |
| `CardMergeReviewTwice` | merge | Report then Finished in one op raises review by two | ReviewOnEntry |
| `CardHandPlanPartial` | merge | a merged move writes a partial record | AttemptOnlyOnStart |

Reach checks (`reach/Card*.cfg`), each reporting that a state is reachable: `NeverLanded` (a producer lands), `NeverFixed` (a review ends in ReadFix), `NeverRed` (merging goes red), `NeverBlocked`, `NeverSentinelReached`, `NeverFaultRedealt`, `NeverRouteBlocked`, `NeverRedToFix`, `NeverNeedFailed`, `NeverRelinked`, `NeverAdded`, `NeverSentinelNeeded`, `NeverAskReads`, `NeverInThisOp`, `NeverWaivedRed`, `NeverFindingCarried`, `NeverPreempted` and `NeverInThisOpMerged`.

# Layer 3: the steps

`Steps.tla` models the tick of `docs/LAYER-3-STEPS.md`, with the inputs as `docs/LAYER-6-RUN.md` has them: reports and takes are the rows' own store ops, and the inbox holds edge results only. The Go in `steps/` cites it by the invariant names below.

The state is the store `w` (producers, consumer slots, rows up or down, each stream's hold, and the take order of working consumers), the view `snap`, `inbox` (edge results: a landing outcome, landed or red with its CI, and a runner's stop ack), `inflight` (edge requests: id `<<epoch, kind, card, sha>>`, stream, sent), `js` (open judgements, kind and subject), `split` (the groups of the last tick's refused ops of two groups or more) and a monitor `last` of the last tick.

`Snap` copies the store to the view (the fold). Between `Snap` and `Tick` the store moves under the tick: a row's `Report` (its verb's own op: a work consumer's report moves its producer in the same plan), the coordinator's hold (with Park of the stream's ready producers in the same op) and its lifting, a row's Gone and Seen. `Take` comes only between ticks: a take changes nothing a tick fences. `EdgeResult` delivers an in-flight request's result once or twice (layer 4 may redeliver).

`Tick(a)` is one atomic action. From the view alone, in the design's order:

1. **Preemption**: a ready blocker with every open serving row at width (working consumers only) is one group: Withdraw of the victim on the chosen row (releasing its producer), the blocker's Start there, and a stop request for the victim's runner. The row is policy's `rows` answer when it is a preemptable row, else the first by name; the victim is the youngest working consumer on it by take, the engine's fixed rule. No open serving row: an `unserved` judgement for the tier.
2. **Land results**, in arrival order: Landed or Red for a producer still merging at the request's head; an ack, a stale or a repeated result is consumed with no move.
3. **Needs**: Resolve in id order: needs landed, stream not held, no producer of the stream overlapping its paths that is started (working, review, merging) or ready, counting those resolved earlier in the tick. Never a function of rows.
4. **Fix**: ReadYes or ReadFix (the default verdict) once every read of the current review is final and one is done.
5. **Deal and reads**: blocker work, every read owed, normal work, in id order; a row is dealt while its working consumers are under its width, with the in-tick tally (the cut is working). The row is policy's `rows` answer when it is eligible for the candidate, else the default: the desired rank exactly (the card's tier; one up once its bounces reach `escalate_after`; for a read, the rank above its author's row), then above it, then an idle lane, most room, name. A row below the card's tier is never eligible. No open serving row: `unserved`; a review no row may read: `cannot-read`.
6. **Land requests**: per stream with none in flight, the first merging producer none of whose `after` targets is merging (a hold never touches landing).
7. **Judgements**: `unserved`, `cannot-read`, and one `policy` judgement per question whose answer failed for a subject; `unserved` and `cannot-read` expire when the cause clears in the view.

A card claimed by an earlier step is skipped by the later ones. Then the order (blocker groups, never cut; land results; needs; fix; the deal), the budget (cards touched; the first non-blocker group always goes, so a group larger than the budget is not starved; stop at the first group that does not fit, so the inbox is consumed as a prefix), the packing (up to `Pack` groups per op; a group touching a card of a `split` group is an op of its own), and the apply: each op in order is accepted whole if every record its groups read is as the plan expects it (the view with this tick's accepted ops applied) and every move passes layer 2's guard on the store as it is, else refused whole. A stop request is dispatched only with its accepted op (its After); a land request has an empty After. An inbox item is consumed when admitted and its op accepted (or it has no move); a refused op's items are put back in place; the rest carry, in arrival order.

The oracle `a` is the asker: when a question has subjects, policy answers in domain (either row preferred for every candidate) or fails ("failed": no answer in the tick's policy time, an error, out of range, or a row not offered, which is one path in the engine).

The per-card moves are layer 2's, written at the grain the steps read: each guard (`Legal`) and write (`ApplyG`) is the `CardMachine.tla` action of the same name. `CardMachine.tla` cannot be INSTANCEd here: its card sets are fixed by its `Scenario` constant (`Prods` is `{p1}` or `{p1, p2}`), and preemption of the youngest of two working consumers needs three producers. Heads are attempt numbers (an ok report of attempt n pushes head n).

## The instance: five scenarios

Rows: r1 width 1 serves tier 1 (rank 1); r2 width 2 serves tiers 1 and 2 (rank 2).

| Scenario | Cards | Start | Environment | Constants |
|---|---|---|---|---|
| pre | b blocker (s2), p, q (s1, disjoint paths) | all waiting, s2 held, r1 down | lift s2's hold; r2 down and up | K 2, MaxAtt 2, MaxEnv 2, MaxArrive 2, Budget 2, Pack 2 |
| pre1 | as pre | as pre | as pre | as pre, Pack 1 |
| deal | p, q (s1, overlapping), b (s2, tier 2, needs p) | all waiting | r1 down and up | K 4, MaxAtt 2, MaxBounce 1 (p only), MaxEnv 1, MaxArrive 1, Budget 2, Pack 2 |
| land | p (s1), q (s2, after p) | both merging at head 1 | hold and lift s1 | K 2, MaxAtt 1, MaxEnv 2, MaxArrive 3, Budget 2, Pack 2 |
| burst | as land | as land | none | K 1, MaxAtt 1, MaxArrive 4, Budget 1, Pack 1 |

Red results come only in land and burst; work and read reports are ok but in deal, where p may fail and a read of p may say fix. A fault report is a fail that spends no bounce, so no scenario needs it.

## Invariants and the liveness property

State invariants:

| Invariant | Says |
|---|---|
| `NeverDealtTwice` | at most one live work consumer per producer, of its current attempt, only while it works |
| `WidthKept` | working consumers on a row never exceed its width |
| `DealIsTake` | no consumer is ever `ready` |
| `ReadyNeverFlipsWithLoad` | the ready rule answers the same for the store and for the store with no consumers and every row down |
| `NeverOverlapStarted` | two producers of one stream with overlapping paths are never ready or started together |
| `NeverBelowTier` | every live consumer is on a row that serves its card's tier (no tier is ever written) |
| `CarryFIFO` | the inbox is in arrival order and nothing that arrived is lost |
| `EdgeAfterAccepted` | an in-flight stop names a withdrawn consumer; an in-flight land a producer merging at that head |
| `DealtWithinKTicks` | a producer ready for `UndealtTicks` ticks with an open serving row with room is dealt, or has an open undealt judgement |

Tick invariants, each a check of the last tick computed from the view, the store before and after and the plan (`TickChecks`):

| Invariant | Says |
|---|---|
| `OneGroupPerCard` | no card is in two of the tick's groups |
| `GroupWhole` | each group's cards are all as applied or all unchanged |
| `NeverHalfApplied` | each op's groups are all applied or none |
| `RefusalChangesNothing` | a refused op changes none of its cards |
| `RefusedOnlyByRace` | an op is refused only when a record it read moved between the fold and the tick |
| `BudgetKept` | past the first non-blocker group, the cards touched stay within the budget |
| `BlockerPreemptsInOneTick` | a ready blocker with every open serving row at width has a preemption group in the tick, or no open serving row and an `unserved` judgement |
| `PreemptYoungest` | the victim is the youngest working consumer on the row by take |
| `PolicyFailsToDefault` | a failed rows answer deals exactly what no policy deals, and every failed question has its `policy` judgement |
| `NoDealWithoutPolicy` | no preemption, Start or read while no active policy answers, and the no-policy judgement is open |
| `AfterKept` | a land request never goes out for a producer whose `after` target is merging |
| `AfterNeverHolds` | a stream with none in flight and a merging producer whose `after` targets are not merging gets a land request |
| `MergingCovered` | after a tick, every merging producer of the view it planned on is in a land request in flight or planned for its stream, under a land judgement, or held by a merging after target |
| `ResolveFromState` | every producer the view's state makes ready is resolved, or is held behind one resolved this tick on a file |
| `HeldBehindShown` | a producer held behind another on a file has an overlap judgement |

`ReadyDealtWhenRoom`, under weak fairness on `Snap` and `Tick` only (rows need not take or report): a ready producer with room on an open serving row (under its width) is eventually dealt, unless the room goes. The model's bounds (attempts, slots) are exempt, as in `CardMachine`, and so is a run with no active policy.

## Expected results

Passing runs: `StepsPre`, `StepsPrePack1`, `StepsDeal`, `StepsLand`, `StepsBurst` (liveness included).

| Witness (`witnesses/`) | Scenario | Fault | Violated |
|---|---|---|---|
| `StepsDealtTwice` | pre | the deal does not skip a blocker the preemption claimed, and the preemption fires with free width | NeverDealtTwice |
| `StepsTwoGroups` | land | the land-result step ignores claimed cards | OneGroupPerCard |
| `StepsWidthOver` | burst | the deal checks no width | WidthKept |
| `StepsBlockerQueued` | pre | preemption counts queue room as free width | BlockerPreemptsInOneTick |
| `StepsHalfWrites` | pre | a refused preemption's Withdraw half is applied | GroupWhole |
| `StepsPartialOp` | pre | a refused op applies its unraced groups | NeverHalfApplied |
| `StepsRefusedApplied` | pre | a refused op is applied anyway | RefusalChangesNothing |
| `StepsSelfRefusal` | deal | the deal does not pre-check that a row is up | RefusedOnlyByRace |
| `StepsFenceOnView` | pre1 | ops fenced on the view, not the view plus the tick's accepted ops | RefusedOnlyByRace |
| `StepsBudgetIgnored` | burst | no budget | BudgetKept |
| `StepsCarryDropped` | burst | the carry drops what the budget left | CarryFIFO |
| `StepsCarryNewestFirst` | burst | the carry is put back newest first | CarryFIFO |
| `StepsStopEarly` | pre | stop requests from refused ops too | EdgeAfterAccepted |
| `StepsReadyOnRoom` | deal | Resolve requires room on a row | ReadyNeverFlipsWithLoad |
| `StepsNoOverlapCheck` | deal | Resolve without the overlap check | NeverOverlapStarted |
| `StepsTierUnchecked` | deal | the tier checked nowhere (answer and Start) | NeverBelowTier |
| `StepsAfterIgnored` | land | the land step ignores after | AfterKept |
| `StepsAfterHolds` | land | the land step holds on any target not landed | AfterNeverHolds |
| `StepsPolicyStall` | deal | a failed rows answer plans nothing | PolicyFailsToDefault |
| `StepsOldestVictim` | pre | the victim is the oldest | PreemptYoungest |
| `StepsDealOneShort` | pre | the deal leaves one slot of width unused | ReadyDealtWhenRoom (liveness, exit 13) |
| `StepsCutReady` | pre | the deal cuts its consumer ready | DealIsTake |
| `StepsUndealtSilent` | burst | no undealt judgement | DealtWithinKTicks |
| `StepsLandAfterHoldsUntilLanded` | land | the after hold lasts until the earlier card lands | MergingCovered |
| `StepsDealsWithoutPolicy` | pre | the policy gate ignored | NoDealWithoutPolicy |
| `StepsResolveOnEvent` | deal | resolve only on a landed result's event | ResolveFromState |
| `StepsGlobOverlaps` | pre | a glob both list counts as overlap | ResolveFromState |
| `StepsHeldUnshown` | deal | no overlap judgement for a held producer | HeldBehindShown |

Reach checks (`reach/Steps*.cfg`), each reporting that a state is reachable: `NeverPreempted`, `NeverUnserved`, `NeverRefused` (an op is refused by a race), `NeverSplit`, `NeverStopAcked`, `NeverPolicyJudged`, `NeverResolved`, `NeverDealt`, `NeverReadAsked`, `NeverMerged`, `NeverFixed`, `NeverEscalated`, `NeverNeedReleased`, `NeverCannotRead`, `NeverLandRequested`, `NeverLanded`, `NeverRed`, `NeverAfterHeld`, `NeverCarried`, `NeverUndealt`, `NeverLandJudged`, `NeverGated`, `NeverHeldBehind` and `NeverHandLanded`.

# Layers 4 and 5

**`LandEdge.tla`** (`LandEdge.cfg`, `docs/LANDING.md`): the landing ladder, from a tick's land request to the base. Cards c1 and c2 are stream s1's, c3 is s2's; both streams land on one base, every card's branch cut from the first base commit. CI is truthful: a tree holding c1 and c3 is red (a semantic conflict across streams) and a tree holding c2 and c3 does not merge. `Tick` is layer 3's landing in one atomic step; `Pass` is one landing pass of layer 4 (all heads already on base are landed at once; one head whose branch holds base's tip gates its own PR; otherwise the heads are merged onto base's tip into the group branch and its PR gates; a conflict stops the pass; a green gate merges only while base is still the tip it was gated on). The coordinator's answer lands by hand, fixes the card or retries. Invariants: `TypeOK`, `BaseNeverRed`, `NothingLandsUnpassed`, `MergingCovered`. Witnesses (`witnesses/LandEdge*`): `LandEdgeJudgementHoldsStream`, `LandEdgeRedLands`, `LandEdgeStaleGate`. Reach: `LandEdgeNeverHandLand`, `LandEdgeNeverJudged`, `LandEdgeNeverRedGated`, `LandEdgeNeverRungTwo`.

**`Runner.tla`** (`Runner.cfg`, `docs/LAYER-4-EDGES.md`): `nova-worker` on one bench with two consumers, width 2, one slot and one crash. Both consumers are dealt working, in the row's hand, at the start; `Take` is the row's acknowledgement and the start of its job. Passes and jobs are separate processes: a pass is the wake loop's pass (idle or planning) and ends whatever its jobs are doing; a job is the rest of each consumer's actions (stage, run, harvest, rebase, push, report, clean). `Stop` is the worker's own stop: each live harness is killed and its job kept at staged to be resumed, never a fault. `Release` is the restart's no-blame give-back of a job it cannot resume. A branch is its holder's: `branch[c]` records who wrote the card branch's tip (none, this job, or the card's next holder), whether that holder wrote it, and how many times this job pushed after its consumer was final. Invariants: `OneLiveHarnessPerJobDir`, `PidRecordedAtStart`, `JobDirNeverReused`, `HarnessInsideSlot`, `CleanOnlyAfterRecordedAndPushed`, `ReportRecordedAtMostOnce`, `FaultSpendsNoAttemptOrBounce`, `WorkingWithinWidth`, `SlotsWithinCapacity`, `PassNeverWaitsOnJobs`, `StopIsNeverAFault`, `NoPushAfterFinal`, `BranchOnlyByHolder`; properties `Liveness` (a working consumer's job is eventually recorded or cleaned) and `FinalDirRemoved`. Witnesses: `witnesses/Runner*` (eleven faults). Reach: `RunnerNeverStopped`, `RunnerNeverTakesWhileAJobRuns`, `RunnerNeverResumedAfterStop`, `RunnerNeverReleased`, `RunnerNeverRetired`, `RunnerNeverHolderPushedOverJob`.

**`Policy.tla`** (`Policy.cfg`, `docs/LAYER-5-POLICY.md`): policy versions, activation and the per-tick fallback. Versions 1..`MaxVersion` are stored in order and never deleted; `Default` is the built-in, never stored, and can fail too. `breaks` is the set of versions that fail (a runtime error, a refused answer or the deadline) on the current snapshot; `Drift` moves the snapshot, so `breaks` changes arbitrarily. A tick answers with the first version in order that passes; when every version tried fails the tick is answered by `NoAnswer`: it still applies every move that needs no answer and deals nothing. Activate answers the built-in's judgement too and is one op fenced at its snapshot's seq. Invariants: `TypeOK`, `ActiveStored`, `AnsweredByUsable`, `TickAlwaysAnswered`, `ActivateMovesNoCard`, `OneJudgementPerVersion`, `WaitKeepsItsBound`, `FallbackWasActive`, `NoAnswerStartsNothing`. Witnesses (`witnesses/Policy*`): `PolicyActivateMovesCard`, `PolicyActivateUnstored`, `PolicyBreakingUsed`, `PolicyBrokenDefaultUsed`, `PolicyDefaultSkipped`, `PolicyFallbackNeverActive`, `PolicyJudgementEveryTick`, `PolicyNoAnswerDeals`, `PolicyTickStalls`, `PolicyWaitRebound`. Reach: `PolicyNeverDefaultAnswers`, `PolicyNeverJudged`, `PolicyNeverNoAnswer`, `PolicyNeverPrevAnswers`, `PolicyNeverRollback`, `PolicyNeverWaitAcrossActivate`.

# Layer 6: the run and setup

`Run.tla` (chooser, FIFO tick, store contract and pusher), `RunClear.tla` (delivery across an epoch change), `Setup.tla` and its variants (`SetupWalk`, `SetupRun`, `SetupChoices`, `SetupChoicesWalk`) and `SetupHandoff.tla` (the two-phase handoff between `up` and its child) are described, with their assumptions, instances and reversed witnesses, in `LAYER-6-MODELS.md`.

The layer 6 runs are listed in manifests (`layer6-runs.json`, `layer6-clear-runs.json`, `RunStop-runs.json`, `SetupWalk-runs.json`, `SetupChoices-runs.json`, `SetupChoicesWalk-runs.json`, `SetupHandoff-runs.json`); each entry names the module, the cfg and the expected outcome (`pass`, `invariant` or `temporal`). `ci-tlc.sh` runs them with everything else, and `python3 run-layer6.py [--manifest FILE] [--only NAME...]` runs one manifest sequentially, two workers per run, with a two-minute timeout each. Witnesses for the Run models are in `witnesses/layer6/`; those for the Setup models sit beside their models.

# Trace validation

A model checks the design; trace validation checks that the code is that design. The Go walks record every step they take, projected onto the model's variables, and TLC checks that each recording is a behaviour of the model. The shape is that of `StepsTrace.tla`: the trace is a generated constant module `<Model>TraceLog.tla` defining `TraceLog == << r_1, ..., r_n >>`; the trace module (`<Model>Trace.tla`) has one variable `l`, the index of the last record matched; `TraceNext` takes record `l + 1` only through the action it names and requires the model's projected state after it to equal the record's; the property is `TraceMatched == <>(l = Len(TraceLog))`. TLC exit 0 is a matched trace; exit 13 (`TraceMatched` violated) leaves `l = k` in the last state, so record `k + 1` is the step the model refuses.

Each record carries only the entries that changed since the record before; an entry not named must be unchanged, so every step still pins the whole projected state.

Trace modules: `StoreTrace.tla`, `CardMachineTrace.tla`, `StepsTrace.tla`, `PolicyTrace.tla`, `LandEdgeTrace.tla`, `RunnerTrace.tla`, and for layer 6 `RunTrace.tla`, `RunClearTrace.tla`, `SetupTrace.tla`, `SetupChoicesTrace.tla`, `SetupHandoffTrace.tla`. The mapping of each layer 6 trace to the code is in `<Model>Trace-MAPPING.md`.

## Running

From the repository root, on a bench host:

```sh
bash tla/make-traces.sh          # the walks write their traces under tla/traces/
bash tla/make-traces.sh check    # ... and TLC checks every trace, in parallel
bash tla/check-trace.sh Store tla/traces/store-fake-1
bash tla/check-trace.sh CardMachine tla/traces/card-walk-3
```

`check-trace.sh <model> <trace dir>` prints `TRACE OK steps=<n>`, or `TRACE REFUSED at k of n: <action> (...)` followed by the model's state before the step and record k. A trace's cfg has an `\* expect:` line (`matched` or `forbidden at K`); the script exits 0 when the outcome is the expected one, 1 when it is not, 2 on any other TLC failure. The CI job `trace` runs `make-traces.sh check` on every push, with `NOVA_TRACE_PG=1` so a missing `initdb` fails the job instead of skipping the PostgreSQL walk. The layer 6 traces are emitted and checked by `check-layer6-traces.py` (`CHECK-LAYER6-TRACES.md`).

The walks write traces only when `NOVA_TRACE_DIR` names a directory (`make-traces.sh` sets it); without it the test suite is unchanged. The walk traces (`store-fake-*`, `store-pg-*`, `card-walk-*`, `policy-walk-*`, `steps-walk-*`, `landedge-walk-*`, `runner-walk-*`) are regenerated, not committed. The committed fixtures under `traces/` are the breakers (`store-breaker`, `card-breaker`, `policy-breaker`), `store-cursor-zero`, and the model's own `steps-ok-*` and `steps-bad-*`; the breaker tests compare them against what the code writes now.

## The walks and their seeds

- Store (`store/trace_test.go`): a walk on the Fake with transport faults (`Fake.Drop`: a lost request is `LoseRequest`, a lost reply after commit is Apply with known=FALSE, followed by Replay or a second Apply), and a functional walk on PostgreSQL with no faults. The generator (`store/trace_gen_test.go`) draws ops over the fixed names (producers p1 p2, cards p1 p2 t1 u1 u2 u3, rows r1 r2, judgements j1 j2, cursor c1, bodies a b, tiers flash pro, hook sessions x y with either role, the stop on and off), biased toward existing entities.
- Card (`card/trace_test.go`): the card walk on the Fake, recording each accepted op.
- Policy (`policy/trace_test.go`): the policy walk on the Fake.
- Steps (`steps/trace_test.go`): the Go steps driving one of the model's scenarios on `store.Fake`, each step one of the model's actions (Snap, Tick, Take, Report, Hold, Unhold, Gone, Seen, EdgeResult) picked at random and recorded with the model's variables projected from the code's state by the refinement mapping in `steps/trace.go` (`StoreTLA`, `InboxTLA`, `InflightTLA`, `JudgementsTLA`). A record logs `w` in parts (`ps`, `up`, `sh`, `tko` whole and `cs` as the slots that are not `none`, which `StepsTrace.Parts` checks), the inbox, the in-flight requests and the open judgements of the model's three kinds, and a Tick's `answer`. The tick plans on the view as Snap left it, so a move between Snap and Tick races it as in the model. The cfg sets the model's bounds above anything a walk reaches and the scenario's Budget and Pack. The model's own fixtures (`steps-ok-*`, `steps-bad-*`, from `make-steps-traces.py`): a reach check's counterexample is a behaviour of the model and must match whole; a state-invariant witness's counterexample is a behaviour of the faulted model whose last step the real model forbids (`expect: forbidden at K`).
- Landing (`edge/git`) and runner (`edge/worker`): refinement walks of `LandEdge.tla` and `Runner.tla`.

## The refinement mappings

`store.TraceState` (`store/trace.go`) and `card.TraceState` (`card/trace.go`) are the mappings, each documented with every model variable it fills and every abstraction. `store.TraceOp` maps an op to the model's Ops entry and refuses what the model has no word for. In brief:

- Store: epoch, cards, rows, judgements, cursors and policies are projected; events, ledger, applied, receipt and lost are the model's own bookkeeping, checked through the record's action and its `known` and `accepted` flags. A card's After, Attempt, Review, Finding and non-normal Priority are not in `Store.tla`, so a walk op that sets them is refused by `TraceOp`; needs are a set, so a need named twice is refused (the code refuses it too). A consumer's tier is not in the model; the generator gives a consumer put beside its producer the producer's tier, which the code requires. PutStream is dropped (one stream).
- Card: producers project to `ps`, consumers to slots `<<p, i>>` in the order attempt, work before read, review, read index; heads to their index in the walk's head table (the base is 0); a finding to "fix" or "-"; flaps counted. One stream.

What a trace module adds to its model, each a lifted bound or a move outside the machine:

- `StoreTrace`: none; `MaxLoss` is 2, every op may be lost.
- `CardMachineTrace`: `TraceInit` takes the rows as the walk's world has them; `env` is raised on every environment move and never bounded; Gone past the model's flap bound of 1 (`GoneAt`); `RawHold`, the walk's raw layer 3 stream hold, which parks nothing (HoldStream is the machine's).

The card walk does not reach Reached, Waive, Relink, WaiveRed, Released, multi-row AskReads, Add, HoldStream, Preempt or any InThisOp move.

## The breakers

Each breaker test (unit tier, the Fake only) writes a fixture that `check-trace.sh` must refuse at its last step:

- `store-breaker`: a store that strips RowHeld conditions accepts a deal fenced on `RowHeld(r1, FALSE)` after r1 was held. Refused at 4 of 4 (Apply o3).
- `card-breaker`: Start with its SetCardAttempt write dropped leaves p1 working at attempt 0. Refused at 3 of 3 (Start).
- `policy-breaker`: a policy walk the model refuses at its last record.

`store-cursor-zero` is `SetCursor{c1, 0}` on a cursor never set: the store refuses it ("cursor c1 is already at 0"), as `Store.tla` does (an absent cursor is 0 and a write that changes nothing is refused), and the fixture is `matched`.

# Not modelled

Layer 3 does not model: holds' Withdraw of untaken consumers after the grace, the retire and fail limits (Block at the bounce limit, Withdraw on silence, Gone, flap), sentinels and `landed-sentinel`, drops (NeedFailed over a closure, Waive, Relink), backed-up landing groups and the rungs (those are `LandEdge.tla`'s), `conflict` and `base-red`, the alarms (`stall`, `idle`, `land-rate`), judgement deadlines and expiry defaults, the per-card request verbs and their actor rule, op ids (an op is its body here; distinct bodies in one tick follow from `OneGroupPerCard`), and the `Park` step: the hold parks in its own op and every return to ready under a hold writes waiting (CardMachine `Back`), so a ready producer the hold's op missed does not arise; the step is a defence. The engine judgement for a group refused on fresh state is not modelled either: `RefusedOnlyByRace` shows no op is refused but by a race when the steps pre-check every store rule (`StepsSelfRefusal` shows what takes that path). A stale report is the report verb's under `docs/LAYER-6-RUN.md` and `CardMachine`'s `OldAttemptNeverMoves`.

Choices the model makes where the designs leave room:

- The fence is on the gens the plan predicts: an op's group reads the view with the tick's accepted ops applied, or an op whose group read a card an earlier op of the same tick wrote would be refused by the tick itself (`StepsFenceOnView`).
- A group can be larger than the budget: the budget counts cards touched (a Start touches two, a preemption four), so the first non-blocker group of a tick is always admitted.
- A land result whose op was refused cannot be derived again from the store; it is put back in place in the inbox (`CarryFIFO` holds with it).
- Resolve and ReadYes/ReadFix groups come after landing groups and before the deal.
- Preemption takes free width (working under width); room elsewhere does not count (`StepsBlockerQueued`).
- Reports are ordinary store ops and reach the tick through the store, not the inbox; the inbox carries edge results only.
- A refused op is split by group next tick; only the inbox carries.
- An edge request id `<<epoch, kind, card, sha>>` gives the second landing of one head after a red the id of the first (with the base unchanged); the model does not redeliver after a result is consumed, so it does not show a late redelivery of the first result read as the second's.
- Escalation falls back to higher ranks, then to lower ones that still serve the tier, when no row of the desired rank has room.
- `CardMachine` is not composable: its card sets are fixed by `Scenario`, so `Steps.tla` restates its guards and invariants.
