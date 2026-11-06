# card: layer 2, the card machine

Every legal move of a producer card, a consumer card and a row, as a pure
function from the records a move reads to the plan layer 1 applies, or a
refusal. No store, no clock, no I/O, no globals. The contract is
`docs/LAYER-2-CARD-MACHINE.md`; the model is `tla/CardMachine.tla` (landing
with the model's branch).

## Calling it

```go
plan, r := card.Move(p, consumers, rows, stream, needs, card.Start{Row: "r1", Branch: "b", Base: sha})
if r != nil { /* refused: r.Reason names why */ }
op := store.Op{ID: id, Epoch: snap.Epoch, Actor: who, Kind: "start", At: now, Expect: plan.Expect, Writes: plan.Writes}
_, err := st.Apply(ctx, op)
```

- `Move(p, consumers, rows, stream, needs, e)`: producer and sentinel events.
  `consumers` is every consumer whose `Of` is p; `rows` every row an event may
  name; `needs` every card reachable through needs. Relink's cycle check sees
  only the cards passed, so the caller passes every card reachable through needs.
- `MoveConsumer(c, p, r, e)`: Take, Report, Withdraw; `p` is c's producer, `r` c's holder.
- `MoveRow(r, e)`: Seen, Gone, Width, Hold, Unhold.
- `Merge(plans...)`: several plans as one op. Blocker preemption: the
  machine does not choose the youngest consumer; policy chooses, and the step
  joins that consumer's `Withdraw` and the blocker's `Start` into one op, then
  moves the preempted producer with `Released`.

Consumer ids are the machine's: `<producer>.w<attempt>` for work and
`<producer>.r<review>.<n>` for reads, each fenced with `CardAbsent` at cut. A
producer's `review` counts its entries into review (Finished and Red raise it
by one with `SetCardReview`); a read is cut at the producer's current review
and keeps it, so a read from before a Red or a fix never counts for ReadYes.

Every plan fences each card it reads with `store.CardGen` and each row with
`store.RowGen`; layer 1's conditions carry the rest of what may change between
read and apply. A plan writes only what changes, since layer 1 refuses a
same-value set write.

## Producer table (Move)

| Event | From | Conditions | Writes |
|---|---|---|---|
| Resolve | waiting | StreamHeld false, NeedsLanded | state ready |
| Park | ready | StreamHeld true | state waiting, reason |
| Reached (sentinel) | waiting | NeedsLanded | state landed |
| Start{Row,Branch,Base} | ready | StreamHeld false; RowGen, RowIs up, RowHeld false; CardGen of every consumer; CardAbsent of the new id | state working, attempt+1, author cleared; PutCard `p.wN` (work, of, stream, holder, tier, attempt) + branch, base_sha, prior_head (p's head), reason (p's), state ready |
| Finished{Consumer} | working | CardGen of the work consumer of this attempt, done ok | state review, review+1, head, author = its holder |
| Broken{Consumer} | working | ... done fail | state ready, reason, bounces+1, head |
| Faulted{Consumer} | working | ... done fault | state ready, reason, head (no bounce) |
| AskRead{Row} | review | StreamHeld false; row as Start; no live work consumer; CardAbsent of the new id | PutCard `p.rR.N` (read, review = p's) + head, author (p's), state ready |
| ReadYes | review | no live consumer; a done ok read whose review is p's current review | state merging |
| ReadFix{Reason} | review | no live consumer | state ready, reason, bounces+1 |
| Landed{Head} | merging | head equals p's | state landed |
| Red{Head,Reason} | merging | head equals p's | state review, review+1, reason |
| Released{Reason} | working | the work consumer of this attempt is fail (withdrawn) | state ready, reason (event's, else consumer's), head |
| Cancel{Reason} | any non-final | no live consumer | state fail, reason |
| Replace{By,Reason} | any non-final | no live consumer | state fail, reason "replaced by X: ..." |
| NeedFailed{Need} | any non-final | CardState need fail; no live consumer | state fail, reason "need X failed" |
| Waive{Need,Reason} | waiting | CardState need fail | needs without it, reason |
| Relink{Need,To,Reason} | waiting | CardState need fail; CardGen of To; no cycle | needs with it replaced, reason |
| Edit{Prose,Paths,DoneWhen,Reason} | any non-final | no live consumer | prose, paths, done_when, reason (each when changed); the brief as given is immutable |
| Rescore{Score} | any non-final | | score |
| SetPriority{Level,Reason} | any non-final | | priority ("" is normal), reason |
| Block{Reason} | ready, review, working | no live consumer | state blocked, reason |
| Unblock{Reason} | blocked | | state waiting, reason |

## Consumer table (MoveConsumer)

| Event | From | Conditions | Writes |
|---|---|---|---|
| Take{Row} | ready | row is the holder; RowGen (layer 1 counts width) | state working |
| Report{Row,Result,Head,Reason,Cost,Priced} | working | row is the holder; a read fences its producer's gen | state done, result, head, reason, cost (when changed) |
| Withdraw{Reason} | ready, working | | state fail, reason |

## Row table (MoveRow)

| Event | From | Writes |
|---|---|---|
| Seen | down | state up |
| Gone | up | state down, flaps+1 |
| Width{N} | any | width |
| Hold{Reason} | not held | held true, reason |
| Unhold{Reason} | held | held false, reason |

## Refusals

From-state: `producer p1 is in ready, Resolve needs waiting` (sentinel and
consumer say so); final: `producer p1 is landed; nothing moves a landed card`.
The rest, in order of the code:

- `card X is a work consumer; Move is for producers` / `card X is a producer; MoveConsumer is for consumers`
- `X is not a producer event; Move takes the producer table's events` (and the consumer and row forms)
- `sentinel X is landed by Reached, never resolved`; `producer X is not a sentinel; Reached is for sentinels`; `sentinel X never has a consumer`
- `stream S is not producer X's stream T`; `stream S is held; no new consumers start in a held stream`; `stream S is not held; Park is for a held stream`
- `need N of producer X does not exist`; `need N of producer X is waiting, not landed`
- `row R does not exist`; `row R is down; a consumer is dealt only to a row that is up`; `row R is held; nothing is dealt to a held row`; `row R serves heavy; consumer needs pro`
- `base is empty; it needs a full commit sha`; `base B is not a full commit sha`; `Start of producer X needs a branch`
- `producer X has live consumer C on row R; withdraw it first`
- `producer X's id is too long to name its consumer C`
- `card C is not producer X's work consumer of attempt N`; `consumer C is working, not done`; `consumer C reported fix, Finished needs ok`
- `consumer C is working; Released needs it withdrawn (fail)`
- `R wrote producer X's head and may not read it`
- `producer X has no read of review N that said ok; ReadYes needs one`
- `producer X is at head H, Landed names G` (and Red)
- `Replace of producer X needs the card it is replaced by`
- `N is not a need of producer X`; `need N of producer X is landed, not fail`
- `card T does not exist`; `card T is a landed producer; a need is relinked only to a producer that is not final`; `producer X already needs T`; a cycle: `p1 needs n2 needs p1`
- `producer X is held by R in consumer C; message R instead`; `Edit of producer X changes nothing`
- `score -1 is below 0`; `producer X already has score 5`
- `priority P is not one of low, normal, high, critical or blocker`; `producer X is already at priority normal`
- `consumer C belongs to X, not Y`; `consumer C is held by R, not S`; `row R is not the row the take names (S)`
- `result "maybe" is not ok, fix, fail or fault`; `head H is not a full commit sha`; `head is empty; it needs a full commit sha`; `head H is the base; nothing was done`; `producer X is at head H, the read was of G`
- `row R is up, Seen needs down`; `width -1 is below 0`; `row R already has width 2`; `row R is already held`; `row R is already not held`

## Tests

```
go test -race ./card/
```

`move_test.go` accepts every table row once with its plan asserted exactly,
refuses it from every other state, and checks every refusal above.
`conformance_test.go` drives producers through the happy, fix, red, fault,
release, withdraw, block, park, need, sentinel, preemption and row paths on
`store.Fake`, applying each plan as a real op and checking the rows it leaves.
