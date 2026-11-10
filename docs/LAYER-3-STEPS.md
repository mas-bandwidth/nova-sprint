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
- Ready, deciding LESSONS 2.20: every hard need landed, the card not held
  and the stream not held. Only a NEEDS orders starts: two producers that
  name the same file both start, and landing stitches them (the group
  rung, then a land judgement). Room on a row is not a condition: ready
  producers queue and the deal fills rows, so ready never flips with row
  load (LESSONS 3.2). `Park` is for held streams only. Until ready, the
  view names the cause (a need, or a hold). Policy: none; needs are hard.

### Holds

- Plans: `Park` for a ready producer of a held stream that the hold's op
  missed; `Withdraw` of every live consumer of a row that is held, down or
  not hooked in, acknowledged or not, each released with the row's reason,
  branch and head kept, its producer dealt elsewhere (Glenn, 2026-11-10: a
  held or down row holds no card; the hold, the Gone and the unhook return
  the hand in their own op, and this catches a hand an older op left);
  `Withdraw` of any consumer still `ready` (dealt before the deal became
  the take), dealt again in hand. A hold never touches landing (LANDING
  step 9). Model: Steps.tla `ReturnHand`, `HandEmptyWhenClosed`.
- Policy: none (no grace: the Gone itself waits row_silent_gone).

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
- Plans: `Start{row, branch, base}`, branch `sprint/<stream>/<producer>` (the stream group branch is `sprint-group/<stream>`, beside `sprint/` and never in it, so git never meets a ref that is both a file and a directory; defect 46, 2026-11-07);
  `AskReads{rows}` cutting every read a head needs in one plan, to distinct
  rows, none the author (LESSONS 3.4). The deal is the take (below, "In
  hand"): a row is dealt while its working consumers are under policy's
  `work` answer, at most its width, and the consumer is cut working
  (LESSONS 3.3). A ready producer no row has room for stays ready, queued.
- A ready producer whose tier no open row lists opens an `unserved`
  judgement per tier (LPM R31); a review no row may read opens
  `cannot-read`, so a review never waits on nothing.
- Policy: `order(rung, producers)` (score, then id); `ahead(row)` is no
  longer read (nothing waits ready in a hand; its answer is owed to layer 5
  to retire); `rows(candidates, rows)`, which picks each candidate's
  rank and row (below, Escalation; then an idle lane first, most room,
  name, never a row the card was withdrawn from, LPM D22; for a read, one
  read on a row whose model differs from the author's when one is open,
  LESSONS 3.8); `may_take(row, stream)` (every serving row). Width is the
  store's row column, set from config by the row verbs, never policy's
  (LESSONS 1.12).

### In hand (defect 52, intent rule 33: nothing sits unworked)

- The deal is the take: a consumer is in its row's hand, `working`, from the
  op that cuts it, and the deal's push (layer 6) names the card, the repo,
  base and branch (a read's head), the brief clipped, `take` and the finish
  or read command. The deadline runs from the deal.
- The row's `take` is its acknowledgement: a cursor, `AckCursor(epoch, c)`,
  written once; it moves no card.
- Nudge: a working consumer its row has not acknowledged, timeouts
  `ready_nag` (30 s in default.star; the human's number of 2026-11-07 was
  1 s) after the deal and until
  the row's first report, gets a `NUDGE` line each tick in the human's words
  ("you have a new ready card. you have x ready cards, you are currently
  working on y, and your width is z. Please work to width!") naming the
  card, how long ago it was dealt and the `take` command. Nudges are
  `Plan.Nudges`: run pushes them outside the event log, and they move no
  cursor.
- Ready timeout: at timeouts `ready_take` (180 s, the human's number) a work
  card still not acknowledged is withdrawn and its producer dealt, in the
  same group, to another row with room, never the slow one (`redeal`).
- At `ready_take`, a card no other row can take (a read, or no room
  elsewhere) is withdrawn back to the queue, its branch kept and the reason
  recorded ("in <row>'s hand N s, not taken"); the deal ranks the row it was
  withdrawn from last (item 2, 2026-11-08: no card is left in a hand that
  never took it). There is no `unacked` judgement.
- `undealt`: a ready producer still ready `UndealtAfter` (2 s: more than one
  one-second tick; a guess) after it went ready, while an open row serving
  its tier has room, opens one judgement naming the row, the card and the
  cause (the tick's budget spent before it, or an engine defect). Checked
  after the deal, so a producer dealt this tick is skipped.
- `admission`: layer 6 re-proves a stream's waiting and ready producers
  with check's admission each time the stream's base moves (run's
  `Admission`, verbs `ReproveStream`) and passes the refused ones in
  `Inputs.Unadmitted`. Such a producer is never dealt nor preempted for, and
  is not counted undealt; each stream with one has one `admission`
  judgement naming its cards and the first refusal and fix, cleared when
  none fails or waits (Steps.tla `admitted`: `NeverDealtUnadmitted`,
  `AdmissionShown`; witnesses `StepsDealtUnadmitted`,
  `StepsAdmissionUnshown`; the walk's `Refuse` and `Admit`).
- `held` for a route: a producer a `missing-route:` fault held waiting
  (LAYER-2, defect 48) gets one `held` judgement naming the row, the card
  and what is missing; it waits, never dealt again by itself, until
  the coordinator answers (`unhold` or `drop`).
- `held` for a refutation (`refutedHeld`, live 2026-11-09): a producer a
  `fail` refuting its brief held waiting (LAYER-2, `card.Refuted`) gets one
  `held` judgement naming the row and carrying the worker's reason; it
  waits until the coordinator drops it, replaces it with a corrected brief
  (`replace <id> --by <card> --reason <why>`) or unholds it.
- `held` for no progress (`limits`): a ready producer whose latest work
  attempts, running, failed with no head pushed on the brief as it is now
  (a fault or a withdraw that pushed nothing passed over; an ok, a pushed
  head or an edited prose, paths or done-when ends the run), at least
  policy's `same_finding` of them since its last unhold and all dealt once
  escalation was spent (its bounces before the run at least
  `frontier_after`), is held (`HoldCard`, "held: failed <n> top-rank
  attempts running with no new head on the same brief, the limit is <n>")
  with a riding `held` judgement, as at the bounce limit. `same_finding` is
  LESSONS 5.9's "the same finding twice means the brief is wrong", applied
  to fails; escalation keeps its room (defect 75). With the default policy
  (bounces 12, frontier_after 4, same_finding 2) a card that pushes nothing
  is held at 6 bounces instead of 12.

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

(Rebuilt to intent rule 32, 2026-11-07; LANDING.md "The ladder, as built".)

- Plans: an edge request per stream with none in flight, from the merging
  producers not under a land judgement: fewer than `backed_up` waiting, the
  first in score order alone (rung 1); `backed_up` or more, all of them in
  score order as one request (rung 2). An `after` orders only producers
  both queued and never holds on one that is not (LESSONS 2.15). From a
  result: `landed` is one group of `Landed{Head, Merge}` for every card
  still merging at its head; anything else is one `land` judgement per such
  card with the facts and the choices land, fix and retry, and nothing
  moves. A merging producer left with no request, no judgement and no
  after target merging is reported as an `engine` judgement.
- Landed means on main (LANDING.md, 2026-11-08): a land result says whether
  its merge is on main (`InboxItem.Main`); one that is not records
  `OnBase` and the card stays merging. Producers on base are never asked to
  land again: per stream with any, not judged, and no promotion in flight,
  one `promote` request carries their merges (`EdgeRequest.Merges`); its
  landed result is `Landed` for each card whose merge it lists, and
  anything else is the same `land` judgement per card.
- The mechanics (PR, group branch, gate, merge) are layer 4's; there is no
  bisection and no blame. Request ids hash epoch, stream, base, cards,
  heads and reviews, so a restart reissues the same request (LPM R22).
- Policy: `landing(stream)`: `backed_up` (default 4), `flowing` (validated,
  unused), `ci_wait` (default 600 s, a guess); `ledgers(repo)` (none).

### Retire and fail limits

- Plans: `HoldCard` (reason `held: ...`) with a `held` judgement when bounces since the
  producer's last unhold (`bounces - bounce_base`) reach the limit (a fault never counts;
  LESSONS 2.24; an unhold is a fresh budget), or when the reads of a producer's
  review and head have faulted policy's `faults` limit running (3, a guess), the
  reason naming the newest fault (live 2026-11-09: one read faulted 378 times
  on the same missing route); `Withdraw` of a working consumer whose
  row is silent past the bound (a row not seen on the bus since the run started counts
  from the view's start), or that is past its tier's deadline (timeouts `consumer`,
  after the deal and `ready_take`) with no report, branch kept and the reason
  recorded (item 2, 2026-11-08); `Gone` for a row whose session
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
  merges or lands (LESSONS 5.7). At the deadline a judgement whose cause a
  step detected this tick is never closed silently: it expires as
  `reopened as <kind.about.n+1> (repeat n)` and that episode opens in the
  same op, its question headed `UNANSWERED n TIMES, ESCALATED: `, so it is
  pushed again as NEEDS YOU; one whose cause no step detects closes as
  `cleared` (tla/Steps.tla `ExpiryNeverSilent`, witness
  `StepsExpiresSilently`).
- The closed list a step opens: `held`, `unserved`, `cannot-read`,
  `land`, `stall`, `idle`, `land-rate`, `flap`, `brief`,
  `landed-sentinel`, `policy`, `engine`, `roster`, `no-model`,
  `no-policy`, `undealt`, `admission` (`conflict`, `base-red`,
  `no-ci` and `forge` went with intent rule 32, 2026-11-07). What v1 escalated besides (failed
  work, bounds, lateness, rebase conflicts, read broken) is a rule move in
  the tick (LESSONS 3.17).
- Alarms: `stall`, nothing landed in the bound while producers wait in
  review or merging, cause per stream; `idle`, working under half the open
  width while producers wait, causes by count, closed back over three
  quarters (LESSONS 3.18); `land-rate`, a stream's or model's landed with no
  bounce over landed, under target over a sample (LESSONS 3.21), while the
  stream has a producer left to land (cleared once it has none).
- Policy: `deadline(kind)` (none for `held` by Glenn's ruling, else one
  hour), `idle_floor` (0.5), `land_target` (0.3 over 20).

### Requests

Each per-card verb is one step: event, actor check, `card.Move`, one op. The actor rule LAYER-2 left here: UnholdCard, Waive,
Relink, SetPriority, WaiveRed, Cancel, Replace, Edit, Rescore, the holds
(of a row, a stream, or a producer card: `HoldCard` with reason `held: <why>`
and its live consumers withdrawn in one op; `unhold` is its `UnholdCard`) and
judgement answers are the coordinator's (the rows nova-config names; an
engine fact, since a wrong coordinator could corrupt); Report is the
holder's, through the inbox (there is no Take event; `take` is the
acknowledgement cursor); Add is anyone hooked in. Refused:
"SetPriority is the coordinator's; ask on the bus: `nova-bus send ...`".

## Policy questions and their defaults

Glenn's rule: if being wrong could corrupt, it is in the engine; if only
slow, it is policy. Layer 3 delegates exactly the questions named above plus
`budget` and `pack`; legality, needs, holds, width, the coordinator set, op ids
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
2. No row is over width: deals (the deal is the take) are tallied against
   width minus working, and every cut is checked by the card machine
   against the row's roster width.
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
   serving it has room under its width, in rung order, or an `undealt`
   judgement names it within `UndealtAfter`; a waiting
   producer whose needs landed is held, with its reason, or resolves.
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
  plans nothing when policy fails; a stale report applied; ready
  conditioned on room (ready flips with row load).
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
  preemption, drop, hold, bounce-hold and burst paths tick by tick, every op
  accepted; an op refused by a racing verb, then split by group; an unknown
  outcome replayed; each fold equal to a fresh snapshot; a random walk
  checking the model's invariants after every op.
- Load, extending `modules/store/load_test.go`: 5,000 producers; fold, plan and
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
  the release; checked in `modules/card/move.go`, `report` writes the consumer only.

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
| 2.20 ready that cannot start | Sentinels and needs (ready: needs, hold; never room) |
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

## Built (rowan's child, 2026-11-07; branch `v2-l3-steps`, package `steps`)

What the code is, where it differs from the text above and why, every number
marked measured or guess. The model is `tla/Steps.tla`; the code follows its
tick order and cites its invariants (`tla/README.md`, "The steps (layer 3)").

**Inputs.** `Inputs{Now, Inbox, Inflight, Heads, Seen, Refused, Roster}`.
Reports and takes are the rows' own ops (LAYER-6 wins; the fold shows them);
the inbox path for them (`Kind report`, `take`) is built with every refusal
of "Reports" in its words, for a layer 6 that routes them here, and is not
in the model. Edge results (`land`, `stop`) are the model's inbox. `Seen` is
when each row was last on the bus (layer 6 keeps it; a row absent from it
fires nothing). `Refused` is the last tick's refused ops from their
receipts: an op's `Note` carries its groups' keys as JSON (`OpGroups`), so
layer 6 splits a refused op by group and puts its items back in place; `Plan`
has the four fields exactly. `Roster` is nova-config (Glenn, 2026-11-06: the
one true source; nothing of it in the store): a row's kind, width and tiers
are read by name, a store row the roster lacks is never dealt to and is a
`roster` judgement. `Inputs.Roster` is package roster's `Roster` (store
round 22, merged from `v2-integrate-22`); `card.RowFromStore(r, width,
tiers)` takes them from it. An open row is up, not held and hooked in to the
sprint (Glenn, 2026-11-06: the hook is the subscription to the pushes): the
row's `Session` and `Role` set, the role worker or coordinator (who counts
as a worker). Round 22 took width out of the store and `card.Take` does not
check it, so width at a take is held only by the tick's inbox path
(`takeItem`'s tally) and must be the take verb's check in layer 6: the
tests take as that verb would (`roomToTake`); one line for rowan.

**The asker.** Layer 5's question set, names and types as that doc has them
(`Questions`, `Answers`), one call per tick with every subject; `budget` and
`pack` are layer 3's keys in `limits`. Absent, failed (`Failed[q]`, or `*`
for the budget spent), out-of-domain or not-offered answers are the default
for the subject with one `policy` judgement per question and version; no
policy (version 0) is every default and no judgement. Every limit layer 5
validates is read from policy's `limits` answer, the steering numbers
(`escalate_after`, `frontier_after`, `flap_limit`, `idle_floor`,
`land_target`) included; `Engine.Limits` is the built-in default for a
failed or absent answer: `escalate_after` 2 and `frontier_after` 4
(guesses), `flap_limit` 3 (unmeasured), `idle_floor` 0.5 (idle closes at
the engine's 0.75), `land_target` 0.3 (over the engine's sample of 20),
and layer 5's defaults for the rest of `limits` (bounces 12,
not the 3 this text guessed: layer 5 is the later word), `timeouts`,
`landing` (backed up at 4, flowing at 1) and `width` (ahead = width, where
the model's instance uses 2 x width; the trace walk's asker answers the
model's). `victim` is not a question: the youngest working consumer on the
row is the engine's fixed rule (LAYER-5's boundary, LESSONS 5.12); the model
lost its `victim` oracle to match. The tick budget (cards touched) is 250 and `pack` 50, both
measured below.

**The steps.** Planned in this order, each skipping claimed cards: reports
and takes (so a consumer that has reported is never preempted), preemption,
land results, needs (drops, Reached with a `landed-sentinel` judgement in
the op, Resolve), holds (Park, withdraw of untaken
consumers on held rows at once and down rows after `row_silent_gone`),
limits (HoldCard with its judgement in the op, withdraw of a silent row's
working consumers, Gone, flap), fix (every yes before every fix, as the
model orders them), the deal by rung (blocker, critical, read, high,
normal, low; within a rung policy's `order` then id), land requests,
judgements. The packer orders blocker groups first (preemptions, then
blockers' starts), then the rest as planned: the model's `E`. A machine
refusal while planning is an `engine` judgement, never an op. Planning is
bounded by the budget (guarantee 11): once the non-blocker groups planned
touch the budget's count no more are planned, while the tally, the claims
and the judgements go on as the model's folds do, so the admitted plan is
the same. Rows written by a group (Gone) are claimed like cards, so no
group of a tick fences a record another group writes and the fence is the
view's gens (the model's predicted fence reduces to it). Land request ids
hash epoch, stream, cards, heads, reviews and base (model finding 8). The
landing rung has no hysteresis: backed up at or past `backed_up` merging,
else flowing (`flowing` unused). Since 2026-11-07 (below) every result but
`landed` is a `land` judgement; `base-red`, `base-green`, `conflict`,
`no-ci` and `forge` are gone.
Judgement ids are `kind.about.episode` (a cause opens once while one is
open; a flap's episode is its count), group key the kind (`policy:v<n>` for
policy), deadline `timeouts.judgement` except `held`; at the deadline
the expiry is the kind's safe default, nothing moves. The closed list gains
`roster` (above). Facts a snapshot lacks (when a consumer was taken, a row
went down, a row faulted, a stream last landed) are witnessed by the fold
and are unknown for records older than the view; bounds then run from
`Since` (Build's clock) and the victim among unwitnessed takes is by id. A
cold view's consumers are in id order, a folded one's in cut order (the
model's slots). Drops: a failed need's closure fails through `NeedFailed`
(`InThisOp` past the first hop); a dependant with a live consumer is
unreachable (a dependant is waiting), the withdraw is kept as defence.
`Park` is a defence too (the hold's own op parks). Requests (`Verb`): the
actor table (coordinator verbs, the holder's take and report, the run
loop's row facts, anyone's Add), a drop with its dependants' chosen Waive or
Relink or their `NeedFailed` in one op, a judgement answered with its move
beside it (the answer's condition is not one the card machine merges, so
it rides outside `Merge`). Stella's two findings (read of 03f44371): a
later report or take of a consumer a group of this tick moves is carried,
never acked from a pending group; an item's answer rides with its group and
goes out only when the budget admits it.

**The group.** `groupOp` composes a group's moves as one `card.Merge` of
the plans each move builds on the view (built against `card.Merge` of
`v2-card-fix4`, b2b0b8c2); card round 7's `Merge` over moves is an edit in
that one function. A group's key is the cards its plan writes plus the card
it is about, with the gens read, the row written and the inbox item.

**Trace validation.** `modules/steps/trace.go` is the refinement mapping to
`Steps.tla`; `modules/steps/trace_test.go` walks the model's four scenarios on
`store.Fake` (every seed and scenario: 100 x 2,000 for depth; 4 x 3 x 150 as
the fixed fixtures CI checks with `tla/check-trace.sh`); `StepsTrace.tla`
logs `w` in parts. All 12 walk fixtures and the model's 13 own fixtures
match (hetzner2, 2026-11-07). What it found: the first walk folded the
tick's view after every outside move, so the tick planned on fresh state
where the model planned on the snapshot (the walk, not the code); and the
model fenced a row by its up flag where the store fences its gen, so a row
that went down and came back passed the model's fence and failed the
store's (the model, fixed: `rg`).

**Measured** (vision, Linux, quiet under `nova-reserve --exclusive`,
2026-11-07; 5,000 producers in 20 streams over 100 rows of width 4;
`NOVA_LOAD=1 go test -run TestFunctionalLoad ./modules/steps/`): the fold of a tick's events 12.9 ms p50
(13.5 p95); the plan at a budget of 250 cards with 2,500 producers ready
47 ms p50, 71 p95, 78 max (the bar of 50 ms p50 met; before the planning
was bounded by the budget it was 1,342 ms: the deal ranked every ready
producer and the overlap tally was O(waiting x resolved)); the ops per tick
at pack 1/10/50/250: 244/25/5/2. On PostgreSQL (fsync on) the same tick
applied as 250 ops of 1 group: 1.83 ms p50 per op, 457 ms the tick; 25 of
10: 9.8 ms, 246 ms; 5 of 50: 35 ms, 203 ms; 1 of 250: 207 ms, 207 ms. So
the per-op fixed cost is about 1.1 ms and packing 50 groups per op takes
98% of the gain of one op while a refusal costs 50 groups, not 250: `pack`
is 50 (measured) and the budget stays 250 (measured: the full tick at
5,000 producers is fold 13 + plan 47 + apply 203 = about 265 ms p50, under
the one-second p99 bar with room; the layer 1 tick alone was 119 ms p50 at
the same size).

**Tests.** Unit (`go test ./modules/steps/`, no clock, sockets, network or store but
`store.Fake`): the view (a fold per event kind equals a fresh build), the
purity lint (also every function under 60 lines), the asker (absent,
erroring, slow, out of range, in domain), the paths (happy, fix, red and
retry, fault, preemption, preemption refused by a race, drop, holds, row
hold and down, hold at the limit, budget and carry, refused op split by
group, unlisted row, unhooked row, silent row and flap, stable op ids), the
verbs (the actor table, Add/Take/Report, a drop's dependants, a judgement
answered), the random walk (100 seeds x 2,000 steps, 50% of picked moves
legal, the model's state invariants after every step and its tick
invariants on every plan, every op accepted since the sim folds and applies
at once), the trace walk. The suite is green under race on vision
(2026-11-07, `go test -race -skip Functional ./...`, steps 185 s).

**Size.** 3,435 lines of non-test Go (view 290, inputs 110, asker 300,
roster 100, group 230, tick 220, pack 170, reports 130, preempt 130, land
210, needs 115, holds 45, limits 100, fix 100, deal 300, judge 260, request
230, trace 195): over the 3,000 budget by 15%; the requests, the roster and
the trace mapping were not in the first estimate's 3,000.

**Not built as written.** The ledger test (`tests/enforced`) indexes the
steps; its remaining failures after the merge are policy's rows (tests the
ledger does not index while policy does not build on the integration
branch). `same_finding` counts reviews running: the `brief` judgement
opens when that many reviews in a row, ending at the current one, joined
their fix reads to the same finding (LESSONS 5.9 in part). The route rest
is per row: `route_rest_after` faults among the row's last
`route_rest_window` finishes the fold saw. `reports_per_tick` caps the
inbox reports planned per tick; the rest carry in order. `land-rate` is per stream, not per route. `base_regate`'s timing is the
edge's. `stitch` and `prices` are not consumed here. Reports arriving
through the inbox are not in the model (LAYER-6's path is).

**Stitch, landing results (2026-11-07, branch `v2-stitch-c3`).** A `red`
land result's `Reason` (the failing checks, the first failing URL, the gate's
reason and the gated sha, from run's edges) is written into the `card.Red`
reason as `CI red at <head> on <base>: ...`, and layer 2 now sends the
producer to fix with it as the finding (intent rule 30; it was `review`).
Two outcomes are new: `no-ci` (the gated group sha got no check within run's
`CIAbsentWait`, defect 40) opens one `no-ci` judgement about the stream naming
`sprint/<stream>/group` and the fix; `forge` (run's forge has no token,
defect 39) opens one `forge` judgement with the refusal's fix. Neither moves
a card; the request leaves flight and is asked again under the open
judgement. `Steps.tla`'s red result moves the producer to `BackOf` (fix)
the same way; the burst scenario's MaxAtt is 2 (it was 1) so a red's fix is
dealt again and the carry stays reachable. Tests: `TestTickRedAndRetry`
(the fix attempt carries the finding), `TestTickNoCIJudges`.

**Stitch, the landing to intent rule 32 (2026-11-07, branch
`v2-stitch-w2L`).** Superseding the stitch above where they differ. The
landing's automatic path is green-and-merge only: `landResults` moves cards
only on `landed` (with `card.Landed{Head, Merge}`, the merge sha), and every
other outcome (`ci_red`, `no-ci`, `conflict`, `merge_refused`,
`pr_unopenable`, `branch_failed`, `ci_fault`, `missing_head`, `head_moved`,
`hand`, `fault`) opens one `land` judgement per card still merging at the
request's head (`land.<card>.<n>`, group key `land`, no deadline; choices
`land | fix | retry`), with the question naming the card, stream, base and
its tip, branch, head, outcome, the edge's facts, and the cards landing
together. Nothing is blamed: the `red`, `no-ci`, `conflict`, `base-red` and
`forge` kinds and their handling are removed. A judged card is left out of
the next request, so a judgement holds that card only, not its stream. The
answers are a coordinator `Request`: `land` carries `card.Landed` (the
merge sha from the hand merge), `fix` carries `card.Red` with the reason as
the finding, `retry` is the answer alone (the next tick asks a new
request). `landRequests` now names the card branch `sprint/<stream>/<id>`
(it was the producer's empty `Branch`), passes policy's `ci_wait` as the
request's `Wait`, and ends with `unlanded`, the engine report of a merging
producer with no request, no judgement and no after hold (intent rule 32;
by construction none). Policy's default `backed_up` was 2 at this round;
default.star's is 4 now, so rung 2 is "four or more heads waiting". `Steps.tla`: `LandJ`, `Judged`,
`LandJudged` (a result's judged items), `Answer(p, choice)` in any phase,
the check `MergingCovered`, the reach `StepsNeverLandJudged` and the
reversed witness `StepsLandAfterHoldsUntilLanded` (the after hold kept until the
earlier card lands, not while it merges); `jsnap`, the open judgements as the tick's snapshot
read them, which the tick's picks and its checks use (the code plans on its
view, so an answer between the snapshot and the tick is seen by the next
tick; the first bench trace check refused three walks at that race until
the model carried it); `ItemGroup` moves only on `landed`, the red result's `ApplyG`
went, and `EdgeAfterAccepted` is weakened to state the stale-request race
(LANDING.md, "Known gaps"). The trace walk answers open land judgements
(`answerOne`) and `StepsTrace.tla` maps `Answer`. Tests:
`TestTickLandJudgementEveryOutcome`, `TestTickLandAnswers`,
`TestTickLadderRungs`; the trace fixtures `steps-ok-*` and `steps-bad-*`
regenerated from TLC on a bench, with `steps-ok-land-judged` new.
Measured on the bench (TLC 1.8.0, at the `jsnap` model): the witness
`StepsLandAfterHoldsUntilLanded` violates `MergingCovered` after 186
distinct states (330 generated); a first fault, one open land judgement
holding every stream, found no violation (1,016 distinct states,
exhaustive), because in the land scenario the other card is after-held by
the judged one, so it was replaced; `make-traces.sh check`: 81 traces, the
`ok` ones `TRACE OK` and the six `steps-bad-*` refused as they must be.

## Built: a drop releases its consumers (stitch w3A, 2026-11-07; directive 77)

`dropMoves` (a Cancel or Replace request) now begins with `withdraw` of each live
consumer of the dropped producer, reason `card.ReleasedPrefix + <the drop's reason>`
(no result: released is neither ok nor fail), and plans the producer's move
with those consumers read as failed, so the one op is Withdraw (which releases a
working producer to ready), then Cancel. A dependant waits on a need and has no
live consumer, so only the dropped producer is released. Tests:
`TestVerbDropReleasesLiveConsumers` (a ready and a working consumer). The model
`Steps.tla` has no coordinator drop verb; the machine's Withdraw and Cancel are
`CardMachine.tla`'s, composed as `CardMerge.cfg` checks.

## Built: routes in the deal (stitch w2A, 2026-11-07)

A producer whose brief carries `NEEDS-ROUTE: <name>` lines (read once, by
`card.NeedsRoutes`) is a candidate only for rows that serve its tier and
every route (`Roster.ServesRoutes`); the candidate cache keys on tier and
routes. With none, the `unserved` judgement's About is the tier plus
`+<route>` per route, and it clears only when a row serves all of them, so a
tier-only row no longer clears it each tick. Test:
`TestTickRoutesDealOnlyToServingRows`. No TLA model names routes; the
candidate filter narrows the row set the model already chooses from.

**Stitch, what the deal carries and when the tick deals (2026-11-07, branch
`v2-stitch-w2D1`).**
- *The deal names the model (defects 54, 57).* A consumer is cut with the
  model its deal named (`card.Start.Model`, `card.AskRead.Model`,
  `AskReads.Models` one per row; store round 23's `cards.model`), so the
  take receipt reads it from the consumer and take never chooses one. A
  friend's model is the roster's (`Roster.Kin[row].Model`); a bench's is
  the active policy's answer for the card's tier, `catalog().bench[tier]`,
  in provider/model form, passed in `Answers.Models` (`modules/steps/model.go`). A
  candidate row with no model to name is not dealt to; when no serving row
  can be named one, the card stays where it is and one `no-model`
  judgement about the tier says why (tier, the row kinds, the policy
  version, and the fix: `bench` in the policy, or the friend's model in
  nova-config). It clears when an open serving row can be named one.
- *The policy gate (defect 53).* While no policy is active, or the active
  one fails as a whole (run's `Answers.NoDeal`, layer 6), the tick deals
  nothing: no preemption, no Start, no read. One `no-policy` judgement on
  the coordinator's row (the first hooked coordinator, else `sprint`) says
  so with the fix, with no deadline (it stands until it clears: a policy
  answers again). Reports, the inbox, resolve, fix, landing and judgements
  go on. This is the one exception to guarantee 10: a question one active
  version fails still falls to the default with a `policy` judgement; only
  no version, or a version that fails whole, stops the deal.
- *Overlap is a file (defect 50).* Two producers of a stream overlap only
  on a path both name that is a file (`Concrete`: not empty, no `*?[`, no
  trailing `/`); a glob or a directory both list never holds one back. The
  view indexes only concrete paths. A producer held behind another opens
  one `overlap` judgement about it whose question is `held behind <card>
  on <path>` (the least such card by id, its first shared file), where
  the board and `check` show open judgements; it clears when the card is
  no longer waiting or the reason changes.
- *Resolve from state (section 64b).* Already so in the steps before this
  change: `needs()` reads every waiting producer of the view each tick and
  resolves one whose needs are all landed, whatever landed them (the land
  step, a store op such as the reconcile's `SetCardState`, a restore). The
  22-producer stall the section reports was not reproduced here; a stall of
  that shape is what the old overlap rule (a glob or directory both listed)
  produces, which the overlap change removes. Tests:
  `TestTickResolvesFromState` (a need landed by a store op, and a cold
  view), `TestTickOverlapIsAFile`, `TestTickDealNamesTheModel`,
  `TestTickNoModelNoDeal`, `TestTickNoDealWithoutPolicy`.
- *Judgements indexed.* `pending`, `isOpen` and `episodes` were scans (of
  the tick's planned judgements and of the view's); with thousands of held
  producers each opening an overlap judgement the needs step was
  quadratic (measured on the working machine, not quiet: 52 ms p50 for
  4,908 opens). They are now lookups: a per-tick index of the planned ones
  and one of the view's, built on first use.
- *Measured, the resolve cost* (`NOVA_LOAD=1 go test -run TestFunctionalLoadResolve
  ./modules/steps/`; the needs step alone, 5,000 waiting producers whose needs a
  store op landed, 20 streams, 100 rows, 20 ticks; ms p50/p95). On the
  working machine (darwin, not quiet): free (all 5,000 resolve) 7.97/10.57;
  held (92 resolve, 4,908 open an overlap judgement) 4.67/7.98; held-open
  (4,908 waiting, their judgements open and checked for clearing)
  3.47/3.81; the whole plan 10.5, 12.6 and 10.8 p50. On a Linux bench
  under other runs' load (load average 92 on 32 cores, so an upper bound,
  not a quiet number): 28.4/52.3, 37.3/74.3 and 32.0/57.7; the plan 31,
  110 and 85 p50. Every tick re-reads the state, so no incremental index
  is kept: at 5,000 waiting it is under 10 ms on the working machine; no
  quiet Linux bench number was taken.
- *The model.* `Steps.tla`: `w.pol` (an active policy answers; in pre it
  fails and is fixed as an environment move) gates `PreStep` and
  `DealStep` (`NoPlan`) and opens `no-policy`; `NoDealWithoutPolicy`
  checks no preemption, Start or read is accepted while it is down and the
  judgement is open. Paths carry the glob `a/*` (p and q both list it);
  `Overlap` counts only files (`Shared`); a held producer opens `overlap`
  (`HeldBehindShown`). In deal a store op may land p by hand between ticks
  (`HandLandOK`: no land request in flight for it); `ResolveFromState`
  checks, stated apart from `Overlap` and `ResolveOK`, that every producer
  the view's state makes ready is resolved or held behind one resolved
  this tick on a file. `ReadyDealtWhenRoom` exempts a run with no active
  policy. Reversed witnesses: `StepsDealsWithoutPolicy` (the gate ignored),
  `StepsResolveOnEvent` (resolve only in the tick that consumes the need's
  landed result), `StepsGlobOverlaps` (a glob both list counts),
  `StepsHeldUnshown` (no overlap judgement); reach checks
  `StepsNeverGated`, `StepsNeverHeldBehind`, `StepsNeverHandLanded`. The
  trace maps `overlap` and `no-policy` judgements; `pol` is not logged
  (the walk's asker always answers). TLC on the bench (2026-11-07,
  `bash tla/ci-tlc.sh`): 251 runs ok, none failed; the five Steps scenarios
  pass, the four new witnesses violate their targets, the three new reach
  checks are reached. `StepsResolveOnEvent`'s counterexample is the hand
  land (no edge result arrived) then a tick: the store-op path. The steps
  trace fixtures were regenerated from the new model's TLC logs, and
  `make-traces.sh check` matched all 82 traces (the Go walks included).
**Stitch, deal is take (2026-11-07, branch `v2-stitch-w2D2`; intent rule
33).** The deal cuts the consumer working (LAYER-2 "Deal is take"), so the
steps changed with it:

- `deal.go`: only `room` changed in logic (policy's `work` answer, no
  longer plus `ahead`: a row has room while its working consumers are under
  it), plus the doc comments of `deal` and `withRoom`. Nothing else in that
  file changed.
- `hand.go` (new): `AckCursor`, `Nudge`, `unacked` (nudges and the
  `unacked` judgement), `undealt`, `routeHeld`. `tick.go` runs `unacked`,
  `undealt` (after the deal) and `routeHeld` and returns the nudges in
  `Plan.Nudges`. `holds.go` withdraws consumers left `ready`, and on a held
  or down row only the unacknowledged ones. `view.go` keeps `TakenAt` (when
  a consumer went working, the deal), `ReadyAt` and `Acked`. `request.go`
  takes a card hold (`holdMoves`: the `HoldCard` merged with the `Withdraw` of
  each live consumer). `asker.go`: timeouts `ReadyNag` 1 s and `ReadyTake`
  180 s (the human's numbers, 2026-11-07, policy's `timeouts()`),
  `UndealtAfter` 2 s (a guess).
- Model: `tla/Steps.tla` has no `Take` and no `Ahead`; cuts are working and
  their slots join the taken order; `Eligible` is `Used < Width`. New
  variables `rdy` (ticks each producer has been ready) and `ju` (open
  undealt judgements), and the property `DealtWithinKTicks`: a producer
  ready `UndealtTicks` (2) ticks is dealt or has an undealt judgement. New
  invariant `DealIsTake`. Reversed witnesses: `StepsCutReady`,
  `StepsUndealtSilent` (no undealt judgement: the property breaks in burst,
  budget 1), `StepsDealOneShort` (replacing `StepsReadyCountedAgainstWidth`);
  reach check `StepsNeverUndealt`. `StepsWidthOver` now runs in burst.
- Not trace-bound: the undealt judgement is counted in ticks in the model
  and in seconds in the code, and `ju` is not in the trace record (the
  trace's judgements are `unserved`, `cannot-read` and `policy`); nudges and
  `unacked` are outside the model (it has no clock for them). The steps
  trace walk has no take: an acknowledgement moves no card.
- TLC and traces (measured, bench host, cd960636): the full `ci-tlc.sh`
  run passed (250 as expected), with `DealtWithinKTicks`, `DealIsTake`,
  `StepsCutReady`, `StepsUndealtSilent`, `StepsDealOneShort` and
  `StepsNeverUndealt` as required. `StepsDealtTwice` stopped violating once
  a blocker preempts only when no row has room (the deal cannot then place
  it either); it gained a second fault, a preemption with free width. The
  trace check refused `steps-walk-land-1` at a Tick: the model asked a read
  of a review in a held stream, so a failed `rows` answer opened a policy
  judgement the code never opens (its `dealRead` returns first); the model
  now skips held streams' reads. The `steps-ok-*` and `steps-bad-*`
  fixtures are regenerated from that run's logs.
- Seen in the tests: with a tick budget of 1, each tick's `Resolve` of the
  next producer spends the budget and the deal never reaches the first
  ready producer; the `undealt` judgement names it with the budget as the
  cause (`TestHandUndealtJudged`). At the default budget (250) this needs a
  burst of over 250 resolves in a tick; not measured.
- Tests: `hand_test.go` (nudge then unacked, no nudge after a report,
  undealt with and without room, a route fault waits held and is never dealt
  again, a plain fault re-deals); the sims' `take` is the acknowledgement.


**Stitch w3I: the landing, the deal's model and deal-is-take together
(2026-11-07, branch `v2-stitch-w3I`).** w2D1 and w2D2 were built apart from
the landing (w2L); merged, superseding them where they differ:
- The closed list of judgement kinds is the landing's (`land`; no
  `conflict`, `base-red`, `no-ci`, `forge`) with w2D1's `overlap`,
  `no-model`, `no-policy` and w2D2's `unacked`, `undealt`. No deadline:
  `held`, `land`, `no-policy`. The view's judgement index (w2D1) is
  `jindex()`; `judged(p)` stays the landing's land-judgement test.
- `unserved` is opened by the deal only (w2D1), with w2A's routes in its
  About (`unservedAbout`); the coordinator events take the landing's
  `Landed` and `Red` and w2D2's `HoldCard` (the card hold).
- `undealt` counts room only on rows the deal can name a model for, and
  nothing while the policy gate deals nothing: `no-model` and `no-policy`
  already say why those cards wait (before this, a gated or model-less
  ready card also opened an `undealt` judgement calling it an engine
  defect; `TestTickNoDealWithoutPolicy`, `TestTickNoModelNoDeal`).
- One vocabulary of routes: a `missing-route:` fault's first word is the
  route's name (`card.RouteName`), the name a brief declares with
  `NEEDS-ROUTE:` (w2A) and a machine serves as `route:<name>`; the held
  judgement's fix says so (replace the card with a brief that declares the
  route; or, when it already declares it, fix the row that serves it
  without having it). The harness prompt asks for that form.
- Model (`Steps.tla`): w2D2's deal-is-take (no `Take`, no `Ahead`, `rdy`
  and `ju`, `DealIsTake`, `DealtWithinKTicks`) with the landing's `jsnap`,
  `Answer` and `MergingCovered` and w2D1's `pol`, hand land, `NoPlan`,
  `ResolveFromState`, `HeldBehindShown`. `und` counts nothing while
  `~V.pol` (the code's gate); `RoomFor` is `w.pol /\ RoomIn(w, p)`. The
  hand land (EnvMove) needs no open land judgement: a judged card's hand
  land answers the judgement in the same op (verbs `handLand`), which is
  `Answer(p, "land")`. Every witness and cfg regenerated by
  `make-steps-witnesses.py`; `StepsReadyCountedAgainstWidth` stays retired
  (w2D2's `StepsDealOneShort`).
- The stream fence (`rsx`) covers the groups that read the stream (pre,
  start, read, fix, resolve), not `yes` or `landed`: CardMachine's ReadYes
  and Landed and `modules/card/move.go` read no stream. Found by the trace check
  (`steps-walk-land-1`, record 38): a Hold between the Snap and the Tick
  refused the model's yes op on the fence while the code moved p to
  merging. The model followed the code and the card machine, which agree.
- Checked at `ca1c7d28` on a bench (measured): `tla/ci-tlc.sh` 260 of 260
  runs ok (every passing cfg clean; every witness and reach check
  violated as required, among them `DealIsTake`, `DealtWithinKTicks`,
  `MergingCovered`, `NoDealWithoutPolicy`, `ResolveFromState`,
  `HeldBehindShown`, `StepsNeverGated`, `StepsNeverHeldBehind`,
  `StepsNeverHandLanded`, `StepsNeverUndealt`); `make-traces.sh check` 88
  of 88 ok (the 12 Steps walks, the 8 `steps-ok-*` fixtures matched, the
  6 `steps-bad-*` refused at their last record). The Steps fixtures from
  that commit's TLC logs are byte-identical to those of `e8852d30`.
## Superseded: inherited red worded for a glance (stitch w3L, 2026-11-07)

Removed 2026-11-08: a red base is the coordinator's whole-picture judgement
(LANDING.md "A red base"), so `ci_red_inherited`, `InheritedRed` and its
"land it" wording are gone; red is one `ci_red` land judgement with the
choices land, fix and retry. `Steps.tla` is untouched.

## Built: why the fleet is idle (stitch w4V, 2026-11-07)

`steps.Waits(cards)` (`waits.go`) explains each stream's waiting producers for
the idle judgement, `check` and the board, so the three say one thing: each
waiting producer is counted once, on the front of the NEEDS chain it waits on
(its first need not landed, followed while that need is itself waiting on a
need not landed; a need naming no card is "missing") or, with every need
landed, on the least producer and first file it is held behind. The holder
rule is the needs step's own: `heldBehind` now calls `leastHolder`, shared
with `Waits` (no change in behaviour; `TestTickOverlapIsAFile` unchanged).
`Waits` also finds each stream's longest NEEDS chain over cards not landed or
failed (a sentinel adds no length and is not listed; a cycle a store op
wrote ends). `IdleWhy` is the summary ("N chains (longest K, top: <card>
blocks M), H overlaps (top: <card> holds C on <file>)") and the command for
the top card: show it when in flight, judge it when held, relink away from
it when missing or failed, and `replace` for a NEEDS that is not a consumed
output; never a priority (intent rule 14: set by request only).

The idle judgement's text (one function, `idleCauses` in `judge.go`; the
alarm's condition, `cleared` and the choices are unchanged): when no ready
producer is dealable (every ready one in a held stream or unserved) and none
resolved this tick, it reads "working W of O open width while R ready and N
waiting: nothing dealable: <IdleWhy>; ready: <held> in held streams,
<unserved> with no serving row; <command>"; otherwise it keeps the held and
unserved counts. It is built only when the judgement would open (one open or
pending skips it). No action, state or judgement kind changed, so
`Steps.tla` and its generators are untouched.

Cost: `BenchmarkWaits5000` (5,000 producers in 50 streams, a chain of 20
and 40 on one front per stream) is 3.9 ms on the working machine (measured,
one run, Apple M3 Ultra). Tests (unit, no clock): `TestWaitsGroupsByChainFrontAndHolder`,
`TestWaitsLineNamesThreeAndCounts`, `TestWaitsSurvivesACycle`,
`TestIdleWhyAndFix`, `TestIdleJudgementSaysWhyNothingDeals`.

## Removed: the PATHS-overlap start hold (2026-11-08)

A waiting producer that named a file a ready or started producer of its
stream named was held from the deal, with an `overlap` judgement ("held
behind <card> on <path>"), overlap counts in `check`, the idle judgement
and `steps.Waits`. It serialized cards with no real dependency, against
the promise of parallel work at width. It is gone: only a NEEDS (B
consumes A's landed output), a hold, a missing serving row and width keep
a ready card from starting. Two cards that edit one file both start; the
landing ladder (the group rung on `sprint-group/<s>`, then a land
judgement) and the coordinator's merge stitch them. PATHS stays for
admission (each path exists at the base) and for the worker's seal step,
which commits only uncommitted changes inside PATHS; committed changes are
not checked against it (the reader, the PR and CI catch one outside
it). Removed with it: the view's paths index, `Concrete`,
`Overlaps`, `heldBehind`, `leastHolder`, `Holder` and the `overlap`
judgement kind (an open one left in a store expires at its deadline).
The model loses `Overlap`, `NeverOverlapStarted`, `HeldBehindShown`, the
witnesses `StepsNoOverlapCheck`, `StepsGlobOverlaps` and
`StepsHeldUnshown`, and the reach check `StepsNeverHeldBehind`; the reach
check `StepsNeverBothStarted` shows p and q of one stream, naming one file,
working together. Test: `TestTickSameFileCardsBothStart` (replaces
`TestTickOverlapIsAFile`). The sections above that describe the hold are
history.
