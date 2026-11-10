# card: layer 2, the card machine

Every legal move of a producer card, a consumer card and a row, as a pure
function from the records a move reads to the plan layer 1 applies, or a
refusal. No store, no clock, no I/O, no globals. The contract is
`docs/LAYER-2-CARD-MACHINE.md`; the model is `tla/CardMachine.tla`, checked
with TLC (`tla/README.md`), and the tables below are its actions.

## Calling it

```go
plan, r := card.Move(p, consumers, rows, stream, needs, card.Start{Row: "r1", Branch: "b", Base: sha})
if r != nil { /* refused: r.Reason names why */ }
op := store.Op{ID: id, Epoch: snap.Epoch, Actor: who, Kind: "start", At: now, Expect: plan.Expect, Writes: plan.Writes}
rec, err := st.Apply(ctx, op) // a refused op is rec.Accepted false, err nil
```

`card.FromStore`, `card.RowFromStore` and `card.StreamFromStore` turn the
store's records into the machine's.

- `Move(p, consumers, rows, stream, needs, e)`: producer and sentinel events.
  `consumers` is every consumer whose `Of` is p, and the list must be
  complete: a working producer whose work consumer of this attempt is not
  passed is refused, since a missing live consumer would let a plan through
  that layer 1 has no condition to stop. `rows` is every row an event may
  name; `needs` every card reachable through needs and after. Relink's cycle
  walk follows needs and after and fences every card it reads with CardGen.
  A walk that reaches a card not passed is refused. The path is joined with
  " needs " either way. Two relinks from one snapshot close no loop: Merge
  runs the second against the first's writes, and the second's walk sees them.
- `MoveConsumer(c, p, cons, r, e)`: Report, Withdraw (no Take: the deal is the take, defect 52); `p` is c's producer, `cons` its consumers, `r`
  c's holder. `Withdraw.Stream` is p's stream, needed when the withdraw
  releases p.
- `MoveRow(r, e)`: Seen, Gone, Width, Hold, Unhold.
- `MoveStream(s, e)`: HoldStream{Reason, Ready, Seq} holds the stream and
  parks, in the same op, every ready producer named once (a duplicate is
  refused), fenced by StreamHeld false, UnchangedSince (no stream or card
  written) the seq of the snapshot the ready set was read from, and each
  producer's gen. A card that became ready after that snapshot is not in
  the plan, and any card or stream write since refuses the whole op; a
  worker's beat, which writes only its row, does not.
  UnholdStream{Reason} clears the hold and parks nothing. The caller passes
  every ready producer of the stream; one it misses is parked later with Park.
  Merged after another plan, HoldStream scans the stream again, so a producer
  an earlier plan readied in the same op is parked too.
- `MoveAdd(e, cards)`: a new producer or sentinel; `cards` is every card its
  needs and after reach. A card id is at most 104 bytes, so a consumer suffix
  still fits in 128. A need is a producer or a sentinel.
- `Merge(plans...) (Plan, *Refusal)`: sequential composition of moves. Nested
  merges flatten to one ordered list, and every move the machine built, the
  first included, is planned again against the records read before any write
  of this op, with each earlier move's writes folded in. The merged plan's
  seen is that pre-write read, so a nested merge composes like the flat list.
  The last write of a field wins, state included: the store refuses two
  writes of one field, so a path cannot be replayed inside the op.
  Merge(Resolve, HoldStream) parks the producer the resolve readied. A later
  write that would erase a move into working is refused (that write is the
  store's width check). A write that changes nothing is not emitted.
  A hand-built plan is folded onto those records, or refused when it writes a
  card no plan read. Conditions are each plan's, taken on the snapshot it was
  read on, minus a condition on a field an earlier plan of the op writes (the
  store checks every condition before any write). A refusal names the plans
  and the condition. Two AskReads of one head still collide. Two Starts of one
  producer, or two Adds of one id, name that fix.

Events carry no actor. Who may HoldCard, UnholdCard, Waive, Relink, SetPriority or WaiveRed
is layer 3's; the machine does not.

A machine refusal never reaches the store as a write. Layer 3 logs it by
applying it as a refused op, so the store's log holds it beside the ops
layer 1 refuses.

### Ops made of several plans

- **Withdraw and release are one plan.** Withdrawing the work consumer of a
  working producer's current attempt also moves the producer back (ready, or
  waiting when the stream is held), with the reason and the head for the next
  attempt's prior head (`Withdraw.Head`, else the consumer's). `Released` is
  kept for recovery of a store where a withdrawn consumer's producer is still
  working; the normal path never needs it.
- **Preemption.** The step chooses the consumer, the youngest on the blocker's
  row; the machine does not. `Withdraw{Preempt: true}` refuses unless that
  consumer is working. The reason text is not that signal. Merged with a
  blocker's `Start`, the check runs on the flattened moves: the starter's
  priority is `blocker`, and the withdrawn consumer is working on the row
  `Start` names. A withdraw without `Preempt` merges with that Start with no
  such check. A row with no open width is layer 3's. The step merges that
  consumer's `Withdraw` (which releases its producer) and the blocker's
  `Start`.
- **A drop.** When a need fails in the same op that moves its dependants,
  each dependant's `NeedFailed`, `Waive` or `Relink` is planned with
  `InThisOp: true` and merged with the drop. The plan fences the need as
  fail. Merge drops that fence when an earlier move of the op wrote the
  need to fail, and refuses `InThisOp` otherwise (`merge it with the drop`),
  including a merge of that plan alone. Applied alone, the fence is still
  there and the store refuses it. A need that is landed is refused. A lone
  Move still returns the plan. Relink's `InThisOp` defaults false.

### The invariant, as the tables keep it

A producer has at most one live work consumer, and it is of the producer's
current attempt: Start is refused under any live consumer and cuts `p.w<n>`
at attempt n. A consumer that has reported (done) or been withdrawn (fail) is
one plan away from the producer leaving working: Finished, Broken or Faulted
applies a report, and a withdraw of the current work consumer moves the
producer in its own plan.

### What a cut consumer carries

A cut consumer carries its producer's brief, prose, paths and done_when
(copies, in slices of its own), stream and tier. A work consumer carries the
producer's finding (the store requires it to equal the producer's); a read
carries the producer's review. Start copies no reason.

A refusal that depends on a live consumer reads every consumer passed, in
any order, and names the refused one with the lowest id; AskRead allows live
reads but never a live work consumer, nor a second live read on one row of
one head.

Consumer ids are the machine's: `<producer>.w<attempt>` for work and
`<producer>.r<review>.<n>` for reads, each fenced with `CardAbsent` at cut. A
producer's `review` counts its entries into review (Finished and Red raise it
by one with `SetCardReview`); a read is cut at the producer's current review
and keeps it, so a read from before a Red or a fix never counts for ReadYes
or ReadFix.

Every plan fences each card it reads with `store.CardGen` and each row with
`store.RowGen`; layer 1's conditions carry the rest of what may change between
read and apply. A plan writes only what changes, since layer 1 refuses a
same-value set write.

A move back to ready (Broken, Faulted, ReadFix, Released, and the withdraw
release) fences the stream's held flag and writes waiting instead when the
stream is held, so no producer is ready in a held stream. The producer's
reason is then `held: stream S is held`. A stream has no reason column, so
the phrase names the stream. Finding is written only by a read (ReadFix) and
carried by Start onto the next work consumer. Broken and Faulted write the
reason and do not touch finding. Start copies finding and copies no reason.
UnholdCard of a producer whose stream is held leaves it waiting with the
same phrase. HoldStream's own park keeps the event's reason. A Red is recorded on
the producer's red column (`SetCardRed`). Finished and Start clear it.
WaiveRed requires it and fences the plan on `CardRed`. Broken, Faulted and
WaiveRed do not clear it. pr stays the pull-request number; this layer does
not write it.

### Values

Before a plan leaves the machine it is checked against layer 1's value
rules, so no plan it emits is `ErrInvalid`: every key is non-empty and at
most 128 bytes, every text valid UTF-8 with no NUL, every count in
0..2147483647, a cost at or above 0, a tier one of the store's tiers. The
refusals: `a card needs a name`, `card "..." is over 128 bytes`,
`reason "..." is not valid UTF-8 or holds a NUL byte`,
`score 2147483648 is outside the range 0..2147483647`, `cost -1 is below 0`,
`tier "x" is not one of ...`, `gen -1 is outside the range 0..2147483647`,
`seq -1 is below 0`. Add's own cap is `id "..." is N bytes; Add caps a card id at 104 so its consumer ids fit in 128`.

## Producer table (Move)

A card with a problem has no state of its own (Glenn, 2026-11-07: "waiting
is where stuff goes where it's having problems"). A producer that has a problem waits, its
reason saying what: the coordinator's hold (`held: <why>`, `HeldPrefix`,
HoldCard), policy's hold at the bounce limit (the same), or a missing-route
fault (`missing-route: <route> <what>`, `MissingRoute`). `HeldReason` is the
one test of such a reason and `Held` of a waiting producer that has one; a
park into a held stream (`held: stream S is held`) is the stream's hold,
never the card's. Resolve and WaiveRed refuse a held producer, so the deal
never takes it; Waive, Relink, Edit and SetPriority keep its reason (a
reason-only Edit is refused), and no event but HoldCard or a route fault
sets a reason that holds. UnholdCard lifts it to plain waiting.

| Event | From | Conditions | Writes |
|---|---|---|---|
| Resolve | waiting, not held (`Held`) | StreamHeld false, NeedsLanded | state ready |
| Park | ready | StreamHeld true; Reason does not hold | state waiting, reason |
| Reached (sentinel) | waiting | NeedsLanded | state landed |
| Start{Row,Branch,Base} | ready | producer only; StreamHeld false; RowGen, RowIs up, RowHeld false; CardGen of every consumer; CardAbsent of the new id; width is the dealer's (layer 3), not checked here | state working, attempt+1, author cleared, red cleared when a Red set it; PutCard `p.wN` (work, of, stream, holder, tier, attempt, task, finding) + branch, base_sha, prior_head (p's head), state working (the deal is the take) |
| Finished{Consumer} | working | CardGen of the work consumer of this attempt, done ok | state review, review+1, head, author = its holder, red cleared when a Red set it |
| Broken{Consumer} | working | ... done fail; StreamHeld as read | state ready (waiting if held), reason (the hold phrase when held), bounces+1, head |
| Faulted{Consumer} | working | ... done fault; StreamHeld as read | state ready (waiting if held), reason (the hold phrase when held), head (no bounce); state waiting with the fault's reason when it starts `missing-route:` (`MissingRoute`, defect 48), which holds it (`Held`) |
| AskRead{Row} | review | producer only; a full sha head; StreamHeld false; row as Start; no live work consumer, no live read of this head on the row; CardAbsent of the new id | PutCard `p.rR.N` (read, review = p's, task) + head, author (p's), state working |
| AskReads{Rows} | review | as AskRead, one snapshot, each row once | one PutCard per row, indices distinct (`p.rR.1`, `p.rR.2`, ...) |
| ReadYes | review | no live consumer; a done ok read of p's current review and head | state merging |
| ReadFix{Finding} | review | no live consumer; a done fix read of p's current review and head; StreamHeld as read | state ready (waiting if held), finding (when given and changed), bounces+1 |
| Landed{Head} | merging | head equals p's | state landed |
| Red{Head,Reason} | merging | head equals p's | state review, review+1, reason, red = true |
| WaiveRed{Reason} | review | no live consumer; red is set (CardRed true); review at least 1; a done ok read of review p.Review-1 at p's head | state merging, reason; review is not raised |
| Released{Reason} | working | the work consumer of this attempt is fail (withdrawn); StreamHeld as read | state ready (waiting if held), reason (the hold phrase when held, else the event's, else the consumer's), head |
| Cancel{Reason} | any non-final | no live consumer | state fail, reason |
| Replace{By,Reason} | any non-final | By a producer or sentinel, not final, not p, passed in needs, fenced by CardGen; no live consumer | state fail, reason "replaced by X: ..." |
| NeedFailed{Need,InThisOp} | any non-final | CardState need fail (none with InThisOp); no live consumer | state fail, reason "need X failed" |
| Waive{Need,Reason,InThisOp} | waiting | CardState need fail (none with InThisOp) | needs without it, reason |
| Relink{Need,To,Reason,InThisOp} | waiting | CardState need fail (none with InThisOp); To a non-final producer or sentinel; the cycle walk follows needs and after; CardGen of every card it read, To first; no cycle | needs with it replaced, reason |
| Edit{Prose,Paths,DoneWhen,Reason} | any non-final | producer only; no live consumer | prose, paths, done_when, reason (each when changed); the brief as given is immutable |
| Rescore{Score} | any non-final | producer only | score |
| SetPriority{Level,Reason} | any non-final | (a sentinel too) | priority ("" is normal), reason |
| HoldCard{Reason} | waiting, ready, review, working | a producer, never a sentinel, not already held; no live consumer; a reported one is applied first; Reason starts `held: ` (`HeldPrefix`) | state waiting (no write from waiting), reason: the coordinator's `hold <card>`, or policy's at the bounce limit |
| UnholdCard{Reason} | waiting, held (`Held`) | StreamHeld as read; Reason does not hold | reason (`unheld` when empty; the hold phrase when the stream is held): plain waiting again; bounce_base = bounces when they differ (a fresh bounce budget) |

Relink on a sentinel is allowed, as is SetPriority; Edit, Rescore, Start and
AskRead on a sentinel are refused.

## Consumer table (MoveConsumer)

| Event | From | Conditions | Writes |
|---|---|---|---|
| Report{Row,Result,Head,Reason,Cost,Priced} | working | row is the holder; work reports ok, fail or fault, a read ok, fix or fault; a non-empty work head is a full sha, neither its base nor its prior head, for every result; a read reports the head it was cut at, which is the producer's, and fences the producer's gen; an empty read head stays empty | state done, result, head (a read's only when it is the head it was cut at), reason, cost (when changed) |
| Withdraw{Reason,Head,Stream} | ready, working | a non-empty work head is a full sha, not the base, not the prior; a ready work consumer has not pushed; a read keeps its own head; a preempt refuses unless the consumer is working; a withdraw that would leave a working producer working is refused, and says to pass the producer in working so the withdraw releases it; releasing: Stream is p's, CardGen p, StreamHeld as read | state fail, reason, head; when it is the current work consumer of a working producer, also the producer: ready (waiting if held), reason (the hold phrase when held), head |

`ReportPlan` is a work consumer's `Report` and the producer's `Finished`,
`Broken` or `Faulted` as one `Merge`, fenced on both gens. A read's
`ReportPlan` moves only the consumer.

## Row table (MoveRow)

| Event | From | Writes |
|---|---|---|
| Seen | down | state up |
| Gone | up | state down, flaps+1 |
| Width{N} | any | width |
| Hold{Reason} | not held | held true, reason |
| Unhold{Reason} | held | held false, reason |

## Add (MoveAdd)

| Event | Conditions | Writes |
|---|---|---|
| Add{ID,Kind,Stream,Tier,Priority,Score,Needs,After,Brief,Prose,Paths,DoneWhen} | kind producer or sentinel; id at most 104 bytes; CardAbsent of the id; a sentinel has no brief and no score; each need a producer or a sentinel, each after a producer or sentinel, none named twice, none the card itself; no cycle through needs and after | PutCard (state waiting) |

No card but the new one is fenced: layer 1 refuses a need or after naming a
card that does not exist, so no stored card can name the new id before it
lands, and a loop can close only through the new card's own lists.

## Refusals

From-state: `producer p1 is in ready, Resolve needs waiting` (sentinel and
consumer say so); final: `producer p1 is landed; nothing moves a landed card`.
The rest, in order of the code:

- `card X is a work consumer; Move is for producers` / `card X is a producer; MoveConsumer is for consumers`
- `X is not a producer event; Move takes the producer table's events` (and the consumer, row and stream forms)
- `sentinel X is landed by Reached, never resolved`; `producer X is not a sentinel; Reached is for sentinels`; `sentinel X never has a consumer`
- `stream S is not producer X's stream T`; `stream S is held; no new consumers start in a held stream`; `stream S is not held; Park is for a held stream`
- `need N of producer X does not exist`; `need N of producer X is waiting, not landed`
- `row R does not exist`; `row R is down; a consumer is dealt only to a row that is up`; `row R is held; nothing is dealt to a held row`; `row R serves heavy; consumer needs pro`
- `base is empty; it needs a full commit sha`; `base B is not a full commit sha`; `Start of producer X needs a branch`
- `producer X is working; its work consumer C was not passed (the consumer list must be complete)`
- `producer X has live consumer C on row R; withdraw it first`
- `producer X has a live work consumer C; a read is cut only after it reports`
- `row R already holds live read C of head H`
- `producer X's id is too long to name its consumer C`
- `card C is not producer X's work consumer of attempt N`; `consumer C is working, not done; Finished waits for its report, or withdraw it`; `consumer C reported fix, Finished needs ok`
- `producer X is at review N, the most there is`
- `consumer C is working; Released needs it withdrawn (fail)`
- `R wrote producer X's head and may not read it`
- `producer X has no read of review N that said ok; ReadYes needs one`; `producer X has no read of review N that said fix; ReadFix needs one`; `producer X has no review yet`; `no red on review N`; `producer X has no done ok read of review N at head H; WaiveRed needs one`
- `producer X is at head H, Landed names G` (and Red)
- `Replace of producer X needs the card it is replaced by`; `producer X cannot be replaced by itself`; `card B does not exist (Replace takes the card it is replaced by among the cards passed)`; `card B is a work consumer; Replace needs a producer or a sentinel that is not final`; `producer B is landed; Replace needs a producer or a sentinel that is not final`
- `N is not a need of producer X`; `need N of producer X does not exist`; `need N of producer X is landed, not fail`; `need N of producer X is landed; it cannot fail in this op`
- `card T does not exist`; `card T is a work consumer; a need is relinked only to a producer or a sentinel that is not final`; `producer T is landed; a need is relinked only to a producer or a sentinel that is not final`; `producer X already needs T`; `card B, needed by R, was not passed; Relink needs every card reachable through needs and after`; a cycle, including one through after: `p3 needs p2 needs p3`
- `sentinel X has no brief or prose to edit`; `producer X is held by R in consumer C; message R instead`; `Edit of producer X changes nothing`
- `sentinel X has no score of its own`; `score -1 is below 0`; `producer X already has score 5`
- `priority P is not one of low, normal, high, critical or blocker`; `producer X is already at priority normal`
- `consumer C reported ok; apply it with Finished|Broken|Faulted before HoldCard`
- `producer X is held (R); the deal never takes a held card: unhold it first`; `producer X is already held (R)`; `producer X is not held; UnholdCard lifts a hold`; `a hold's reason starts "held: " and names why: "R"`; `reason "R" would hold producer X; hold it with HoldCard`; `reason "R" would hold producer X again; an unhold names why it is lifted`; `producer X is held (R); unhold it before WaiveRed`
- `consumer C belongs to X, not Y`; `consumer C is held by R, not S`; `MoveConsumer was passed row S for a take on R; pass the consumer's holder row`; `row R is down; it cannot take`
- `result "maybe" is not ok, fix, fail or fault`; `a work consumer reports ok, fail or fault; fix is a read's result`; `a read consumer reports ok, fix or fault; fail is a work's result`; `head H is not a full commit sha`; `head is empty; it needs a full commit sha`; `head H is the base; nothing was done`; `head H is the prior head; nothing new was pushed`; `read C was cut at H1, the report names H2`; `read C was cut at H1; producer X is at H2`
- `read C is of head H; a read is withdrawn without a head of its own`; `consumer C is ready; it has not pushed a head`; `consumer C is ready; a preempt withdraws a working consumer, and the step chooses the youngest on the row`; `producer X is ready; withdrawing live work consumer C would leave it working; pass the producer in working, so the withdraw releases it`; `Withdraw of C releases producer X; it needs X's stream S, not ""`
- `stream S is already held`; `stream S is already not held`; `producer X is listed twice in HoldStream; name each ready producer once`; `card C is a work consumer; HoldStream parks producers`; `producer X is in stream S, not T`; `producer X is in working, HoldStream parks ready producers`; `row R is named twice in AskReads of producer X; name each row once`
- `row R is up, Seen needs down`; `width -1 is below 0`; `row R already has width 2`; `row R is already held`; `row R is already not held`
- Add: `Add makes a producer or sentinel, not a work; consumers are cut by Start and AskRead`; `id "..." is N bytes; Add caps a card id at 104 so its consumer ids fit in 128`; `card X exists`; `sentinel X has no brief`; `sentinel X has no score of its own`; `need N of producer X is not a card` / `is named twice` / `is a work consumer; a need is a producer or a sentinel`; `X needs X`; `X comes after X`; `after A of producer X is a work consumer; a card comes after a producer or sentinel`; `card N, reached from M, was not passed; Add needs every card reachable through needs and after`; a cycle: `p1 needs n1 needs p1`
- The value rules above.

## Tests

```
go test -race ./card/
```

`move_test.go` accepts every table row once with its plan asserted exactly,
refuses it from every other state, and checks every refusal above.
`conformance_test.go` drives producers through the happy, fix, red, fault,
release, withdraw, hold, park, need, sentinel, preemption, row, stale
relink, task copy, work-fix, stream-hold, return-into-held-stream, drop and
add paths on `store.Fake`, applying each plan as a real op, requiring the
receipt accepted, and checking every write type it lands. `coldread_test.go`
is the first cold read of layer 2 (defects D1-D11 and suspects S1-S3, each
asserting its ruling) and its random walk of accepted moves on the Fake.
Probes from Stella's and Johnny's reads of a822dfc7 are folded in, named in
their tests. `coldread2_test.go` is the second cold read, folded with the
rulings (AskReads, the seq fence, Relink's InThisOp, the 104-byte cap, and
the suspects). `emma_read2_test.go` is Emma's four from that read, folded
the same way. `coldread3_test.go` is the third cold read, folded with these
rulings: Merge is sequential, WaiveRed needs a recorded Red, a fault does not
write finding, a sentinel may be a need, and a preempt withdraws a working
consumer. `round4_test.go` covers Start then Hold, Hold then Start, and the
red column.
