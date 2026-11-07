# Layer 3: the steps

Package `steps`. Pure Go: no store, clock, bus, git or `os/exec`. From one view of
the sprint, the tick's inputs (the clock value, what arrived on the bus, the git
facts the run loop read) and policy's answers, `Tick` returns one tick's plans:
ops for layer 1, edge requests for layer 4, answers owed to rows. Layer 6's run
loop gathers the inputs, applies the ops, hands on the edge requests and pushes
over nova-bus. Every move is a layer 2 move (package `card`); a step decides only
which moves this tick holds, in what order, within what budget.

```go
func Tick(v *View, in Inputs, ask Asker) Plan
func Verb(v *View, in Inputs, r Request) (store.Op, *card.Refusal) // one coordinator or row request as one op

type Plan struct {
    Ops     []store.Op     // packed groups in priority order, then the judgement op
    Edges   []EdgeRequest  // land and stop requests: layer 4
    Answers []RowAnswer    // acks and refusals owed to rows, by result id
    Carry   []InboxItem    // what the budget left, in arrival order
    Nudges  []Nudge        // pushes to rows outside the event log
}
```

- [View](#the-view)
- [Inputs and Plan](#inputs-and-plan)
- [The asker and policy defaults](#the-asker-and-policy-defaults)
- [The tick](#the-tick)
- [The steps](#the-steps)
- [Groups, packing and refused ops](#groups-packing-and-refused-ops)
- [Requests (Verb)](#requests-verb)
- [Judgements](#judgements)
- [Why the fleet is idle](#why-the-fleet-is-idle)
- [Guarantees](#guarantees)
- [Testing](#testing)

The division of labour: if being wrong could corrupt, it is in the engine; if it
can only be slow, it is policy. Legality, needs, holds, width, overlap, the
coordinator set, op ids and the refused-op rule stay in the engine. Policy
answers ordering, row choice, verdicts, limits and timeouts, and an absent or
bad answer is always the engine's default.

## The view

`View` is the snapshot's records plus the indexes the steps read: producers by
state and by priority rung, consumers by producer and by row, dependants by need,
live concrete paths by stream. It is built once (`Build(snapshot, now)`) and
advanced by `Fold(events)`, the same fold as `store.Fold`: after any fold it
equals a fresh `Build` at the same seq. A snapshot per tick is a defect; the run
loop folds `Events` and keeps one view.

Facts a snapshot does not carry are witnessed by the fold and are zero for records
older than the view, in which case every bound they feed is measured from
`View.Since` (Build's clock):

| Field | Meaning |
|---|---|
| `TakenAt[consumer]` | when it went `working`: when it was dealt |
| `ReadyAt[producer]` | when it last went `ready` |
| `Landed[stream]` | when a producer of the stream last landed |
| `Down[row]` | when the row last went down |
| `Faults[row]` | when the row's consumers reported `fault` |
| acknowledgements | the `took:<epoch>:<consumer>` cursors (`AckCursor`), kept across epochs |

A cold view lists a producer's consumers in id order, a folded one in cut order.

**Open row.** A row may be dealt to when it is `up`, not held, and hooked in to the
sprint: its `Session` set and its `Role` `worker` or `coordinator` (who counts as
a worker). A row not in the roster is never dealt to.

**Overlap is a file.** Two producers of a stream overlap only on a path both name
that is a *concrete* file (`Concrete`: non-empty, no `*?[`, no trailing `/`). A
glob or a directory both list never holds one back; a true conflict under a
shared directory is the landing's rebase and the read's job. The view indexes
only concrete paths of producers that are `ready`, `working`, `review` or
`merging`.

## Inputs and Plan

```go
type Inputs struct {
    Now      time.Time
    Inbox    []InboxItem          // arrival order; the last tick's Carry first
    Inflight []EdgeRequest        // edge requests dispatched, not yet answered by a consumed result
    Heads    map[string]string    // stream -> head of its branch: the base a Start records
    Seen     map[string]time.Time // row -> when last seen on the bus; a row absent from it fires nothing
    Refused  []RefusedOp          // the last tick's refused ops, from their receipts (OpGroups)
    Roster   roster.Roster        // nova-config: each row's kind, width, tiers, model and routes, by name
}
```

Nothing else is read (`TestLintPure`: no `time.Now`, `net`, `os/exec` or
`store.Store`, and no function over 60 lines).

The roster is nova-config, the one source of fleet facts; nothing of it is in the
store. A store row the roster lacks is never dealt to and gets a `roster`
judgement. Width, tiers, kind, model and routes are looked up by name through
the roster.

**Inbox items** (`InboxItem{ID, Kind, ...}`): `report` (a row's report: Row,
Session, Consumer, Result, Head, Reason, Cost, Priced, Usage), `land` (an edge
result: Request, Outcome, Landed heads, Merge sha, Reason) and `stop` (a runner
stop's ack). Rows normally apply their own reports as ops and the fold shows
them; the `report` path is for a layer 6 that routes them through the tick. There
is no `take` item: the deal is the take (below).

**EdgeRequest** is work for layer 4: `land` (Heads of Cards on their Branches in
stream `Stream`, `Base` the stream's tip as the tick saw it, `Wait` seconds a
gated sha may have no CI) or `stop` (the `Row`'s run of `Consumer`, after a
preemption). `ID` is stable across ticks so a restart reissues the same request
and results match by id. `After` names the op whose acceptance a request waits
for ("" for none): a stop goes out only with its preemption.

**GroupKey** (`Kind, Cards, Gens, Row, Item`) is what a group touched. An op's
`Note` carries its groups' keys as JSON (`OpGroups`), so layer 6 can split a
refused op by group.

## The asker and policy defaults

`Asker.Ask(Questions) Answers` is layer 5 as the steps consume it: one call per
tick with every question the tick has subjects for (`AskBudget` = 500 ms of policy
time). A subject absent from an answer map is a failure for that subject. An
answer the engine cannot use (absent, failed, out of its domain, naming a row or
card not offered, or the budget spent) is the engine's default for this tick,
recorded once as a `policy` judgement per question and version. Version 0 is no
policy: every default and no judgement.

| Question | Subjects | Domain checked | Default |
|---|---|---|---|
| `order(card)` | ready producers, reviews | tuple of at most 4 ints or strings | `(0, score)` for a fix attempt (has a prior head), else `(1, score)`; then id |
| `rows(card, kind, candidates)` | work and read candidates | a non-empty list of offered rows | the engine's ranking (below) |
| `reads(card)` | reviews | 1..5 | 1 |
| `verdict(card, reads)` | reviews whose reads are settled | `yes` needs a read that said ok; `fix` a fix read; `retry` when none said either | any fix read: `fix`; else any ok: `yes`; else `retry` (every read faulted: ask one more, never a yes) |
| `width(row)` | rows | `0 <= work <= width` | `work` = the roster width |
| `landing(stream)` | streams with merging heads | `0 <= flowing < backed_up <= 1000`, `ci_wait` 0..86400 | backed_up 2, flowing 1 (unused), ci_wait 600 |
| `limits()` | none | every count in range, else the defaults whole | below |
| `timeouts()` | none | every bound 1..86400 s, `row_silent_withdraw >= row_silent_gone`, `base_regate` a rising list | below |
| `Models` (`Answers.Models`) | tiers | tier -> the model a bench runs, `provider/model` form | none: a bench cannot be dealt |

Engine defaults (`Engine`):

| Setting | Value |
|---|---|
| `Limits.Bounces` | 6 (a producer at or past it is blocked) |
| `Limits.Faults` / `SameFinding` / `ReportsPerTick` / `RouteRestWindow` | 3 / 2 / 1000 / 10 (accepted by `limits()`; the steps read no other field than `Bounces`, `RouteRestAfter`, `Budget`, `Pack`) |
| `Limits.RouteRestAfter` | 3 (faults within the rest window that rest a row) |
| `Limits.Budget` | 250 (cards touched per tick) |
| `Limits.Pack` | 50 (groups per op) |
| `Timeouts.RowSilentGone` | 600 s |
| `Timeouts.RowSilentWithdraw` | 1800 s |
| `Timeouts.LandingStall` | 60 s |
| `Timeouts.Judgement` | 3600 s (the deadline of a judgement) |
| `Timeouts.RouteRestFor` | 1800 s |
| `Timeouts.ReadyNag` / `ReadyTake` | 1 s / 180 s |
| `Timeouts.BaseRegate` | 120, 300 (read by the edge) |
| `EscalateAfter` / `FrontierAfter` | 2 / 4 bounces |
| `UndealtAfter` | 2 s |
| `FlapLimit` | 3 |
| `IdleFloor` / `IdleCeiling` | 0.5 / 0.75 |
| `LandTarget` / `LandSample` | 0.9 / 20 |

`Budget` and `Pack` were chosen by the load test (below); the others are layer 5's
defaults or unmeasured guesses. `escalate_after`, `frontier_after`, the flap and
alarm numbers are the engine's, not policy's.

**The gate.** When `Answers.NoDeal` is set (no policy active, or the active one
failed as a whole), the tick deals nothing: no preemption, no `Start`, no read.
One `no-policy` judgement on the coordinator's row says why. Reports, the inbox,
resolve, fix, landing and judgements still run. This is the one case where a
policy failure stops an action; a single failed question still falls to its
default.

## The tick

`Tick` runs these steps in order; each skips cards (and rows) an earlier step
claimed, so a card moves once per tick:

1. `rest` marks rows resting after faults (a deal filter, never a stored hold).
2. `unlisted`: a `roster` judgement for each store row the roster lacks.
3. `reports`: inbox reports.
4. `preempt`: blockers with no free width.
5. `landResults`: inbox land and stop results.
6. `needs`: failed-need drops, sentinels, resolve.
7. `holds`: park, and withdraw from held and down rows.
8. `limits`: bounce limit, silent rows, flaps.
9. `fix`: verdicts on settled reviews.
10. `deal`: ready producers and owed reads, down the ladder.
11. `undealt`: the stall check, after the deal.
12. `landRequests`: edge requests for merging producers.
13. `unacked`: nudges and the ready timeout.
14. `judgements`: alarms, policy misses, expiries.

Then the groups are ordered, budgeted, packed into ops, named, and the rest of the
inbox is carried. Planning is bounded by the budget: once the non-blocker groups
planned touch the budget's count, no more are planned, while tallies, claims and
judgements continue so the admitted plan is the same.

**Order.** Blocker groups first (preemptions, then blockers' starts; never cut
by the budget), then the rest as planned: inbox reports in arrival order, land
results, needs, holds, limits, fix, the deal by rung, land requests.

**Budget.** Admit every blocker group; the first non-blocker group always (so a
group larger than the budget is not starved); then each while the cards touched
stay within `Limits.Budget`, stopping at the first that does not fit, so the inbox
is consumed as a prefix.

**Carry.** Only inbox items carry, in arrival order, at the head of the next
tick's inbox. Every other move is derived again from the store, so a carried
deal is never stale. A burst of B items over budget N is through in ceil(B/N)
ticks, reports before deals.

**Packing.** Groups go into ops in order up to `Limits.Pack` groups per op. A group
larger than the pack is an op of its own. A group touching a card of a group of
the last tick's refused ops is an op of its own.

**Naming.** An op's id is `t<epoch>.<seq>.<hash of its body>`: one view, one id,
applied once. The tick's judgement op is `j<epoch>.<seq>.<hash>`; a verb's is
`v<epoch>.<seq>.<hash>`. Every op carries its groups' keys in `Note`, kind `tick`,
actor `sprint` (`steps.Actor`).

**Fences.** Each op carries the conditions its groups' plans built (card and row
gens, hold flags, `NeedsLanded`, `CardAbsent` for new consumers). Layer 6 applies
the ops in order, replaying an unknown outcome by id before the next tick plans.

**Refused ops.** A refused op changed nothing. The next tick rebuilds from the
folded view (never resends the old body) and packs the groups of the refused op
each as an op of its own: split by group, never by halves of writes. A group
refused alone on fresh state is a defect here or in layer 2 and is left out
with an `engine` judgement naming the plan and the store's reason. A machine
refusal while planning is likewise an `engine` judgement, never an op.

## The steps

### Reports

Inbox reports in arrival order, each one group (a work report with the producer's
`Finished`/`Broken`/`Faulted`, built by `card.ReportPlan`; a read report moves
only the read) or one answer. Refusals are answered to the row and consume the
item:

| Cause | Answer |
|---|---|
| unknown consumer | `card C does not exist: run nova-sprint next --as R` |
| not the holder | `consumer C is held by r1, not r2` |
| a session the row no longer has | `report from session S1; row R is now S2; nothing applied` |
| withdrawn | `consumer C was withdrawn: <reason>; branch B is kept` |
| a done consumer with a different result | `consumer C reported <result> at head H already; a report is applied once` |
| an old attempt | `consumer C is of attempt N; producer P is at attempt M` |
| an old head | `read C was cut at H1; producer P is at H2` |

A report matching a done consumer's result and head is acked from the record
(`applied at seq N`) and never applied twice, so layer 4 may redeliver. A report
for a consumer a group of this tick moves waits for the folded result (carried).
Policy has no say: reports are facts. An answer rides with its group and goes out
only when the budget admits the group.

### Preemption

A ready blocker whose stream is free, with open serving rows (up, not held, not
resting, unclaimed, serving its tier and routes, nameable a model) none of which
has free width (every one works at its width; ready consumers hold no width),
gets one group on one such row: `Withdraw` of the youngest working consumer there
(latest `TakenAt`, then id) merged with the blocker's `Start`, plus a `stop` edge
request for that consumer's runner, dispatched only with the op. The withdraw
releases the victim's producer to `ready` with its score and pushed branch kept;
a preempted read's producer stays in review and is asked again. At most one
preemption per row per tick. The row is policy's `rows` answer when it names one,
else the first by name. No serving row at all: the deal opens `unserved` and the
blocker stays ready. A blocker with room anywhere is simply dealt.

### Land: results

For each `land` result in arrival order whose request is in flight:

- `landed`: one group of `Landed{Head, Merge}` for every card of the request still
  `merging` at the head requested, unclaimed, whose head is among `Landed`.
- anything else (`ci_red`, `ci_red_inherited`, `no_ci`, `conflict`, `merge_refused`,
  `pr_unopenable`, `branch_failed`, `ci_fault`, `missing_head`, `head_moved`, `hand`,
  `fault`, ...): one `land` judgement per card still merging at its head, naming
  the card, stream, base and tip, branch, head, outcome, the edge's facts and the
  cards landing together, with choices `land | fix | retry`. Nothing moves and
  nothing is blamed. `ci_red_inherited` (every failing workflow is red on the base
  too and the run ended) is worded so it reads at a glance: land it with
  `nova-sprint land <card> --reason <why>`.

A `stop` ack, or a result for a request no longer in flight (a repeat), is consumed
with no move.

### Sentinels and needs

Every tick, from the view's state (never from land events, so a need landed by a
store op or a restore resolves too):

- **Drops.** For each `fail` producer, the whole closure of its non-final dependants
  fails as one group: each dependant's live consumers are withdrawn, then
  `NeedFailed` (marked `InThisOp` for dependants past the first hop).
- In id order, a `waiting` producer whose needs have all landed and whose stream is
  free is processed: a sentinel gets `Reached` with a `landed-sentinel` judgement
  riding in the op; a producer that names a concrete file a `ready` or started
  producer of its stream names (those resolved earlier this tick counted) opens an
  `overlap` judgement `held behind <card> on <path>` (the least such card by id,
  the first shared file); otherwise it is `Resolve`d.

Room on a row is never a condition for ready: ready producers queue and the deal
fills rows, so ready never flips with row load. Until ready, the view names the
cause (held stream, overlap with X). No policy question: needs are hard.

### Holds

- `Park` a ready producer in a held stream that the hold's op missed.
- `Withdraw` every consumer still `ready` (a leftover, since consumers are cut
  working): the producer is dealt again in hand.
- A **held** row's unacknowledged consumers are withdrawn at once, and a **down**
  row's after `row_silent_gone` (measured from `Down`, else `Since`), releasing
  their producers to be dealt elsewhere. Acknowledged consumers stay: a hold stops
  new deals, not work in hand. A hold never touches landing.

### Retire and fail limits

- `Block` with a `blocked` judgement riding in the op, for a `ready` or `review`
  producer with no live consumer whose `bounces` reach `Limits.Bounces`
  (`bounced N times, the limit is M`). A fault never counts.
- A producer a `missing-route:` fault blocked gets one `blocked` judgement naming
  the row, the card and what is missing, plus the fix (declare `NEEDS-ROUTE`, or
  fix the row that serves the route without having it); it is never dealt again by
  itself until the coordinator answers `unblock` or `drop`.
- Per row (only a row present in `Seen`): `Withdraw` of a `working` consumer when the
  row has been silent for `row_silent_withdraw` (branch kept); `Gone` when it is
  `up` and silent for `row_silent_gone`, never for one missed challenge in a long
  turn; a `flap` judgement when `Flaps >= FlapLimit` (episode = the flap count).
- **Resting.** A row with `RouteRestAfter` or more consumer faults within
  `route_rest_for` is resting until that long after its last fault: it is dropped
  from candidates. It is a deal filter in the tick, never a stored hold, since rows
  are never narrowed to steer deals.

### Fix

For each producer in `review` whose reads of the current review are all final and at
least one is done (`settled`), policy's `verdict` over all of them decides: `yes` is
`ReadYes`; `fix` is `ReadFix{finding}` with every fix read's finding joined (a fix
read that said nothing is named as such); `retry` plans nothing (`owed` deals one
more read). Every yes is planned before every fix. A finding byte-equal to the
producer's current one opens a `brief` judgement: the brief is wrong, not the
worker. A producer back from `Broken` or `Faulted` needs no plan: its score keeps it
at the head of its queue and `Start` carries its prior head and finding.

### Deal and reads

One candidate list down the ladder `blocker, critical, read, high, normal, low`:
blocker work, critical work, every read owed, then high, normal, low work. Within a
rung, policy's `order` tuple then id. `read` is the rung every read consumer
occupies; a producer's level is declared, never computed.

**Work** (`Start{Row, Branch, Base, Model}`), for a ready producer in a free stream:

1. Candidate rows are open, unclaimed, not resting, serve the card's tier, and
   serve every route in the brief's `NEEDS-ROUTE:` lines (`card.NeedsRoutes`,
   `Roster.ServesRoutes`).
2. None: an `unserved` judgement about the tier plus `+<route>` per route
   (`flash+adb`); it clears only when a row serves all of them.
3. Of those, only rows the deal can name a model for: a kin's own model from the
   roster, a bench's from `Answers.Models[tier]`. None: a `no-model` judgement
   about the tier says whether the bench policy or a friend's nova-config model is
   missing, and the card stays.
4. The row is policy's first answered row with room, else the engine's ranking.
   **Room** is the width check: live consumers on the row plus this tick's deals are
   under policy's `width.work`, at most the roster width. A card no row has room for
   stays `ready`, queued.
5. Branch `sprint/<stream>/<producer>` (a stream's group branch is
   `sprint-group/<stream>`, outside `sprint/`, so git never meets a ref that is both
   a file and a directory), base the stream's head from `Inputs.Heads`. A stream
   with no head is an `engine` judgement.

**Reads** (`AskReads{Rows, Models}`): a review owed `reads()` minus the reads of the
current review that are live or done (a withdrawn or faulted read is owed again) is
asked all in one plan, to distinct rows, none the author, none already holding a
live read of this head. No eligible row: a `cannot-read` judgement, so a review
never waits on nothing.

**Escalation, and the engine ranking** (policy absent or its answer unusable): the desired rank exactly
(100), then above it (50), plus 10 for an idle row, minus 1000 for a row the card
was withdrawn from, plus remaining room; ties by name. Desired rank: a work
consumer at the card's own tier; one rank up at `escalate_after` bounces, the top
rank at `frontier_after`; a read at the rank above the writer's row (the top stays
the top). A row's rank is the highest tier it lists. No tier is ever written: a
card's tier is fixed at add and is its minimum, and a row below it is never offered.

Past the budget nothing more is dealt (the judgements above are still opened).

### In hand

The deal is the take: a consumer is cut `working` and is in its row's hand from the
op that cuts it, and the deal's push (layer 6) names the card, repo, base, branch
(a read's head), brief and the finish or read command. The row's `take` is its
**acknowledgement**: a cursor `took:<epoch>:<consumer>` (`AckCursor`; hashed when it
would pass 128 bytes) written once, moving no card. A consumer with no model
counts as acknowledged.

- **Nudge.** A working consumer not yet acknowledged, from `ready_nag` after the
  deal, gets a `NUDGE` line each tick (`Plan.Nudges`, pushed outside the log, no
  cursor moved): the row's counts of untaken and held cards and its width, the card,
  how long ago it was dealt and the `take` command.
- **Ready timeout.** At `ready_take`, a work card still unacknowledged is withdrawn
  and its producer dealt to another row with room (never the slow one), in one group
  (`redeal`). If no other serving row has room, the producer is claimed this tick,
  the base is unknown, or the stream is held, the card stays and one `unacked`
  judgement for the coordinator names the row and the card (`release` deals it
  again).
- **Undealt.** A ready producer still ready `UndealtAfter` after it went ready, with
  an open row serving its tier (and nameable a model) with room, opens one
  `undealt` judgement naming the row, the card and the cause (the tick's budget was
  spent, or an engine defect). Checked after the deal, so a producer dealt this tick
  is skipped; not checked while the gate deals nothing.

### Land: requests

Per stream with no land request in flight: the `merging` producers not under a land
judgement and not claimed, in score order. An `after` orders only producers both
queued, and never holds on a target that is not (an `after` target merging outside
the list holds its dependant; targets inside the list are moved ahead of their
dependants). Fewer than `backed_up` waiting: one request for the first alone
(rung 1); `backed_up` or more: all of them in score order as one request (rung 2).
`ci_wait` is passed as the request's `Wait`. The request id hashes epoch, stream,
base, and each card's `id@head/review`, so a restart reissues the same request and a
changed head gets a new id. A held stream never stops landing. A merging producer
left with no request, no land judgement and no after target holding it is reported as
an `engine` judgement (`answer retry to ask the landing again`); by construction
there is none.

The mechanics (PR, group branch, gate, merge) are layer 4's. There is no bisection and
no blame.

## Groups, packing and refused ops

A **group** is one producer's move with the consumers it touches, composed as one
`card.Merge` of the plans each move builds on the view (a report with its
`Finished`; a preemption with its deal; a drop with its dependants; a redeal's
withdraw with its start). It is the unit the packer orders and never splits across
ops. Its key is every card its plan writes plus the card it is about, with the gens it
read them at; the row it wrote (`Gone`) is claimed like a card. A later step skips a
claimed card until the next tick, so no two groups of a tick fence a record another
writes. A card moves once per tick, so a chain of engine hops takes a tick per hop.

Before planning, the steps check every store rule they can predict, so an op is
refused only by a race (a coordinator verb between snapshot and apply).

## Requests (Verb)

`Verb(view, inputs, Request)` turns one per-card request into one op: the actor check,
the move through the card machine, one op. A verb refused for its actor writes nothing.

```go
type Request struct {
    Actor        string
    Coordinators []string          // the rows nova-config names as coordinators
    Card, Row, Stream string
    Event        card.Event        // the move
    Judgement, Answer string       // an open judgement this request answers, and the choice
    Drop         map[string]card.Event // per dependant of a Cancel/Replace: its chosen Waive or Relink
}
```

Actor rules (an engine fact, since a wrong coordinator could corrupt):

| Events | Who |
|---|---|
| `Block`, `Unblock`, `Waive`, `Relink`, `SetPriority`, `WaiveRed`, `Cancel`, `Replace`, `Edit`, `Rescore`, `Hold`, `Unhold`, `HoldStream`, `UnholdStream`, `Landed`, `Red`, and every judgement answer | a coordinator |
| `Seen`, `Gone` | the run loop (`steps.Actor`) |
| `Report` | the consumer's holder |
| `Add` | anyone hooked in |

Refusal text: `SetPriority is the coordinator's; ask on the bus: nova-bus send --as A --to C ...`.

- **Hold of a card.** `Block` as a request is the coordinator's hold: each live consumer
  is withdrawn, then `Block`, in one op, so the card is never dealt again between.
- **Hold of a stream.** `HoldStream` parks every ready producer of the stream in the
  same op, fenced at the view's seq; `UnholdStream` lifts it.
- **Drop** (`Cancel` or `Replace`): the producer's live consumers are withdrawn as
  released (reason `released: <the drop's reason>`, no result), then the producer's
  move; every dependant, transitively, gets its chosen `Waive` or `Relink` from `Drop`
  or else `NeedFailed`, all `InThisOp`, in the same op.
- **Answering a judgement** rides beside the move outside the card machine's merge:
  fenced `JudgementOpen`, refused when the judgement does not exist (`run nova-sprint
  judge`), is not open (`judgement J was answered by A; nothing to answer` (or `expired`)), or has no
  choice (`judgement J needs a choice: ...`).

## Judgements

A judgement is a stored question owed to a person (layer 1), opened by a step. Its id
is `kind.about.episode`; a cause opens once while one is open; its group key is its
kind (`policy:v<n>` for policy, `roster:<row>` for roster) so related ones are pushed
as one; its command is `nova-sprint judge <id> <choice> --reason <why>`. The deadline
is `timeouts.judgement` except for `blocked`, `land` and `no-policy`, which wait until
answered or cleared. At the deadline a judgement expires with its kind's safe default:
nothing is dropped, accepted, merged or landed. A judgement whose cause has cleared in
the view is expired by the tick.

| Kind | Opened when | Cleared when | Choices |
|---|---|---|---|
| `blocked` | a producer reached the bounce limit or a missing-route fault | it is no longer blocked | unblock, drop |
| `unserved` | a ready producer's tier (plus routes) has no open serving row | a row serves all of it | retry |
| `no-model` | serving rows exist but none can be named a model | one can | retry |
| `cannot-read` | no open row may read an owed review | one may, or no read is owed | retry, drop |
| `land` | a land result other than `landed` | answered | land, fix, retry |
| `overlap` | a waiting producer is held behind another on a file | no longer waiting, or the holder changed | retry |
| `unacked` | a card is unacknowledged past `ready_take` with no other row to take it | acknowledged or no longer in hand | retry |
| `undealt` | a ready producer is not dealt though a row has room | dealt or no longer ready | retry |
| `brief` | a fix read found the same thing twice | | rework, drop |
| `landed-sentinel` | a sentinel was reached (rides in its op) | | retry |
| `flap` | a row's flaps reached the limit | | retry |
| `stall` | nothing landed for `landing_stall` while producers wait in review or merging | the stream lands | retry |
| `idle` | working under `idle_floor` of the open width while producers wait | back above `idle_ceiling` | retry |
| `land-rate` | a stream's first-time land rate (landed with no bounce over landed) is under `land_target` over `land_sample` | back at target | retry |
| `policy` | a policy question failed (one per version and question) | | retry |
| `roster` | a store row is not in nova-config | | retry |
| `no-policy` | no active policy answers (the gate) | the gate lifts | retry |
| `engine` | a plan the card machine or the store refused on fresh state, or a stream with no head | | retry |

Failed work, bounce bounds, lateness, rebase conflicts and a broken read are rule
moves in the tick, not judgements.

## Why the fleet is idle

`steps.Waits(cards)` explains each stream's waiting producers for the idle judgement,
`nova-sprint check` and the board, so the three say one thing. Each waiting producer is
counted once, on the front of the `NEEDS` chain it waits on (its first need not landed,
followed while that need is itself waiting; a need naming no card is `missing`) or,
with every need landed, on the least producer and first file it is held behind (the
needs step's own `leastHolder`). It also finds each stream's longest `NEEDS` chain
over cards not landed or failed (a sentinel adds no length; a cycle a store op wrote
ends). `IdleWhy` is the summary (`N chains (longest K, top: <card> blocks M), H overlaps
(top: <card> holds C on <file>)`) with the command that moves the most: show it when
in flight, judge it when blocked, relink away from it when missing or failed, and
`replace` for a `NEEDS` that is not a consumed output; never a priority, which is set
by request only. `ChainWarn` (4 cards) is the chain length at which add, check and
the board warn. The idle judgement's text is built only when it would open: when no
ready producer is dealable and none resolved this tick it reads `working W of O open
width while R ready and N waiting: nothing dealable: <IdleWhy>; ready: <held> in held
streams, <unserved> with no serving row; <command>`.

## Guarantees

1. No producer is dealt twice: at most one group touches a card in a tick, and every
   `Start` is fenced on the producer's gen.
2. No row is over width: deals are tallied against width minus working, and the card
   machine's guards check the row's roster entry.
3. No landing holds on an unqueued soft `after`.
4. A ready blocker with no room is dealt by preemption in the tick that finds it ready,
   unless no row serves its tier, which is a judgement in the same tick.
5. A refused op never half-applies: an op is one transaction and a group is never split
   across ops, so no preemption lands without its deal and no drop without its
   dependants.
6. A report for a stale head, attempt or session is refused to the row and moves
   nothing; a repeated report is acknowledged and applied once.
7. Every report that arrives is applied or answered: the carry is arrival order and
   reports come before deals.
8. Ready never depends on row load. A ready producer is started when a row serving it has
   room, in rung order, or an `undealt` judgement names it within `UndealtAfter`; a waiting
   producer whose needs landed has a named cause (held stream, or overlap with X).
9. Every non-final producer is waited on by a named actor, the next tick or an open
   judgement (nothing stalls).
10. No policy answer, absence or error makes a step plan an illegal move or plan nothing
    where the default would have planned (the gate is the one exception, and it says so).
11. The tick's cost is the fold of events since the last tick, planning O(G log C + R) for
    G groups (at most the budget), C cards and R rows, and ceil(G / pack) ops. Nothing per
    tick is O(C) after the cold build.

## Testing

| Tier | What |
|---|---|
| Unit (`go test -skip Functional ./steps/...`) | each step's table of view and inputs to groups; every refusal's words; every question absent, failing, slow and lying (`asker_test.go`); the view (a fold per event kind equals a fresh build, `view_test.go`); the purity lint (`lint_test.go`); the paths (happy, fix, red, fault, preemption, drop, hold, block, budget and carry, refused op split by group, unlisted and unhooked rows, silent row and flap, stable op ids); hand (`hand_test.go`); routes; waits; the request table of every verb and actor (`request_test.go`). Time is `Inputs.Now`; no sockets, no network, no store but `store.Fake`. |
| Random walk (`walk_test.go`) | 100 seeds of 2,000 steps on `store.Fake`; the model's state invariants after every step and its tick invariants on every plan |
| Trace walk (`trace_test.go`) | the model's scenarios driven on `store.Fake`, one record per action, projected onto `tla/Steps.tla` (`trace.go` is the refinement mapping) and checked as behaviours by TLC |
| Load (`load_test.go`) | skipped unless `NOVA_LOAD=1`; run on a Linux bench, never in per-commit CI. 5,000 producers in 20 streams over 100 rows of width 4: fold, plan and apply timed apart at steady state, with 200 reports/s and a 2,000-report burst; `TestFunctionalLoadResolve` times the needs step alone |

```
go test -count=1 -skip Functional ./steps/...
NOVA_LOAD=1 go test -count=1 -timeout 60m -run TestFunctionalLoad -v ./steps/   # a bench host
bash tla/ci-tlc.sh                                                    # models, witnesses, reach checks
bash tla/make-traces.sh check                                         # trace fixtures against the model
```

**Measured** at 5,000 producers on a Linux bench, quiet: the fold of a tick's events
about 13 ms p50; planning at a 250-card budget with 2,500 producers ready about 47 ms p50;
the same tick applied on PostgreSQL (fsync on) as 250 ops of one group takes about 457 ms,
as 5 ops of 50 about 203 ms, as one op of 250 about 207 ms. The per-op fixed cost is about
1.1 ms, so packing 50 groups per op takes 98% of the gain of one op while a refusal costs
50 groups, not 250: that is why `Pack` is 50 and `Budget` 250. The full tick at 5,000 producers is
about 265 ms p50, under the one-second p99 bar. The needs step alone at 5,000 waiting
producers is under 10 ms.

**The model.** `tla/Steps.tla` composes `CardMachine.tla` with the tick: variables are the
producers, consumer slots, rows, the hold, `snap` (the view at tick start), `inbox`,
`inflight`, judgements (`js`, `jsnap`), `now`, policy availability, how long each producer
has been ready, and a monitor of the last tick's groups. Actions: `Snap`, `Tick` (groups
from the snapshot by the step rules, under the budget, packed into ops applied in order,
each atomic and fenced against the current state, else refused whole), `Verb` (a
coordinator move between `Snap` and `Tick`), `Arrive`, `EdgeResult`, `Clock`,
`PolicyFails`, `Answer`. Scenarios (cfgs `StepsPre`, `StepsPrePack1`, `StepsDeal`,
`StepsLand`, `StepsBurst`): three producers (one a blocker), two rows of width 1 and 2,
three slots, budget and pack 1 and 2, an inbox of 3.

Invariants include `OneGroupPerCard`, `GroupWhole`, `BudgetKept`, `CarryFIFO`,
`NeverDealtTwice`, `WidthKept`, `NeverBelowTier`, `BlockerPreemptsInOneTick`,
`PreemptYoungest`, `RefusalChangesNothing`, `RefusedOnlyByRace`, `NeverHalfApplied`,
`AfterKept`, `AfterNeverHolds`, `NeverOverlapStarted`, `ReadyNeverFlipsWithLoad`,
`ResolveFromState`, `HeldBehindShown`, `PolicyFailsToDefault`, `NoDealWithoutPolicy`,
`MergingCovered`, `DealIsTake`, `EdgeAfterAccepted`, and every CardMachine invariant.
Liveness (weak fairness on `Tick`, rows and edges): `DealtWithinKTicks` and
`ReadyDealtWhenRoom`, plus nothing stalls. Each rule has a reversed witness in
`tla/witnesses/Steps*.tla` that TLC must fail (two groups on one card; a carry that
drops overflow or runs newest first; a refused op resent blind or split by halves of
writes; an unqueued `after` holding a landing; a step that plans nothing when policy
fails; a stale report applied; resolve without the overlap check; ready conditioned on
room; a consumer cut `ready`; no `undealt` judgement; a glob counted as overlap; a deal
with no policy; and so on), and reach checks in `tla/reach/Steps*.cfg` show the passing
run reaches each action. The inbox is a Go slice, so the sequence is its layout (no
ring, no layout model).

Not bound by the trace: the `undealt` judgement is counted in ticks in the model and in
seconds in the code; nudges and `unacked` are outside the model (it has no clock for
them); the policy gate flag is not logged (the walk's asker always answers); reports
arriving through the inbox are not in the model.

## Source map

| File | |
|---|---|
| `tick.go` | `Tick`, the tick state, the questions |
| `view.go` | the view, `Build`, `Fold`, overlap |
| `inputs.go` | `Inputs`, `InboxItem`, `EdgeRequest`, `Plan`, `GroupKey` |
| `asker.go` | `Asker`, the question types, `Engine` defaults, the oracle that validates answers |
| `group.go`, `pack.go` | groups, ordering, budget, packing, op ids |
| `reports.go`, `preempt.go`, `needs.go`, `holds.go`, `limits.go`, `fix.go` | the steps |
| `deal.go`, `hand.go`, `model.go` | the deal, rows and ranking, acknowledgement and nudges, models and the gate |
| `land.go` | land results and requests |
| `judge.go` | judgements and alarms |
| `request.go` | `Verb` and the actor table |
| `roster.go` | roster lookups |
| `waits.go` | why the fleet is idle |
| `trace.go` | the refinement mapping to `tla/Steps.tla` |
