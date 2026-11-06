# Layer 3: the steps

A draft for Glenn, 2026-11-06, not yet approved. It stands on layer 1
(locked at 81bc9c24) and layer 2 as built, and asks layer 2 for one
addition ("Asked of the layers below").

Pure Go, package `steps`: no store, clock, bus or git. From one view of the
sprint, the tick's inputs (the clock value, what arrived on the bus, the git
facts the run loop read) and policy's answers, `Tick` returns one tick's
plans: ops for layer 1, edge requests for layer 4, answers owed to rows.
Layer 6's run loop gathers the inputs, applies the ops, hands on the edge
requests and pushes over nova-bus. Every move is a layer 2 move; a step
decides only which moves this tick holds, in what order, within what budget.
Glenn, 2026-11-06: "Let's not throw away what we learned in v1, which is always batch."

## The list for Glenn

The structures of this layer, bottom up, each secured before the next:

1. **The view**: the snapshot plus indexes (producers by state and rung,
   consumers by row, dependants by need, live paths by stream, counters),
   advanced by folding `Events`. Guarantees: after any fold it equals a
   fresh build at the same seq. Checked: a fold test per event kind; the
   random walk on `store.Fake`.
2. **The inputs**: `Now`, the inbox (reports, takes, CI and land results,
   in arrival order), stream heads, presence, edge requests in flight.
   Guarantees: no other time or outside read. Checked: a lint (no
   `time.Now`, `net`, `os/exec`, `store.Store` in the package).
3. **The asker**: one interface over layer 5, a default per question.
   Guarantees: an absent, failing, slow or out-of-range answer is the
   default for this tick, recorded once; never an illegal move, never a
   stall. Checked: unit tests with a policy that errors, times out and lies.
4. **The group**: a producer with the consumers its move touches (a report
   with its `Finished`, `Broken` or `Faulted`; a preemption with its deal; a
   drop with its dependants), as one `card.Merge`. Guarantees: one group per
   card per tick; never split across ops. Checked: the model
   (`OneGroupPerCard`, `GroupWhole`).
5. **The steps**: one function each, below. Guarantees: only layer 2 moves,
   and every predictable store rule pre-checked, so an op is refused only
   by a race. Checked: table tests; conformance on Fake and PG.
6. **The packer**: orders groups, fills the tick budget, packs groups into
   ops up to the packing size, names each op by a hash of its body, carries
   the rest. Guarantees: budget kept, carry never lost or reordered, one
   body one id. Checked: the model (`BudgetKept`, `CarryFIFO`); unit tests.
7. **The tick**: step order, the refused-op rule, the cost bound.
   Guarantees: the invariants below. Checked: `tla/Steps.tla`; the load
   test at 5,000 producers.
8. **The requests**: the per-card verbs, with the actor rule layer 2 left
   here. Guarantees: a verb refused for its actor writes nothing. Checked:
   a table test of every verb and actor.

```go
func Tick(v *View, in Inputs, ask Asker) Plan
type Plan struct {
    Ops     []store.Op      // packed groups, in priority order
    Edges   []EdgeRequest   // merge, gate, push, stop a runner: layer 4
    Answers []RowAnswer     // acks and refusals to rows, by result id
    Carry   []InboxItem     // what the budget left, in arrival order
}
```

`Asker` asks each question once per tick with all its subjects. The view is
folded after every apply; a snapshot per tick is a defect (LAYER-1).

## The steps (a refusal is one line naming the fix)

### Reports

- Inputs: inbox reports (result id, row, session, consumer, result, head,
  reason, cost) in arrival order; the consumer, producer and row.
- Plans: `MoveConsumer(Report)`; for the current work consumer of a working
  producer the same group moves the producer (`Finished`, `Broken`,
  `Faulted`; one plan, asked of layer 2 below), so no producer is left
  working under a reported consumer. A read report moves only the read.
- Refusals: unknown consumer ("card C does not exist: run `nova-sprint next
  --as R`"); not the holder; a session the row no longer has ("report from
  session S1; row R is now S2; nothing applied"); withdrawn ("consumer C was
  withdrawn: preempted by blocker X; branch B is kept"); an old attempt or
  an old head; not taken ("take it first: `nova-sprint take C@G`"). A report
  matching a done consumer's result and head is acked from the record
  ("applied at seq N"), never applied twice, so layer 4 may redeliver.
- Takes ride the same inbox: `MoveConsumer(Take)`, at most width minus
  working per row by an in-tick tally, so the store's width write never
  refuses an op; the excess is refused ("row R is at width 2: finish
  C first").
- Policy: none; reports are facts.

### Sentinels and needs

- Inputs: dependants of each need landed or failed since the last tick.
- Plans: `Resolve`; `Reached` for a sentinel, with a `landed-sentinel`
  judgement in the group; for a failed need, one group with
  `NeedFailed{InThisOp}` over the whole closure of dependants, or the
  `Waive` or `Relink` the drop named.
- Ready, deciding LESSONS 2.20: every hard need landed, the stream not
  held, and no started producer of its stream (working, review, merging)
  overlapping its paths, ledgers aside (LANDING step 2). Room on a row is
  not a condition: ready producers queue and the deal fills rows, so ready
  never flips with row load (LESSONS 3.2). Resolve counts the stream's
  ready producers as started for the overlap check, so two overlapping
  producers are never ready together and a start never strands a ready
  one; so `Park` stays for held streams only. Until ready, the view names
  the cause (held, overlap with X). Policy: none; needs are hard.

### Holds

- Plans: `Park` for a ready producer of a held stream that the hold's op
  missed; `Withdraw` of a held or down row's untaken consumers after the
  grace, releasing their producers to be dealt elsewhere; working consumers
  stay. A hold never touches landing (LANDING step 9).
- Policy: `regrace(row)`: 0 held, 10 minutes down (unmeasured).

### Preemption

- Plans: with no free width on an open serving row, one group: `Withdraw` of a
  working consumer there (releasing its producer, score and branch kept)
  merged with the blocker's `Start` to that row, and an edge request to stop
  the runner. Glenn, 2026-11-06: "blocker is so high, it'll kick out the
  youngest work or read task on a worker to get started right away." At
  most one per row per tick. No serving row at all: an `unserved` judgement
  per tier, and the blocker stays ready.
- Policy: `victim(blocker, candidates)`; default the youngest by take time.

### Deal and reads

One candidate list down Glenn's ladder, "low, normal, high, read, critical,
blocker in specific order": blocker work, critical work, every read owed,
high, normal, low; within a rung, policy's order.

- Inputs: ready producers; producers in review owed reads; open rows (up,
  not held, not rested) with tiers, rank, model, width, working and ready
  counts; stream heads (the `base` a `Start` records).
- Plans: `Start{row, branch, base}`, branch `sprint/<stream>/<producer>`;
  `AskReads{rows}` cutting every read a head needs in one plan, to distinct
  rows, none the author (LESSONS 3.4). A row is dealt while ready plus
  working is under width plus `ahead`; width caps working only (LESSONS
  3.3). A ready producer no row has room for stays ready, queued.
- A ready producer whose tier no open row lists opens an `unserved`
  judgement per tier (LPM R31); a review no row may read opens
  `cannot-read`, so a review never waits on nothing.
- Policy: `order(rung, producers)` (score, then id); `ahead(row)` (2 x
  width, LPM D21); `rows(candidates, rows)`, which picks each candidate's
  rank and row (below, Escalation; then an idle lane first, most room,
  name, never a row the card was withdrawn from, LPM D22; for a read, one
  read on a row whose model differs from the author's when one is open,
  LESSONS 3.8); `may_take(row, stream)` (every serving row). Width is the
  store's row column, set from config by the row verbs, never policy's
  (LESSONS 1.12).

### Fix

- Plans: `ReadYes` or `ReadFix{finding}` once every read of the current
  review is done or withdrawn, from policy's verdict over all of them
  (LESSONS 3.7). A producer back from `Broken` or `Faulted` needs no plan:
  its score puts it at the head of its queue and `Start` carries its prior
  head and finding (LESSONS 3.9). A finding byte-equal to the producer's
  current one opens a `brief` judgement (LESSONS 5.9, in part).
- Policy: `verdict(producer, reads)`; default yes when every done read says
  ok, else fix with every finding joined; a split never merges by default.

### Land

- Plans: an edge request per stream with none in flight: flowing, the first
  merging producer alone; backed up, all of them in score order as one group
  with one CI. An `after` orders only producers both queued and never holds
  on one that is not (LESSONS 2.15). From a result: one group of `Landed` for
  every head that landed (the edge fast-forwards the stream branch); `Red`
  for the head bisection blamed; nothing for a bench fault (LANDING step 7);
  a `conflict` judgement for a group that cannot merge (rung 3); one
  `base-red` judgement per base, stopping a stream at its third meeting and
  resuming all it stopped when green (step 8).
- The mechanics (merge, rebase, gate, bisect, push, ledgers) are layer 4's
  script; an agent only when the script fails, and that is a judgement.
  Glenn: "work efficiently when merging". Request ids hash stream, heads and
  base, so a restart reissues the same request (LPM R22).
- Policy: `rung(stream, backlog)` (default backed up at 3 merging),
  `ledgers(repo)` (none), `stall_bound` (60 s), `regate(base)` (2 and 5 min).

### Retire and fail limits

- Plans: `Block` with a `blocked` judgement when bounces reach the limit (a
  fault never counts; LESSONS 2.24); `Withdraw` of a working consumer whose
  row is silent past the bound, branch kept; `Gone` for a row whose session
  has not acked past its bound, never for one missed challenge in a long
  turn (LESSONS 4.7); a `flap` judgement past the flap limit.
- Policy: `bounce_limit` (3; v1 left it unsourced, LPM G1); `silence`
  (2 hours of running time, unmeasured); `gone_after` (10 minutes);
  `flap_limit` (3); `fault_limit` (3 faults of the last 10 on a row in 30
  minutes rest it 30 minutes, LPM R34: a deal filter in the view, never a
  stored hold, since rows are never narrowed to steer deals, R37).

### Escalation

No tier is ever written: a card's tier is fixed at add and is its minimum.
Escalation and read tier are row choice. A row lists every tier it serves,
and its rank is the highest it lists; policy's `rows` answer picks the rank
for each candidate, and the engine refuses one below the card's tier.

- Default ranks: a work consumer at the card's own tier first; one rank up
  once the producer's bounces reach `escalate_after` (default 2,
  unmeasured), one more per further step; a read at the rank above the
  writer's row (frontier stays frontier).
- Policy: `rows`, `escalate_after`; an answer below the card's tier or off
  the row's list is the default.

### Judgements

- Plans: `PutJudgement` with id `kind.about.episode` (a cause opens once), a
  group key (one push for a shared cause; LESSONS 4.9), policy's deadline and
  the command that answers it; `ExpireJudgement` when the cause clears, or
  at the deadline with the kind's safe default, which never drops, accepts,
  merges or lands (LESSONS 5.7).
- The closed list a step opens: `blocked`, `unserved`, `cannot-read`,
  `conflict`, `base-red`, `stall`, `idle`, `land-rate`, `flap`, `brief`,
  `landed-sentinel`, `policy`, `engine`. What v1 escalated besides (failed
  work, bounds, lateness, rebase conflicts, read broken) is a rule move in
  the tick (LESSONS 3.17).
- Alarms: `stall`, nothing landed in the bound while producers wait in
  review or merging, cause per stream; `idle`, working under half the open
  width while producers wait, causes by count, closed back over three
  quarters (LESSONS 3.18); `land-rate`, a stream's or model's landed with no
  bounce over landed, under target over a sample (LESSONS 3.21).
- Policy: `deadline(kind)` (none for `blocked` by Glenn's ruling, else one
  hour), `idle_floor` (0.5), `land_target` (0.9 over 20).

### Requests

Each per-card verb is one step: event, actor check, `card.Move`, one op. The actor rule LAYER-2 left here: Unblock, Waive,
Relink, SetPriority, WaiveRed, Cancel, Replace, Edit, Rescore, the holds and
judgement answers are the coordinator's (the rows nova-config names; an
engine fact, since a wrong coordinator could corrupt); Take and Report are
the holder's, through the inbox; Add is anyone hooked in. Refused:
"SetPriority is the coordinator's; ask on the bus: `nova-bus send ...`".

## Policy questions and their defaults

Glenn's rule: if being wrong could corrupt, it is in the engine; if only
slow, it is policy. Layer 3 delegates exactly the questions named above plus
`budget` and `pack`; legality, needs, holds, width, overlap, the coordinator set, op ids
and the refused-op rule stay in the engine. Policy absent: every default.
An answer that fails (a Starlark error, the tick's 100 ms of policy time
spent, out of range, naming a row or card not offered) is the default for
this tick, with one `policy` judgement per version and question. Each
default deals, reads, lands or names a cause; none stalls.

## The tick

1. Fold events into the view; read the inputs.
2. Run the steps in this order: preemption, reports and takes, land
   results, needs and sentinels, holds, retire and fail limits, escalation,
   fix, deal and reads, land requests, judgements. A card claimed by an
   earlier step is skipped by the later ones until the next tick.
3. Order, then budget: blocker groups (never cut; one per row), reports
   and takes in arrival order, landing groups, drops and holds, limits,
   judgements, then the deal list by rung. The tick budget is policy's
   count of cards touched per tick, default 250 until the load test sets it
   (unmeasured; a measured burst cost about 1.4 ms a report). Glenn,
   2026-11-06: "if you distributed too many reports over several frames
   you'd stay in budget".
4. Pack: groups go into ops in that order, up to the packing size (`pack`,
   groups per op, a policy number whose default the load test measures; 50
   until then, unmeasured). A group larger than the packing size is an op
   of its own. The packing size is the lever the load test sets.
5. Carry. Only the inbox carries, in arrival order, at the head of the next
   tick's inbox; every other move is derived again from the store, so a
   carried deal is never stale.
6. Name. An op's id is `t<epoch>.<seq>.<hash of its body>`: one view, one
   id, applied once. An unknown outcome is replayed by id before the next
   tick plans.
7. Apply the ops in order, each fenced on the gens its groups read. A
   refused op changed nothing; the next tick rebuilds from the folded view,
   never resends the old body, and applies the groups of the refused op
   each as an op of its own: split by group, never by halves of writes. A
   group refused alone on fresh state is left out until its cards change,
   with an `engine` judgement naming the plan and the store's reason (a
   defect here or in layer 2).

## Guarantees

1. No producer is dealt twice: at most one group touches a card in a tick,
   and every `Start` is fenced on the producer's gen.
2. No row is over width: takes are tallied against width minus working, and
   the store's width write is the final check.
3. No landing holds on an unqueued soft `after`.
4. A ready blocker with no room is dealt by preemption in the tick that
   finds it ready, unless no row serves its tier, which is a judgement in
   the same tick.
5. A refused op never half-applies: an op is one transaction, and a group is
   never split across ops, so no preemption lands without its deal and no
   drop without its dependants.
6. A report for a stale head, attempt or session is refused to the row and
   moves nothing; a repeated report is acknowledged and applied once.
7. Every report and take that arrives is applied or answered: the carry is
   arrival order and reports come before deals, so a burst of B over budget
   N is through in ceil(B/N) ticks.
8. Ready never depends on row load; a ready producer is started when a row
   serving it has room under width plus `ahead`, in rung order; a waiting
   producer whose needs landed has a named cause (held, or overlap with X).
9. Every non-final producer is waited on by a named actor, the next tick or
   an open judgement (LAYER-2, nothing stalls).
10. No policy answer, absence or error makes a step plan an illegal move or
    plan nothing where the default would have planned.
11. The tick's cost is the snapshot plus ops x per-op cost: the fold of the
    events since the last tick (the steady-state snapshot), planning
    O(G log C + R) for G groups (at most the budget), C cards and R rows,
    and ceil(G / pack) ops at the measured per-op cost. Nothing per tick is
    O(C) after the cold build, which is O(C + events).

## The model: tla/Steps.tla

CardMachine.tla holds the per-card rules; the step model composes it: its
variables (producers, consumer slots, rows, the hold) plus `snap` (the view
at tick start), `inbox`, `inflight`, `js` (judgements), `now` and a monitor
of the last tick's groups.

- Actions: `Snap`; `Tick` (groups from `snap` by the step rules, under
  `Budget`, packed by `Pack` into ops applied in order, each atomic and
  fenced against the current state, else refused whole); `Verb` (a coordinator move between `Snap` and `Tick`);
  `Arrive` (a report or take, duplicates included); `EdgeResult`; `Clock`;
  `PolicyFails` (no answer or any answer; the default used).
- Invariants: guarantees 1 to 6 and 8, `OneGroupPerCard`, `GroupWhole`,
  `BudgetKept`, `CarryFIFO`, `RefusalChangesNothing`, and every
  CardMachine invariant. Liveness, weak fairness on `Tick`, rows and edges:
  guarantees 7 and 9 (the nothing-stalls obligation layer 2 handed here).
- Reversed witnesses, each a required violation: two groups on one card; a
  deal counting ready against width; a carry that drops overflow; a carry
  newest first (a report starves); a refused op resent blind; a refused op
  split by halves of writes (a preemption lands without its deal); an unqueued `after` holding a landing; a step that
  plans nothing when policy fails; a stale report applied; resolve without
  the overlap check (two overlapping producers ready); ready conditioned
  on room (ready flips with row load).
- Sizes: three producers (one a blocker), two rows of width 1 and 2, three
  slots, budget and pack 1 and 2, inbox 3; scenarios `deal`, `land`,
  `burst`; `tla/ci-tlc.sh` on hetzner2, never the working machine. The inbox
  is a Go slice, so the sequence is its layout (no ring, no layout model).

## Tests

- Unit, package `steps`: each step's table of view and inputs to groups,
  exactly; every refusal's words; every question absent, failing, slow and
  lying; stable op ids; packing, carry and the split by group. Time is
  `Inputs.Now`; no sockets, no network, no store but values.
- Conformance on `store.Fake` and PostgreSQL: happy, fix, red, fault,
  preemption, drop, hold, block and burst paths tick by tick, every op
  accepted; an op refused by a racing verb, then split by group; an unknown
  outcome replayed; each fold equal to a fresh snapshot; a random walk
  checking the model's invariants after every op.
- Load, extending `store/load_test.go`: 5,000 producers; fold, plan and
  apply timed apart at steady state, 200 reports/s, and a 2,000 burst.
  The bar: the full tick under one second at p99 and planning under 50 ms;
  the result is the per-op cost by packing size, which sets `pack` and the
  budget. Today the store tick alone is 119 ms
  at p50.
- Cold read questions: can one card be in two groups of a tick; what does a
  refused op do next tick; where does a report over budget wait; what if
  policy throws on `rows`; can a hold stop a landing; why can a waiting
  producer not oscillate; how does a card escalate with no tier write; which judgements are answered by rule; what is
  the tick at 10,000 cards; can an old session's report move anything?

## Size

About 3,000 lines of Go, tests excluded (view 600, steps 1,500, packer 250,
asker 300, requests 350): an estimate; layer 1's first was half its size.

## Asked of the layers below

- Layer 2: a current work consumer's `Report` carries the producer's
  `Finished`, `Broken` or `Faulted` in one plan, as `Withdraw` already folds
  the release; checked in `card/move.go`, `report` writes the consumer only.

## Lessons placed (LESSONS section 3, and the decisions owed here)

| lesson | where |
|---|---|
| 3.4 all reads in one tick to distinct readers | Deal and reads (`AskReads`) |
| 3.5 the reader's done-when judgement | deferred: the reader's per-item result is layer 4's report form, its weight layer 5's `verdict` |
| 3.6 broken only for a real defect | deferred: a typed defect field on a read report (layer 4) that `verdict` reads (layer 5); never matched from text |
| 3.8 reader a different model, adversarial brief | Deal and reads (`readers` default prefers another model); the adversarial read brief is layer 4's deal text |
| 3.17 mechanical judgements answered by rule | Judgements (the closed list) |
| 3.18 the idle alarm | Judgements (`idle`) |
| 3.21 first-time land rate | Judgements (`land-rate`) |
| 2.20 ready that cannot start | Sentinels and needs (ready: needs, hold, overlap; never room) |
| 2.21 nothing stalls, with the tick | the model's liveness; guarantee 9 |
| 1.14 running-time deadlines | in part: every bound is compared in one function of `Now`; excluding stopped spans is owed to layer 6's stop and start |
| 4.5 who redelivers | in part: the ack from the record (Reports); redelivery is layer 4's |
| 4.9 grouped notifications | Judgements (group keys) |
| 4.24 resting a route | Retire and fail limits (`fault_limit`) |
| 5.3 order within a level, tiers, escalation | the questions `order`, `rows` (rank by row choice); their scripts are layer 5's |
| 5.7 deadlines with safe defaults | Judgements (`deadline`, expiry default) |
| 5.12 blocker preemption | Preemption |

## Decided inside the layer (Rowan, 2026-11-06), easy to overrule

- Ops are packed groups, applied in priority order: a refusal costs only
  its op, split by group next tick. Only the inbox carries.
- A card moves once per tick (conditions are checked before writes, so two
  hops of one card cannot share an op): a tick per engine hop.
- Policy is asked per question per tick, a dozen calls whatever the size.
