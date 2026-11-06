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
  moves its producer to review; a read consumer saying yes moves it to
  merging, saying fix sends it back to ready at the head of its queue.
- The holder lives on the consumer only. Two reads of one head are two read
  consumers; there is never a second holder column.

In the store both are rows of `sprint.cards`, told apart by `kind`
(`producer`, `work`, `read`) and `of` (the producer a consumer belongs to;
empty on a producer). Schema version 2 adds `kind`, `of`, `author` (on a
consumer: the row that produced the head it reads; on a producer: the row
whose work the current head is), `base_sha` (on a consumer: the base commit
the attempt started from), and `result` (on a consumer: `ok`, `fix` or `fail`
once reported). A consumer's `holder` is its row; a producer's holder is
always none. `attempt` on a producer counts its work consumers cut so far;
a consumer carries the attempt it belongs to.

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
(Live: a consumer in `ready` or `working`.)

## Consumer states

| state | meaning | holder |
|---|---|---|
| `ready` | dealt to the row, not yet taken | the row |
| `working` | the row took it | the row |
| `done` | reported (`result` ok, fix or fail; `head` set); **final** | the row |
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
| `Resolve` | waiting | ready | state | `NeedsLanded(p)`; a producer only |
| `Reached` | waiting | landed | state | `NeedsLanded(p)`; a sentinel only: it has no brief and never a consumer; its landing is the signal (a release gate, a phase mark), reported to the coordinator |
| `Start{row, branch, base}` | ready | working | state, attempt+1, author=""; **cuts** a work consumer: kind work, of p, holder row, attempt, branch, base_sha, state ready | row up and not held; stream not held; the holder write enforces width |
| `Finished{c}` (its work consumer reported ok with head H) | working | review | state, head=H, author=c's row | c is p's work consumer of this attempt, done, result ok |
| `Broken{c}` (its work consumer reported fail) | working | ready | state, reason=c's reason | c is p's work consumer of this attempt, done, result fail |
| `AskRead{row}` | review | review | **cuts** a read consumer: kind read, of p, holder row, head=p.head, author=p.author, state ready | row up and not held; stream not held; row != p.author; width by the holder write |
| `ReadYes{c}` | review | merging | state | c is p's read consumer at p.head, done, result ok; no live consumer of p |
| `ReadFix{c}` | review | ready | state, reason=c's reason | c is p's read consumer at p.head, done, result fix; no live consumer of p (layer 3 withdraws the others first) |
| `Landed{head}` | merging | landed | state | head equals p.head |
| `Red{head, reason}` | merging | review | state, reason | head equals p.head |
| `Released` | working | ready | state, reason | p's work consumer of this attempt is `fail` (withdrawn) |
| `Cancel{reason}` / `Replace{by, reason}` | any non-final | fail | state, reason | no live consumer of p |
| `NeedFailed{need}` | any non-final | fail | state, reason naming the need | need is in p's needs and is `fail`; no live consumer of p |
| `Rescore{score}` | any non-final | same | score | |
| `Block{reason}` | ready, review, or working after its consumer is withdrawn | blocked | state, reason | no live consumer of p; raised by policy when attempts pass its limit, with a judgement |
| `Unblock{reason}` | blocked | waiting | state, reason | a coordinator verb; Resolve then moves it on |

Consumer events:

| event | from | to | writes | conditions |
|---|---|---|---|---|
| `Take{row}` | ready | working | state | row is the holder |
| `Report{row, result, head, reason}` | working | done | state, result, head, reason | row is the holder; result ok needs a full 40-hex head; a read's head must equal the producer's |
| `Withdraw{reason}` | ready or working | fail | state, reason | |

Rules the tables encode, each checked by the model and tested one by one:

- `landed` and `fail` are final for a producer; `done` and `fail` for a
  consumer. No event moves them, `Rescore` included.
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
  consumers.
- A held stream stops `Start` and `AskRead` (no new consumers); live
  consumers, `Landed` and `Red` continue.

## The row machine

| event | from | to | writes |
|---|---|---|---|
| `Seen` | down | up | state=up |
| `Gone` | up | down | state=down, flaps+1 |
| `Width{n}` | any | same | width (refused below the row's live count by layer 1) |
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

A refusal is a value (`Refusal{Reason}`), never an error; the step layer logs
it through layer 1 as a refused op so it is visible.

## How it is checked

- `tla/CardMachine.tla`: one or two producers (c2 needs c1), their
  consumers, two rows, a stream with a held flag; every event above as an
  action. Invariants: final states never move; a producer never has a
  holder; working ⇔ one live work consumer; a live read consumer's holder is
  never the producer's author; attempt rises only on Start; a Red step never
  ends in merging or landed; NeedFailed is enabled whenever a need has failed
  and the dependant is non-final; holders ≤ width; fix keeps score; a
  consumer's holder never changes; a consumer of an old attempt never moves
  its producer. Reversed witnesses, each a required violation: a landed
  producer moving; the author reading their own head; red advancing;
  NeedFailed not enabled after a need failed; a report by a non-holder
  accepted; a producer with two live work consumers; a stale attempt's
  report moving the producer.
- Unit tests, package `card`: every row of the three tables accepted once and
  refused from every other state; every refusal above with its exact reason;
  no store, no clock, no sockets. A conformance test drives a producer
  through the happy path and the fix, red, withdraw and fail paths by
  applying each plan to `store.Fake`.
- Layer 1 gains `kind`, `of`, `author`, `base_sha`, `result`, the state
  `done`, and schema version 2, with conformance cases.

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
- The attempt limit that blocks a producer is policy's number; the move is the engine's. A row is never blocked: on the record (10-25, 11-05) the flaps were our hook's, not the row's; a row's flaps stay a counted fact for policy to raise a judgement on.
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
- `after` is soft: work starts whenever the card is ready, but the landing
  step (layer 3) takes merging producers in score order and skips one whose
  `after` has a card not yet landed. v1's land order, without blocking the work.
- A sentinel is a producer of kind `sentinel`: no brief, no consumer, no
  score of its own. When its needs have landed, `Reached` lands it, and that
  landing is reported. A release gate or a phase mark is a sentinel with the
  cards it waits for as its needs.
- A need failing takes a sentinel with it like any dependant.
