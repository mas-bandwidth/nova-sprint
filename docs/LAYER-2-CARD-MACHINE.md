# Layer 2: the card machine

Approved in shape by Glenn 2026-11-06 ("Looks fine. Continue."), then
corrected by him the same hour to the producer and consumer shape of
2026-09-29: "Remember the ideas we had of producer cards, and consumer cards.
And consumer cards are either workers in fleet, or friends. Unifying the two.
This was a good part of the previous design, even when the implementation was
not."

Every legal move of a card, and of a row, lives here and nowhere else. Pure
Go, package `card`: no store, no clock, no bus. Given the rows it touches and
one event, it returns the plan layer 1 applies (conditions and writes from
package `store`) or a refusal. Layer 3 (the steps) builds the event from a
request and the snapshot; layer 5 (policy) decides *when* to raise an event;
this layer says *whether* the move is legal and *what* it writes.

## Two kinds of card, one structure

- **A producer card** is the unit of work in a stream: something between an
  issue and a PR. It has needs, a score (the work order), a brief, and the
  states below. It has no holder: it is the bookkeeping, not the activity.
- **A consumer card** is what a row is handed: the brief plus its place to
  work (repository, base commit, branch, done-when, how to report), cut from
  a producer and dealt to exactly one row. Its kind is `work` or `read`. It is
  the same structure whether the row is a `nova-worker` on a bench or a
  friend on a harness; the row is a name on the bus either way.
- The producer moves on its consumers' results. A work consumer finishing
  moves its producer to review. Whether the done reads of a head mean yes
  (to merging) or fix (back to ready, at the head of its queue) is policy's
  decision from all the reads of that head, never the engine's on the first
  fix it sees (LESSONS 3.7; ruled by Rowan 2026-11-06).
- The holder lives on the consumer only. Two reads of one head are two read
  consumers; there is never a second holder column.

In the store both are rows of `sprint.cards`, told apart by `kind`
(`producer`, `work`, `read`) and `of` (the producer a consumer belongs to;
empty on a producer). Schema version 2 adds `kind`, `of`, `author` (on a
consumer: the row that produced the head it reads; on a producer: the row
whose work the current head is), `base_sha` (on a consumer: the base commit
the attempt started from), and `result` (on a consumer: `ok`, `fix`, `fail`
or `fault` once reported). A consumer's `holder` is its row; a producer's holder is
always none. `attempt` on a producer counts its work consumers cut so far;
a consumer carries the attempt it belongs to. The lessons placed on
2026-11-06 add three more: `prior_head` (on a work consumer: the head this
attempt continues from; LESSONS 3.9, 4.3), `bounces` (on a producer: the
returns that count toward `blocked`; LESSONS 2.24), and `cost` with `priced`
(on a consumer: what its run cost, or that its route has no price; LESSONS
1.17).

## Producer states

The six states Glenn locked on 2026-09-29, plus `fail`:

| state | meaning |
|---|---|
| `waiting` | a need has not landed |
| `ready` | every need landed; no live work consumer |
| `working` | exactly one live work consumer exists |
| `review` | a head is finished; a read consumer is owed or live |
| `merging` | a read said yes to this head; landing in progress |
| `landed` | the head is on the base branch; **final** |
| `fail` | stopped any other way (cancelled, replaced, a need failed); **final**, reason kept |

Invariant across the two kinds: a producer is in `working` exactly when it
has one live work consumer; it has a live read consumer only in `review`.
(Live: a consumer in `ready` or `working`.) Width is a different count: it
caps the consumers a row holds in `working`, never those in `ready`, so ready
work is dealt into a row's queue even when the row is at width (LESSONS 3.3;
PRIORITY D21; ruled by Rowan 2026-11-06). How many `ready` consumers to deal
a row ahead of its width is policy's number.

## Consumer states

| state | meaning | holder |
|---|---|---|
| `ready` | dealt to the row, not yet taken | the row |
| `working` | the row took it | the row |
| `done` | reported (`result` ok, fix, fail or fault; `head` set when one was pushed); **final** | the row |
| `fail` | withdrawn before a report (released, cancelled, its producer moved on); **final**, reason kept | the row |

A consumer never has needs or a score of its own; its order is its
producer's. `done` is a consumer-only state; the store's state set gains it.

## The moves

Each event is one row below. The machine checks the pre-state and the actors
and returns the writes, fenced on the gen of every card it reads (the plan
carries `CardGen` for the producer and for the consumer it touches).

Producer events:

| event | from | to | writes | conditions |
|---|---|---|---|---|
| `Resolve` | waiting | ready | state | `NeedsLanded(p)`; stream not held; a producer only |
| `Park{reason}` | ready | waiting | state, reason naming the hold | p's stream is held; raised in the same op as the stream's hold, so a card that cannot start is never left in `ready` (LESSONS 2.20; PRIORITY D20); `Resolve` moves it on when the hold lifts |
| `Reached` | waiting | landed | state | `NeedsLanded(p)`; a sentinel only: it has no brief and never a consumer; its landing is the signal (a release gate, a phase mark), reported to the coordinator |
| `Start{row, branch, base}` | ready | working | state, attempt+1, author=""; **cuts** a work consumer: kind work, of p, holder row, attempt, branch, base_sha, prior_head=p.head, reason=p.reason, state ready | row up and not held; stream not held; width is not checked here: it is checked at `Take` (LESSONS 3.3) |
| `Finished{c}` (its work consumer reported ok with head H) | working | review | state, head=H, author=c's row | c is p's work consumer of this attempt, done, result ok |
| `Broken{c}` (its work consumer reported fail) | working | ready | state, reason=c's reason, bounces+1, head=c's head when c pushed one | c is p's work consumer of this attempt, done, result fail |
| `Faulted{c}` (its work consumer reported fault) | working | ready | state, reason=c's reason, head=c's head when c pushed one; **bounces unchanged** | c is p's work consumer of this attempt, done, result fault: a harness, environment or bench fault (exit 134 or 137, a run that never started, a staging failure, a provider's rate limit or credit refusal), which spends no attempt toward `blocked` (LESSONS 2.24, 4.14, 4.24) |
| `AskRead{row}` | review | review | **cuts** a read consumer: kind read, of p, holder row, head=p.head, author=p.author, state ready | row up and not held; stream not held; row != p.author; width is checked at `Take` |
| `ReadYes` | review | merging | state | every read consumer of p at p.head is done or fail; no live consumer of p; raised by policy, which reads all of them (LESSONS 3.7) |
| `ReadFix{reason}` | review | ready | state, reason=the findings policy chose, bounces+1 | every read consumer of p at p.head is done or fail; no live consumer of p; raised by policy from all the reads of that head, never automatically on the first fix (LESSONS 3.7); p.head is kept, so the next `Start` continues from it (LESSONS 3.9) |
| `Landed{head}` | merging | landed | state | head equals p.head |
| `Red{head, reason}` | merging | review | state, reason | head equals p.head |
| `Released` | working | ready | state, reason, head=the consumer's last pushed head when there is one; score and bounces unchanged | p's work consumer of this attempt is `fail` (withdrawn, after a silence or preempted by a blocker) |
| `Cancel{reason}` / `Replace{by, reason}` | any non-final | fail | state, reason | no live consumer of p |
| `NeedFailed{need}` | any non-final | fail | state, reason naming the need | need is in p's needs and is `fail`; no live consumer of p |
| `Waive{need, reason}` | waiting | same | needs without `need`, reason | need is in p's needs and is `fail`, or is failed by an earlier write of the same op (the drop that names the waive); a coordinator verb; the actor and time are the op's own, kept in the event (LESSONS 2.18) |
| `Relink{need, to, reason}` | waiting | same | needs with `need` replaced by `to`, reason | as `Waive`; `to` is a non-final producer of the epoch and the new needs make no cycle; recorded as `Waive` is (LESSONS 2.18) |
| `Edit{brief, reason}` | any non-final | same | brief, reason | no live consumer of p: an edit never bounces or rewrites a card in hand, and it keeps p's head, author, attempt, score and finished reads, so a scope-only edit keeps the reviewed head (LESSONS 2.25; PRIORITY D39, W14) |
| `Rescore{score}` | any non-final | same | score | |
| `SetPriority{level, reason}` | any non-final | same | priority | a producer only; level is `low`, `normal` (the default), `high`, `critical` or `blocker`; a verb a person or the coordinator runs on request, never raised by the engine or by policy (Glenn 2026-11-06: "Priorities are manually set by request." "Default is normal."); policy only orders within a level |
| `Block{reason}` | ready, review, or working after its consumer is withdrawn | blocked | state, reason | no live consumer of p; raised by policy when `bounces` passes its limit, with a judgement; a `fault` never counts (LESSONS 2.24) |
| `Unblock{reason}` | blocked | waiting | state, reason | a coordinator verb; Resolve then moves it on |

Consumer events:

| event | from | to | writes | conditions |
|---|---|---|---|---|
| `Take{row}` | ready | working | state | row is the holder; the store's state write refuses it when the row already has as many `working` consumers as its width (LESSONS 3.3) |
| `Report{row, result, head, reason, cost}` | working | done | state, result, head, reason, cost, priced | row is the holder; result ok needs a full 40-hex head; a work consumer's ok head must differ from its base_sha (an empty head is never ok; LESSONS 3.5); a read's head must equal the producer's; the report's id is its op id, so a report delivered twice is one op (LESSONS 4.5) |
| `Withdraw{reason}` | ready or working | fail | state, reason | |

Rules the tables encode, each checked by the model and tested one by one:

- `landed` and `fail` are final for a producer; `done` and `fail` for a
  consumer. No event moves them, `Rescore` and `SetPriority` included.
- A producer's priority is declared, never computed: `low`, `normal`,
  `high`, `critical` or `blocker`, normal by default, changed only by
  `SetPriority` on request. Glenn 2026-11-06: "Critical was a priority
  manually set by me at request." The deal order, lowest to highest, is
  Glenn's (2026-11-06): "There is low, normal, high, read, critical, blocker
  in specific order." `read` is the rung every read consumer occupies: above
  high work, below critical and blocker. Glenn 2026-11-06: "reads are below blocker and critical.
  the idea is that when something is blocker and critical it must be done
  ahead of current work in flight (that resulted in the reads)." Ordering
  inside a level is policy's (LESSONS 5.3).
- A blocker preempts. Glenn 2026-11-06: "blocker is so high, it'll kick out
  the youngest work or read task on a worker to get started right away."
  When a blocker producer is ready and no row that serves it has width,
  policy raises `Withdraw{reason: "preempted by blocker <id>"}` on the
  youngest live consumer (work or read) of a serving row and, in the same
  plan, `Start` deals the blocker to that row. A preempted work consumer's
  producer goes `Released` to ready with its score kept, so it is next after
  the blocker, and its pushed branch kept: the withdrawn consumer's partial
  work stays on its branch and is never deleted (the friends' NEVER lists),
  and the next `Start` continues from it as `prior_head`. A preempted read
  consumer's producer stays in review and is asked again. A preemption never
  raises `bounces`. Which consumer is youngest is policy's choice; the
  withdraw and the deal are the engine's moves (LESSONS 5.12).
- A red CI result never advances a producer: the step retreats
  `merging → review` and is reported (layer 3 raises the notification; this
  layer only moves). A later read may say yes again; that is legal.
- A fix keeps the score, so a fixed producer is at the head of its queue
  without any priority rule (Glenn, 2026-09-29).
- `attempt` rises only on `Start`. A consumer carries its attempt, and a
  producer event accepts only the consumer of its current attempt, so a
  report from an old attempt can never move the producer.
- The reader is never the author: `AskRead` refuses the row that produced
  the head.
- A row going down does **not** withdraw its consumers. `Withdraw` is a
  separate event that policy raises after a silence it has stated (Johnny,
  Emma, 2026-11-06); the engine offers the move and never decides the
  silence.
- A need failing takes its dependants: layer 3 raises `NeedFailed` for every
  producer whose needs name it, in the same tick, after withdrawing their
  consumers. The drop that fails a need may instead name, per dependant, a
  `Waive` or a `Relink` to a replacement, applied in the same op, so the
  dependant is never failed first and revived after (LESSONS 2.18).
- A held stream stops `Start` and `AskRead` (no new consumers); live
  consumers, `Landed` and `Red` continue. Its `ready` producers are parked in
  `waiting` with the hold named, in the same op (LESSONS 2.20).
- `ready` lasts one tick: a ready producer is started on the next tick, or
  parked with the reason it cannot start. The held stream is the parking
  this layer defines; the other reasons a producer cannot start (no row
  serves its tier, no room ahead) are owed a decision (LESSONS 2.20).
- A fix continues from the work already done: `Broken`, `Faulted` and
  `ReadFix` keep the last pushed head and the reason on the producer, and
  `Start` copies them to the new work consumer as `prior_head` and `reason`,
  so the next attempt starts from that head with the finding in hand, never
  from base (LESSONS 3.9, 4.3).
- Only `Broken` and `ReadFix` raise `bounces`. A `fault` (harness,
  environment, bench, staging, a provider's rate limit or credit refusal)
  returns the producer to ready with nothing counted, and `Block` compares
  `bounces`, never `attempt` (LESSONS 2.24, 4.14, 4.24).
- A brief is changed only by `Edit`, and only while the producer has no live
  consumer; the coordinator messages a holder instead of editing under it
  (LESSONS 2.25; PRIORITY D39).
- A soft `after` never holds a landing whose target is not itself queued to
  land (below; LESSONS 2.15).

## The row machine

| event | from | to | writes |
|---|---|---|---|
| `Seen` | down | up | state=up |
| `Gone` | up | down | state=down, flaps+1; raised by policy after a silence it states, never for one missed challenge inside a single long turn, which is "busy or gone, unknown", not down (LESSONS 4.7) |
| `Width{n}` | any | same | width, from nova-config only (LESSONS 1.12); refused below the row's `working` count by layer 1 |
| `Hold{reason}` | any | same | held=true |
| `Unhold` | any | same | held=false |

`limit` is a number policy supplies; the move is the engine's, the number is
not. `blocked` leaves only by `Unblock`, a coordinator verb. A row's state
never moves a card by itself.

## What it refuses, naming the fix

- A move from a final state: "producer X is landed; nothing moves a landed card".
- An event whose from-state does not match: "producer X is in review, Finished needs working".
- A take or report by a row that is not the consumer's holder: "consumer C is held by r1, not r2".
- A head that is not a full 40-hex commit: "head abc is not a full commit sha".
- A read report whose head is not the producer's: "producer X is at head H1, the read was of H2".
- A read asked of the author: "r1 wrote producer X's head and may not read it".
- A start or read in a held stream, to a blocked producer, or to a row that is down or held.
- A producer event naming a consumer that is not its consumer of the current
  attempt and kind, or one that is not done.
- `NeedFailed` naming a card that is not a need, or a need that has not failed.
- `Unblock` of a producer that is not blocked.
- A cycle through `needs` or through `after`, at add or at any later needs,
  after or `Relink` write, naming the loop: "c1 needs c2 needs c1" (LESSONS
  2.19).
- An `Edit` while the producer has a live consumer: "producer X is held by
  r1 in consumer C; message r1 instead" (LESSONS 2.25).
- An event naming a row, card or stream that does not exist: a refusal
  naming the unknown name, never an empty or zero answer; a sha that is not
  on the branch is refused by `finish` before the event is built (CORRECT-USE
  step 8) (LESSONS 2.26).
- An ok report of a work consumer whose head equals its base_sha: "head H is
  the base; nothing was done" (LESSONS 3.5).

A refusal is a value (`Refusal{Reason}`), never an error; the step layer logs
it through layer 1 as a refused op so it is visible.

## How it is checked

- `tla/CardMachine.tla`: one or two producers (c2 needs c1), their
  consumers, two rows, a stream with a held flag; every event above as an
  action. Invariants: final states never move; a producer never has a
  holder; working ⇔ one live work consumer; a live read consumer's holder is
  never the producer's author; attempt rises only on Start; a Red step never
  ends in merging or landed; NeedFailed is enabled whenever a need has failed
  and the dependant is non-final; `working` consumers per row ≤ width
  (`ready` ones are not counted; LESSONS 3.3); fix keeps score; a
  consumer's holder never changes; a consumer of an old attempt never moves
  its producer; a `fault` never raises `bounces` (LESSONS 2.24); an `Edit`
  never lands while a consumer is live (LESSONS 2.25); no producer stays in
  `ready` across a tick in a held stream (LESSONS 2.20); the needs and after
  graphs stay acyclic (LESSONS 2.19). Reversed witnesses, each a required violation: a landed
  producer moving; the author reading their own head; red advancing;
  NeedFailed not enabled after a need failed; a report by a non-holder
  accepted; a producer with two live work consumers; a stale attempt's
  report moving the producer; a fault counted toward `Block`; an edit under
  a live consumer; a `ready` consumer counted against width, which starves a
  row at width; a needs cycle accepted, which stalls both cards.
- Nothing stalls (LESSONS 2.21), a liveness obligation of the same model:
  with the outside actors modelled as acting eventually (weak fairness on
  the rows, the tick and the coordinator), every non-final producer leaves
  its state or is waited on by a named actor, and following the chain of
  waits from any producer reaches an actor, never a loop. Who waits on what:

  | producer state | waits on | bounded by |
  |---|---|---|
  | `waiting` | each need in `needs` (itself not stalled), or the stream's hold | the need's own chain; the hold is the coordinator's, shown as such |
  | `ready` | the next tick's `Start` or `Park` | one tick |
  | `working` | the holder of its work consumer | the silence policy states, then `Withdraw` |
  | `review` | the holders of its live read consumers; else the next tick's `AskRead`; else, when no row may read it, a judgement to the coordinator | the readers' silence; one tick; the judgement's deadline (policy's) |
  | `merging` | the landing step's next pass, or a judgement on its stream (LANDING.md) | the landing-stall bound (LANDING.md) |
  | `blocked` | the coordinator's `Unblock` | none, by Glenn's ruling (2026-11-05: it "sits there until you review it") |

  Reversed witnesses: a review with no askable reader and no judgement; a
  needs cycle; a producer parked with no named hold.
- Unit tests, package `card`: every row of the three tables accepted once and
  refused from every other state; every refusal above with its exact reason;
  no store, no clock, no sockets. A conformance test drives a producer
  through the happy path and the fix, red, withdraw and fail paths by
  applying each plan to `store.Fake`.
- Layer 1 gains `kind`, `of`, `author`, `base_sha`, `result`, the state
  `done`, and schema version 2, with conformance cases; and `prior_head`,
  `bounces`, `cost`, `priced` and the result `fault` (LESSONS 1.17, 2.24,
  3.9).

## Size

About 1,100 lines of Go. An estimate from the event count.

## Decided inside the layer (Rowan, 2026-11-06), easy to overrule

- Consumers live in `sprint.cards` with `kind` and `of`, not in a second
  table: one structure, one set of writes, one holder column, and the
  dashboard reads one table.
- A consumer is cut by the producer's event (`Start`, `AskRead`) in the same
  plan, so a producer is never in `working` without its consumer and a
  consumer never exists without its producer's state saying so.
- The producer's `author` is set from the work consumer's row at `Finished`
  and cleared at `Start`: an attempt has one author, and a release before a
  finish leaves none.
- The bounce limit that blocks a producer is policy's number, compared with `bounces` (LESSONS 2.24); the move is the engine's. A row is never blocked: on the record (10-25, 11-05) the flaps were our hook's, not the row's; a row's flaps stay a counted fact for policy to raise a judgement on.
- The runner is its own binary, `nova-worker`, in this repo (Glenn: "your
  call"); it and `nova-sprint` share the consumer verbs (take, report) from
  one package. The name `nova-swarm` is reserved by Glenn for a future mode
  where the whole sprint works one hard problem; this is workers across
  lanes, not a swarm.

## Hard needs, soft order, sentinels

Glenn, 2026-11-06: "Cards had dependencies on other cards, we had sentinel
cards (very useful). And you even had soft ordering for dependencies, eg.
land order. Those were hard earned."

- `needs` is hard: `Resolve` waits until every need has landed.
- `after` is soft: work starts whenever the card is ready, and it orders
  landing only among cards that are both queued to land. When producer B
  names A in `after` and A is itself queued to land (in `merging`, or ahead
  of B in the same batch), A lands first; when A is not queued to land, the
  `after` is ignored for this landing: B lands, and A lands when it is
  queued (PRIORITY M20). A soft
  `after` never holds a landing whose target is not queued: in v1 a
  two-day-old LAND-AFTER froze four streams' landings until that rule was
  made (2026-10-16; LESSONS 2.15; PRIORITY D29, M20; ruled by Rowan
  2026-11-06).
- A sentinel is a producer of kind `sentinel`: no brief, no consumer, no
  score of its own. When its needs have landed, `Reached` lands it, and that
  landing is reported. A release gate or a phase mark is a sentinel with the
  cards it waits for as its needs.
- A need failing takes a sentinel with it like any dependant.

## Rules added from the promise reviews (2026-11-06)

- `ReadYes` requires at least one read consumer of the current head that is
  `done` with result ok and was cut after the producer last entered `review`;
  reads withdrawn or from an earlier head count for nothing, so a yes never
  fires on zero yes (Fable, promise 4). After a `Red`, a waived red re-enables
  the same reads only when the coordinator's waive says so; otherwise the fix
  brings a new head and new reads.
- A consumer that reports `fault` (the harness, the bench or the route
  failed, not the work) spends no attempt and raises no bounce; policy rests
  the route it ran on before re-dealing, so a fault never loops (Fable,
  promise 2; LESSONS 4.24).
- `AskRead` may be raised for every read a head needs in one tick; how many
  reads a head gets and whether they are asked together is policy's (LESSONS 3.4).
- A judgement is a stored record in layer 1 (`sprint.judgements`), opened by
  a named layer 3 step, pushed from the record, answered by a verb; nothing
  owed lives only in a message (all six reviews).

## Added after layer 2 was built (2026-11-06)

Building layer 2 on the locked store found one gap in the store and four
details, each ruled here (the lock allows a column that a layer above needs,
with a design change, never a special case):

- `cards.review integer NOT NULL DEFAULT 0`: on a producer, the number of
  times it has entered `review`; on a read consumer, the producer's `review`
  at the moment it was cut (fixed at add). Write: `SetCardReview` (producer
  only, raised by one). `ReadYes` requires a done ok read whose `review`
  equals the producer's current `review`, so a read from before a `Red` or
  a fix never counts and a yes never fires on zero yes.
- `rows.reason text NOT NULL DEFAULT ''`: the reason a row was held or
  released; `SetRowHeld` takes it.
- `Edit` changes prose, paths and done_when; the brief as given is immutable.
- `SetPriority{level, reason}` writes the reason too.
- Consumer ids are the machine's: `<producer>.w<attempt>` for work,
  `<producer>.r<review>.<n>` for reads, fenced with `CardAbsent` at cut.
- `Relink`'s cycle check sees the cards the caller passes; the caller passes
  every card reachable through needs.
- Blocker preemption: the machine does not choose the youngest consumer;
  policy chooses, and the step joins the `Withdraw` and the blocker's `Start`
  into one op.

## Rulings from the first cold read of layer 2 (2026-11-06)

- A work consumer reports ok, fail or fault; a read consumer reports ok, fix
  or fault; anything else is refused naming the kind.
- In `working` the machine knows its work consumer's id (`<producer>.w<attempt>`)
  and requires it in the consumers passed; a missing one is a refusal, and it
  is fenced in every plan that depends on "no live consumer".
- `Block` from `working` requires the current attempt's consumer to be
  withdrawn (`fail`); a consumer that has reported must be applied first
  (`Finished` or `Broken`), never discarded.
- `Replace{by}` requires `by` to be an existing card other than the producer.
- `SetPriority` applies to producers and sentinels (a release gate may be a
  blocker); the earlier "a producer only" is corrected.
- The reader's finding has its own column: `cards.finding text` (producer: the
  last read's finding; work consumer: a copy at cut, so a fix attempt carries
  it), written by `ReadFix`; `reason` stays the status reason. Layer 1 round 14.
- `Withdraw{reason, head}` carries the consumer's last pushed head when there
  is one, written to the consumer's `head`; `Released` copies it to the
  producer as the next attempt's `prior_head`. And a `Withdraw` of the work
  consumer in `working` folds `Released` into the same plan, so the producer
  is never in `working` with no live consumer between two ops.
- `NeedFailed{need, inThisOp}`: when the step fails the need in the same op,
  the plan carries no "need is fail" condition (layer 1 checks conditions
  before writes), so a waive and its drop can be one op.
- A fix attempt's ok report with head equal to `prior_head` is refused
  ("nothing new was pushed"), as is head equal to `base_sha`.
- `ReadYes` requires the counted read to belong to this producer and to carry
  the producer's current head, as well as its review.
- `AskRead` refuses a row that already holds a live read of this head, and
  refuses a sentinel.
- The machine applies layer 1's value rules (ranges, names, NUL) as refusals
  before building a plan, so no plan it emits is `ErrInvalid`.
- An `Add` event builds the put-card plan for a producer or sentinel and
  refuses a cycle through needs or after, and a need that is not a card.
- Wording: `attempt` counts the work consumers cut; `bounces` is what the
  block limit reads; a fault raises neither.

## Rulings from the card model (2026-11-06)

- The invariant is as the tables have it: at most one live work consumer per
  producer, belonging to the current attempt; a producer in `working` whose
  consumer has reported or been withdrawn is one plan away from leaving
  `working` (Finished, Broken, Faulted, Released, Block), and that plan is
  built in the same tick.
- A move that returns a producer to `ready` while its stream is held
  (Broken, Faulted, ReadFix, Released) writes it parked in the same plan.
- Preemption withdraws a `working` consumer only (a ready one holds no width),
  and "no serving row has width" counts open rows: up and not held.
- `ReadFix` requires at least one done fix read of the current review, as
  `ReadYes` requires a yes.
- `NeedFailedTakesDependants` is a rule, not a side effect: a dependant of a
  failed need gets no new consumer and is failed by layer 3 in the same tick.
- Row table text: there is no row block; the flap limit sentence is gone.
- The liveness obligation "nothing stalls" is layer 3's model, with the tick.

## Rulings from the second cold read of layer 2 (2026-11-06)

- `AskReads{rows}` cuts several reads from one snapshot with distinct
  indices; `Merge` validates the merged plan (the same checks as a single
  plan, plus no two writes to one field), so no plan the machine emits is
  ever invalid at the store.
- `HoldStream` refuses a producer listed twice; its ready set is fenced with
  the snapshot's `AtSeq`.
- `Relink{InThisOp}` as `Waive`, so a drop and its relink are one op.
- `Broken` and `Faulted` write the consumer's reason into the producer's
  `finding`; `Start` copies `finding` to the new work consumer, so a fix
  attempt always carries what it fixes.
- Every return into a held stream, and `Unblock`, parks with a reason that
  names the hold ("held: <the stream's reason>").
- `Add` caps a card id at 104 bytes so every consumer suffix fits in 128.
- A sentinel may be a need (a phase mark is what later phases need).
- `Replace{by}`: `by` is a producer or sentinel, non-final, fenced.
- `Withdraw` of a working producer's live consumer always releases the
  producer in the same plan (a withdraw that leaves it working is refused);
  `Withdraw{head}` equal to `base_sha` is refused (nothing was pushed).
- `UnholdStream{reason}` is an event; `WaiveRed{reason}` moves a producer
  that a Red sent back to `review` on to `merging` without a new read when a
  done ok read of the previous review at the same head exists (the
  coordinator's judgement that the red was not the code's).
- Actor rulings (who may Unblock, Waive, Relink, SetPriority, WaiveRed) are
  layer 3's; events carry no actor.

## Rulings from the third cold read of layer 2 and the card model (2026-11-06)

- `Merge` is sequential composition, never a union of writes: plan 1's
  writes are applied to a copy of the snapshot, plan 2's move is re-run
  against that state, and so on; any refusal refuses the whole merge naming
  both plans. This is what makes "the same checks as a single plan" literally
  true (two relinks cannot close a cycle; a hold merged with a resolve cannot
  leave a ready producer in a held stream).
- A Red records itself on the producer (`red = true`, the column of store round 17); `Finished` and
  `Start` clear it; `WaiveRed` refuses "no red on review N" without it.
- A fault never writes `finding`. A fault's reason goes to `reason`;
  `finding` is written only by a read (`Broken` carries the reader's text,
  `Faulted` does not) and `Start` copies it to the next attempt. This
  replaces the second read's "Broken and Faulted write the consumer's reason
  into finding".
- `Relink` accepts a sentinel as the new need, as `Add` does.
- A `Withdraw` whose reason is preempt refuses unless the consumer is
  working; choosing the youngest on the blocker's row is the step's (layer 3
  has the list).
- `HoldStream` parks exactly the ready set the caller lists; passing the full
  ready set of the snapshot is layer 3's duty, and the `AtSeq` fence makes a
  stale list refuse rather than fill it in.
- `Landed` has no `after` check in the machine; "a landing never holds on a
  soft after whose target is not queued" is the landing step's rule (layer 3).
- A hold merged after a start parks what the start readied; a start merged
  after a hold is refused "held". Both fall out of sequential `Merge`.
- The model (`tla/CardMachine.tla`, 93 runs at 279a8835) matches the code at
  1b6e66ab; `prior_head` is model state (Report and Withdraw refuse a head
  equal to it); `reason`, `brief`, `cost`, `priced`, `prose`, `paths` and
  `done_when` are data no transition reads. Its holes are listed in
  `tla/README.md`; the ones above are the rulings on them.
