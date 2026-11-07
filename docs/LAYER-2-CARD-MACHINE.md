# Layer 2: the card machine

Package `card`. Every legal move of a card, a row and a stream lives here and
nowhere else. It is pure Go: no store, no clock, no bus, no I/O, no globals.
Given the records a move reads and one event, it returns the *plan* layer 1
applies (conditions and writes from package `store`) or a *refusal*. Layer 3
(the steps) builds the event from a request and the snapshot, layer 5 (policy)
decides *when* to raise an event, and this layer says *whether* the move is
legal and *what* it writes.

```go
plan, r := card.Move(p, consumers, rows, stream, needs, card.Start{Row: "r1", Branch: "b", Base: sha})
if r != nil { /* refused: r.Reason names the fix */ }
op := store.Op{ID: id, Epoch: snap.Epoch, Actor: who, Kind: "start", At: now, Expect: plan.Expect, Writes: plan.Writes}
rec, err := st.Apply(ctx, op) // a refused op is rec.Accepted == false, err == nil
```

A refusal is a value (`*Refusal{Reason}`), never an error. It never reaches the
store as a write; layer 3 logs it by applying it as a refused op, so it is
visible beside the ops layer 1 refuses. Events carry no actor: who may
`Unblock`, `Waive`, `Relink`, `SetPriority` or `WaiveRed` is layer 3's.

- [Two kinds of card](#two-kinds-of-card)
- [Entry points](#entry-points)
- [States](#states)
- [Producer events](#producer-events)
- [Consumer events](#consumer-events)
- [Stream, row and add events](#stream-row-and-add-events)
- [Rules the tables encode](#rules-the-tables-encode)
- [Plans and Merge](#plans-and-merge)
- [Refusals](#refusals)
- [Hard needs, soft order, sentinels](#hard-needs-soft-order-sentinels)
- [Testing](#testing)

## Two kinds of card

Both are rows of `sprint.cards` (layer 1), told apart by `kind` and `of`.

- **Producer card** (`kind` `producer`): the unit of work in a stream, something
  between an issue and a PR. It has needs, a score (the work order), a brief and
  a state. It never has a holder: it is bookkeeping, not activity.
- **Sentinel** (`kind` `sentinel`): a producer with no brief, no score and no
  consumers. It lands when its needs have landed (a release gate or a phase
  mark); the landing is the signal.
- **Consumer card** (`kind` `work` or `read`): what a row is handed. It carries
  the brief plus its place to work (repository, base commit, branch,
  done-when), is cut from a producer and dealt to exactly one row (its
  `holder`, fixed at the cut). The structure is the same whether the row is a
  bench worker or a kin on a harness.

The producer moves on its consumers' results. A work consumer finishing moves
its producer to review. Whether the done reads of a head mean yes (to merging)
or fix (back to ready) is policy's decision from all the reads of that head,
never the engine's on the first fix it sees. The holder lives on the consumer
only; two reads of one head are two read consumers.

`card.Card`, `card.Row` and `card.Stream` are the machine's view of store
records (`FromStore`, `RowFromStore(r, width, tiers)`, `StreamFromStore`). A
row's width and tiers come from the roster, not the store.

## Entry points

| Function | Moves |
|---|---|
| `Move(p, consumers, rows, stream, needs, e)` | a producer or sentinel `p` on a producer event |
| `MoveConsumer(c, p, r, e)` | a consumer `c` of producer `p`, held by row `r`: `Report`, `Withdraw` |
| `MoveRow(r, e)` | a row: `Seen`, `Gone`, `Hold`, `Unhold` |
| `MoveStream(s, e)` | a stream: `HoldStream`, `UnholdStream` |
| `MoveAdd(e, cards)` | a new producer or sentinel (`Add`) |
| `Merge(plans...)` | several plans as one op |
| `ReportPlan(c, p, consumers, rows, s, needs, report)` | a row's report as one plan: the consumer's `Report`, and for a work consumer the producer's `Finished`/`Broken`/`Faulted` merged in |

Arguments to `Move`:

- `consumers`: every card whose `Of` is `p`. The list must be complete: a
  `working` producer whose work consumer of this attempt (`<p>.w<attempt>`) is
  not passed is refused, since a missing live consumer would let through a plan
  layer 1 has no condition to stop. A refusal that depends on a live consumer
  reads every consumer passed in any order and names the refused one with the
  lowest id.
- `rows`: every row an event may name, with roster width and tiers.
- `needs`: every card the event reads: `p`'s needs, for `Relink` every card
  reachable through the target's needs and after, for `Replace` the replacement.

Every plan fences each card it reads with `CardGen`, each row with `RowGen`, and
adds the conditions the tables name. A plan writes only what changes (layer 1
refuses a same-value set write). Before a plan leaves the machine it is checked
against layer 1's value rules (below), so no plan it emits is `ErrInvalid`.

## States

Producer states:

| State | Meaning |
|---|---|
| `waiting` | a need has not landed, or the stream is held |
| `ready` | every need landed; no live consumer; to be started or parked on the next tick |
| `working` | exactly one live work consumer of the current attempt exists |
| `review` | a head is finished; a read consumer is owed or live |
| `merging` | reads said yes to this head; landing in progress |
| `landed` | the head is on the base branch; **final** |
| `fail` | stopped any other way (cancelled, replaced, a need failed); **final**, reason kept |
| `blocked` | parked for the coordinator; only `Unblock` leaves it |

Consumer states:

| State | Meaning |
|---|---|
| `working` | in the row's hand from the deal (a consumer is cut `working`; none is ever `ready`; the deal is the take) |
| `done` | reported (`result` `ok`, `fix`, `fail` or `fault`; `head` set when one was pushed); **final** |
| `fail` | withdrawn before a report (released, cancelled, preempted, its producer moved on); **final**, reason kept |

A consumer has no needs and no score of its own; its order is its producer's.
(`ready` is a legal state for the store and for `Withdraw`, but this machine
never cuts a consumer in it.)

*Live* means `ready` or `working`. Invariant across the kinds: a producer is in
`working` exactly when it has one live work consumer, of its current attempt; it
has a live read consumer only in `review`. A producer in `working` whose consumer
has reported or been withdrawn is one plan from leaving `working` (`Finished`,
`Broken`, `Faulted`, `Released`, `Block`), and that plan is built in the same
tick.

Width caps the consumers a row holds in `working`, never those in `ready`. The
machine sees one producer's consumers and cannot count a row, so the width check
is the dealer's (layer 3, from the roster) and the model guards the cut with
`RoomOn`.

## Producer events

Each event is one row. The machine checks the pre-state and returns the writes.
Every plan starts with `CardGen` of the producer; "fences" below lists the other
conditions.

| Event | From | To | Writes | Guards and fences |
|---|---|---|---|---|
| `Resolve` | waiting | ready | state | producer only (not a sentinel); stream free (`StreamHeld false`); `NeedsLanded` |
| `Park{Reason}` | ready | waiting | state, reason (default `stream S is held`) | stream held (`StreamHeld true`); raised in the same op as the stream's hold so a card that cannot start is never left `ready` |
| `Reached` | waiting | landed | state | sentinel only; `NeedsLanded` |
| `Start{Row, Branch, Base, Model}` | ready | working | state, `attempt`+1, `author`="" if set, red cleared; **cuts** a work consumer | producer only; stream free; `Base` a full sha; `Branch` non-empty; row exists, `up`, not held, serves the producer's tier (fences `RowGen`, `RowIs up`, `RowHeld false`); no live consumer |
| `Finished{Consumer}` | working | review | state, head=consumer's, author=consumer's row, `review`+1, red cleared | the consumer is `<p>.w<attempt>`, done, result `ok` |
| `Broken{Consumer}` | working | ready (waiting if stream held) | state, reason, `bounces`+1, head if the consumer pushed one | consumer done, result `fail` |
| `Faulted{Consumer}` | working | ready (waiting if held), or **blocked** | as `Broken` but **bounces unchanged**; reason starting `missing-route:` goes to `blocked` and is never re-dealt | consumer done, result `fault` |
| `AskRead{Row, Model}` | review | review | **cuts** a read consumer (kind read, head=p.head, author=p.author, `review`=p.review) | producer only; stream free; p has a full-sha head; no live work consumer; row is not `p.author`; row as `Start`; row holds no live read of this head |
| `AskReads{Rows, Models}` | review | review | one read per row, distinct indices | as `AskRead`; at least one row; each row once; `Models` nil or one per row |
| `ReadYes` | review | merging | state | no live consumer; at least one read cut at p's current review and head is done `ok` |
| `ReadFix{Finding}` | review | ready (waiting if held) | state, `finding`, `bounces`+1 | no live consumer; at least one read of the current review/head is done `fix`; finding non-empty; p.head is kept |
| `WaiveRed{Reason}` | ready or waiting | merging | state, reason | no live consumer; p has a review; `red` is recorded (fences `CardRed true`); a done `ok` read of the previous review at this head exists |
| `Landed{Head, Merge, Reason}` | merging | landed | state, reason `landed at <merge>[: <reason>]` | head equals p.head; with both Merge and Reason empty the reason is unchanged |
| `Red{Head, Reason}` | merging | ready (waiting if held) | state, `review`+1, `finding`=reason, reason, `red`=true; **bounces unchanged** | head equals p.head; reason non-empty (the failing checks and their URLs) |
| `Released{Reason}` | working | ready (waiting if held) | state, reason, head=consumer's | the work consumer of this attempt is `fail` |
| `Cancel{Reason}` | any non-final | fail | state, reason | no live consumer |
| `Replace{By, Reason}` | any non-final | fail | state, reason `replaced by <by>: <reason>` | no live consumer; `By` is another non-final producer or sentinel among the cards passed (fenced by its gen) |
| `NeedFailed{Need, InThisOp}` | any non-final | fail | state, reason `need <n> failed` | need is in p's needs and is `fail` (fences `CardState`); no live consumer |
| `Waive{Need, Reason, InThisOp}` | waiting | waiting | needs without `Need`, reason | need is in p's needs and failed |
| `Relink{Need, To, Reason, InThisOp}` | waiting | waiting | needs with `Need` replaced by `To`, reason | as `Waive`; `To` is a non-final producer or sentinel not already a need; the new needs make no cycle through needs or after |
| `Edit{Prose, Paths, DoneWhen, Reason}` | any non-final | same | prose, paths, done_when, reason (only what changes; nil/"" leaves a field) | producer only; no live consumer; must change something |
| `Rescore{Score}` | any non-final | same | score | producer only; `Score` >= 0 and different |
| `SetPriority{Level, Reason}` | any non-final | same | priority, reason | producer or sentinel; `Level` in `low normal high critical blocker` (`""` is normal) and different from current |
| `Block{Reason}` | waiting, ready, review or working | blocked | state, reason | producer only; no live consumer (from `working` the attempt's consumer must be withdrawn: a consumer that reported must be applied first with `Finished`/`Broken`/`Faulted`) |
| `Unblock{Reason}` | blocked | waiting | state, reason (the hold's phrase when the stream is held) | the producer's stream is the one passed |

Notes on the table:

- **`Start`.** The stream check comes before the state check, so a producer that
  was parked and then started in one op is refused `held`. Width is not checked.
  The cut consumer `<p>.w<attempt>` carries `branch`, `base_sha`, `prior_head`
  (= p.head when set), the producer's finding and copies of its brief, prose,
  paths, done_when, stream, tier and the deal's `Model`. It is cut `working`.
  `Start` copies no reason.
- **`AskRead`.** The read `<p>.r<review>.<n>` is cut at the producer's current
  `review` with `head` = p.head. Several reads of one head may be live together
  (different rows); a row never holds two live reads of one head.
- **A move back to ready** (`Broken`, `Faulted`, `ReadFix`, `Red`, `Released`,
  withdraw-release) fences the stream's held flag and writes `waiting` instead
  when the stream is held, with the reason `held: stream S is held`. No producer
  is ever `ready` in a held stream.
- **`Faulted`** is a harness, environment, bench or route failure (exit 134 or
  137, a run that never started, a staging failure, a provider's rate limit or
  credit refusal). It spends no attempt toward `blocked` and writes no `finding`;
  the reason goes to `reason`. A fault whose reason starts `missing-route:`
  (`card.MissingRoute`; the first word after it is the route's name,
  `card.RouteName`) blocks the producer on the first fault, because the same kind
  of row would fault the same way; layer 3 opens the blocked judgement and the
  coordinator answers `unblock` or `drop`.
- **`Block`** is raised by policy when `bounces` passes its limit (with a
  judgement), and by the coordinator's `hold <card>` with reason `held: <why>`
  (`card.HeldPrefix`, live consumers withdrawn in the same op); `unhold` is
  `Unblock`. A `blocked` card is not final.
- **`Red`** is a red landing: it goes to fix, never to a terminal state. The
  `review` rises so reads before the red never count; no bounce is spent because
  a red is not a reader's or a worker's verdict. `WaiveRed` is the coordinator's
  judgement that the red was not the code's; it needs the red still recorded
  (`Start` and `Finished` clear it) and an earlier ok read of this head.
- **`NeedFailed` / `Waive` / `Relink` `InThisOp`**: when the step fails the need
  earlier in the same op, set `InThisOp` so the plan carries a `CardState fail`
  fence that `Merge` drops when an earlier move of the op wrote the need to fail,
  and refuses otherwise (`merge it with the drop`). Applied alone the fence stays
  and the store refuses it. A need that is `landed` is refused. This makes
  `Waive` and the drop that names it one op, so a dependant is never failed first
  and revived after.
- **`Edit`** never bounces or rewrites a card in hand, and keeps the head,
  author, attempt, score and finished reads, so a scope-only edit keeps the
  reviewed head. While a waiting card is parked in a held stream its reason is
  the hold's: an `Edit` that only changes the reason is refused. The brief as
  given is immutable.
- **`Landed`**'s `Merge` is the merge commit the base has (or a hand landing's
  base tip) and `Reason` a hand landing's why.

## Consumer events

| Event | From | To | Writes | Guards |
|---|---|---|---|---|
| `Report{Row, Result, Head, Reason, Cost, Priced, Usage}` | working | done | state, `result`, head, reason, `cost`+`priced`, `usage` (each only if changed) | `Row` is the holder; result one of `ok fix fail fault`; a **work** consumer reports `ok`, `fail` or `fault`, a **read** `ok`, `fix` or `fault`; `ok` needs a full 40-hex head; a work consumer's head must differ from its `base_sha` and its `prior_head`; a read's head must equal the head it was cut at, which must equal the producer's (fences the producer's gen); a head, when given, is a full sha |
| `Withdraw{Reason, Head, Stream, Preempt}` | ready or working | fail | state, reason, head | see below |

`Withdraw` rules:

- `Head` is the consumer's last pushed head; it is checked like an ok report's:
  a full sha, not the base, not the prior head; a ready work consumer has pushed
  nothing; a read is withdrawn without a head of its own.
- Withdrawing the work consumer of a `working` producer's current attempt also
  releases the producer in the same plan (to `ready`, or `waiting` when the
  stream is held, with the reason and the head as the next attempt's
  `prior_head`). `Stream` is the producer's stream, read with it, and is required
  for the release. A withdraw of a live work consumer whose producer is not
  `working` is refused.
- `Preempt: true` marks a blocker's preemption: refused unless the consumer is
  `working`.

There is no `Take`: the deal is the take. The row's `take` verb is an
acknowledgement kept as a cursor in layer 3 and moves no card. A report
delivered twice is one op (the report's id is its op id).

## Stream, row and add events

**Stream.** `HoldStream{Reason, Ready, Seq}` holds the stream and parks, in the
same op, every producer in `Ready` (each once, each `ready` in this stream),
fenced by `StreamHeld false`, `AtSeq{Seq}` of the snapshot the ready set was read
from, and each producer's gen. A card that became ready after that snapshot
refuses the op rather than being filled in. The caller passes every ready
producer of the stream; one it misses is parked later by `Park`. `UnholdStream`
clears the hold and parks nothing (`Resolve` moves cards on). Holding a held
stream or unholding a free one is refused. A held stream stops `Start` and
`AskRead`; live consumers, `Landed` and `Red` continue.

**Row.**

| Event | From | Writes |
|---|---|---|
| `Seen` | down | state=up |
| `Gone` | up | state=down, flaps+1; raised by policy after a silence it states, never for one missed challenge inside a long turn |
| `Hold{Reason}` | not held | held=true, reason |
| `Unhold{Reason}` | held | held=false, reason |

All fence `RowGen`. A row going down does **not** withdraw its consumers:
`Withdraw` is a separate event that policy raises after a silence it has stated.
A row's state never moves a card by itself. A row's width and tiers are
roster configuration; the machine reads them but never writes them.

**Add.** `MoveAdd(Add{ID, Kind, Stream, Tier, Priority, Score, Needs, After, Brief,
Prose, Paths, DoneWhen}, cards)` builds the put-card plan for a producer or
sentinel, fenced `CardAbsent`. `cards` is every card its needs and after reach.
Refused: a kind other than producer or sentinel; an existing id; an id containing
`.` (it would collide with consumer ids); an id over 104 bytes (so the longest
consumer suffix fits in 128); a sentinel with a brief or a score; a priority
outside the levels; a need that is itself, missing, repeated, or not a producer
or sentinel; an after that is itself, missing, repeated or a consumer; a cycle
through needs or after, naming the loop (`c1 needs c2 needs c1`); a card reached
by the walk that was not passed.

## Rules the tables encode

Each is checked by the model and tested.

- **Final is final.** `landed`/`fail` for a producer and `done`/`fail` for a
  consumer: no event moves them, `Rescore` and `SetPriority` included.
- **Attempts.** `attempt` rises only on `Start`. A consumer carries its attempt,
  and a producer event accepts only the consumer of its current attempt and kind,
  so a report from an old attempt can never move the producer. At most one live
  work consumer exists per producer.
- **The reader is never the author.** `AskRead` refuses the row that produced the
  head; a reader's read counts only against the review and head it was cut at.
- **Priority is declared, never computed.** `low`, `normal` (default), `high`,
  `critical`, `blocker`; changed only by `SetPriority`, which a person or the
  coordinator runs on request. Read consumers rank between `high` and `critical`
  in the deal order; ordering inside a level is policy's.
- **A blocker preempts.** When a blocker producer is ready and no serving row has
  width, policy chooses the youngest live consumer on a serving row and the step
  merges `Withdraw{Preempt: true}` of it with the blocker's `Start`. The preempted
  work consumer's producer is released to `ready` with its score kept (next after
  the blocker) and its pushed branch kept: the withdrawn work stays on its branch
  and the next `Start` continues from it as `prior_head`. A preempted read's
  producer stays in `review` and is asked again. A preemption never raises
  `bounces`. A hand-built pair that withdraws a consumer on another row than the
  blocker's, or a non-working one, is refused by `Merge`.
- **A fix continues from the work done.** `Broken`, `Faulted` and `ReadFix` keep
  the last pushed head on the producer; `Start` copies it as `prior_head` and the
  producer's `finding` to the new work consumer, so the next attempt starts from
  that head with the finding in hand, never from base. A fix keeps the score, so
  a fixed producer is at the head of its queue.
- **Only `Broken` and `ReadFix` raise `bounces`.** `Block` compares `bounces`,
  never `attempt`. The bounce limit is policy's number; the move is the engine's.
- **`finding` is written by a read**: `ReadFix` and `Red`. `Broken` and `Faulted`
  write `reason` only.
- **A need failing takes its dependants.** Layer 3 raises `NeedFailed` for every
  producer whose needs name it, in the same tick, after withdrawing their
  consumers. The coordinator's drop of a producer under a live consumer
  withdraws it as released in the same op, then cancels (the machine still
  refuses `Cancel` under a live consumer).
- **A brief is changed only by `Edit` (prose, paths, done_when), and only while the
  producer has no live consumer.** The coordinator messages a holder instead of
  editing under it.
- **`ready` lasts one tick**: a ready producer is started on the next tick or
  parked with the reason it cannot start. Held streams are the parking this layer
  defines.
- **A soft `after` never holds a landing whose target is not itself queued to
  land.** The machine's `Landed` has no `after` check; that rule is the landing
  step's (layer 3).

## Plans and Merge

A `Plan` is `Expect []store.Expect` and `Writes []store.Write`, applied as one
op. `Merge(plans...)` is sequential composition, never a union of writes: every
move the machine built is planned again against the records read before any write
of the op, with each earlier move's writes folded in, and the last write of a
field is the one emitted. This is what makes "the same checks as a single plan"
literally true: two relinks from one snapshot cannot close a cycle; a hold merged
after a resolve parks what the resolve readied; a start merged after a hold is
refused `held`; a hold merged after a start parks what the start readied.

Details:

- A merged plan's read set is the pre-write read, so a nested merge composes like
  the flat list. Nested merges flatten first.
- A condition on a field an earlier plan of the op writes is dropped (the store
  checks every condition before any write); the other conditions are each plan's,
  taken on the snapshot it was read on.
- A write that would change nothing is not emitted. A later state write that
  would erase a move into `working` is refused: that write is the width check.
- A hand-built plan is folded onto those records, or refused when it writes a
  card no plan read.
- Two plans that cut the same new card (two `AskRead`s of one index, two `Start`s
  of one producer, two `Add`s of one id) are refused with the fix named: several
  reads of one head are `AskReads`; one `Start`; one `Add`.
- No two writes to one field survive (`a second write to card c1 field state; one
  op writes a field once`).
- A refusal names the plans and the cause: `merge of Resolve p1 and Hold s1
  refused: ...`.
- The `Withdraw{Preempt}` / blocker `Start` pair is checked on the flattened
  moves: the starter's priority (including a `SetPriority` earlier in the op) is
  `blocker`, and the preempted consumer is `working` on the row `Start` names. A
  plain `Withdraw` merges with a `Start` without that check. A row with no open
  width is layer 3's.

## Refusals

Every refusal names the fix. The set, with exact wording where it is quoted:

- **Final state**: `producer X is landed; nothing moves a landed card`.
- **Wrong from-state**: `producer X is in review, Finished needs working`.
- **Wrong actor**: `consumer C is held by r1, not r2`.
- **Wrong card**: a producer event naming a consumer that is not its work
  consumer of the current attempt (`card X is not producer P's work consumer of
  attempt N`), or one that is not done (`consumer C is working, not done;
  Finished waits for its report, or withdraw it`), or reported another result
  (`consumer C reported fail, Finished needs ok`).
- **Heads**: `head abc is not a full commit sha`; `head H is the base; nothing was
  done`; `head H is the prior head; nothing new was pushed`; `read C was cut at H1,
  the report names H2`; `read C was cut at H1; producer P is at H2`.
- **Readers**: `r1 wrote producer X's head and may not read it`; `row r1 already
  holds live read C of head H`; `producer P has a live work consumer C; a read is
  cut only after it reports`.
- **Rows**: `row r does not exist`; `row r is down; a consumer is dealt only to a
  row that is up`; `row r is held; nothing is dealt to a held row`; `row r serves
  flash,pro; consumer needs heavy`.
- **Streams**: `stream S is held; no new consumers start in a held stream`;
  `stream S is not held; Park is for a held stream`; a stream other than the
  producer's.
- **Live consumers**: `producer X has live consumer C on row r; withdraw it first`;
  `producer X is held by r1 in consumer C; message r1 instead` (an `Edit` under a
  live consumer); `producer X is working; its work consumer X.w2 was not passed
  (the consumer list must be complete)`.
- **Reads**: `producer X has no read of review N that said ok; ReadYes needs one`;
  `... said fix; ReadFix needs one`; `a fix read says what to fix`.
- **Needs**: `need n of producer P does not exist` / `is waiting, not landed` /
  `is ... not fail`; `n is not a need of producer P`; `NeedFailed` naming a card
  that is not a need; `card c1 ... already needs c2`; a cycle, naming the loop; a
  walk that reaches a card that was not passed.
- **Block / Unblock**: `sentinel X is never dealt; there is nothing to block`;
  `consumer C reported ok; apply it with Finished before Block`; `Unblock` of a
  producer that is not `blocked`.
- **Unknown names**: a row, card or stream that does not exist is refused naming
  the unknown name, never an empty or zero answer. A sha that is not on the branch
  is refused by `finish` (layer 3) before the event is built.
- **Values**: see below.

**Values.** The machine applies layer 1's value rules as refusals, so no plan is
`ErrInvalid`: every key is non-empty and at most 128 bytes; every text is valid
UTF-8 with no NUL; every count is in 0..2147483647; a cost is at or above 0; a
tier is one of the store's tiers. Messages: `a card needs a name`, `card "..." is
over 128 bytes`, `reason "..." is not valid UTF-8 or holds a NUL byte`, `score
2147483648 is outside the range 0..2147483647`, `cost -1 is below 0`, `tier "x" is
not one of flash, pro, heavy, frontier`, `seq -1 is below 0`.

## Hard needs, soft order, sentinels

- `needs` is hard: `Resolve` waits until every need has landed.
- `after` is soft: work starts whenever the card is ready, and `after` orders
  landing only among cards that are both queued to land. When B names A in `after`
  and A is itself queued to land (in `merging`, or ahead of B in the same batch),
  A lands first; when A is not queued, the `after` is ignored for this landing: B
  lands, and A lands when it is queued. A soft `after` never holds a landing whose
  target is not queued.
- A sentinel has no brief, no consumer and no score. When its needs have landed,
  `Reached` lands it and the landing is reported. A release gate or a phase mark
  is a sentinel with the cards it waits for as its needs. A sentinel may be a
  need; a need failing takes a sentinel with it like any dependant.
- Both lists are acyclic: a cycle through needs or after is refused at add and at
  any later needs, after or `Relink` write.

## Testing

Unit tests in package `card` use no store, clock or sockets:

| Test | Covers |
|---|---|
| `move_test.go` | every table row accepted once with its plan asserted exactly, refused from every other state, and every refusal above with its exact reason |
| `conformance_test.go` | drives producers through the happy, fix, red, fault, release, withdraw, block, park, need, sentinel, preemption, row, stale-relink, stream-hold, drop and add paths on `store.Fake`, applying each plan as a real op and requiring it accepted |
| `trace_test.go`, `trace_tla_test.go` | the code's random walk is projected onto the model (`TraceState`) and checked as a behaviour of `tla/CardMachine.tla` |
| `model_test.go` | the deal's model: `Start` and `AskReads` carry the model the deal names onto the consumer |
| `report_test.go` | `ReportPlan` |

```
go test -count=1 -skip Functional ./card/...   # unit tier
go test -count=1 -run Functional ./card/...    # merged plans compared against PostgreSQL
bash tla/ci-tlc.sh                      # the model, its witnesses and reach checks (a bench host)
```

**Model.** `tla/CardMachine.tla` has one or two producers (c2 needs c1), their
consumers, two rows and a stream with a held flag; every event above is an
action. Scenarios are the cfgs `CardMachine.cfg`, `CardDeps.cfg`,
`CardPreempt.cfg`, `CardReads.cfg`, `CardMerge.cfg`, `CardMergePre.cfg`,
`CardMergeDeps.cfg`. Invariants include: final states never move; a producer
never has a holder; working iff one live work consumer; a live read's holder is
never the producer's author; attempt rises only on `Start`; a red step never ends
in merging or landed; `NeedFailed` is enabled whenever a need has failed and the
dependant is non-final; `working` consumers per row stay within width and no
consumer is ever `ready` (`DealIsTake`); fix keeps score; a consumer's holder
never changes; a consumer of an old attempt never moves its producer; a fault
spends nothing and a route fault blocks; an `Edit` or `Block` never lands under a
live consumer; no producer stays `ready` across a tick in a held stream; the needs
and after graphs stay acyclic; a read yes needs a yes and a fix needs a fix; the
fences are on the snapshot. Each rule has a reversed witness (`tla/witnesses/
Card*.tla`, a variant TLC must fail: a landed producer moving, the author reading
their own head, red advancing, a non-holder's report accepted, two live work
consumers, a stale attempt's report, a fault counted toward `Block`, an edit under
a live consumer, a needs cycle accepted, a consumer cut `ready`, a deal over
width, a route fault re-dealt, and so on) and reach checks in `tla/reach/Card*`
show the passing run reaches each move.

**Nothing stalls.** A liveness obligation of the layer 3 model: with the outside
actors modelled as acting eventually, every non-final producer leaves its state or
is waited on by a named actor, and the chain of waits reaches an actor, never a
loop:

| Producer state | Waits on | Bounded by |
|---|---|---|
| `waiting` | each need (itself not stalled), or the stream's hold | the need's own chain; the hold is the coordinator's |
| `ready` | the next tick's `Start` or `Park` | one tick |
| `working` | the holder of its work consumer | the silence policy states, then `Withdraw` |
| `review` | the holders of its live reads; else the next tick's `AskRead`; else, when no row may read it, a judgement to the coordinator | the readers' silence; one tick; the judgement's deadline |
| `merging` | the landing step's next pass, or a judgement on its stream | the landing-stall bound |
| `blocked` | the coordinator's `Unblock` | none: it waits until a person reviews it |

**Trace validation.** `card/trace.go` (`TraceState`) is the refinement mapping
from store records to the model's variables: consumers become slots `<<p, i>>`
by cut order, heads map to small integers, a non-empty finding is `fix`, a
missing-route fault is the result `route`. Branch, base_sha, reason, brief,
prose, paths, done_when, cost, priced, pr, tier, gen and the row's tiers are not
projected because no model rule reads them.
