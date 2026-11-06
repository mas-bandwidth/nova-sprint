# Layer 2: the card machine

Approved by Glenn 2026-11-06 ("Looks fine. Continue."). Every legal move of a
card, and of a row, lives here and nowhere else. Pure Go, package `card`: no
store, no clock, no bus. Given the current rows it touches and one event, it
returns the plan layer 1 applies (conditions and writes from package `store`)
or a refusal. Layer 3 (the steps) builds the event from a request and the
snapshot; layer 5 (policy) decides *when* to raise an event; this layer says
*whether* the move is legal and *what* it writes.

## The card states

The six states Glenn locked on 2026-09-29, plus `fail`:

| state | meaning | holder |
|---|---|---|
| `waiting` | a need has not landed | none |
| `ready` | every need landed; not dealt | none |
| `working` | dealt to a row | the worker |
| `review` | finished with a head; a read is owed or in progress | none until a reader is dealt, then the reader |
| `merging` | a read said yes to this head; landing in progress | none |
| `landed` | the head is on the base branch; **final** | none |
| `fail` | stopped any other way (cancelled, replaced, a need failed); **final**, reason kept | none |

Two columns arrive with this layer, schema version 2: `author text` (the row
whose work the card's head is; set on finish, kept through review, merging
and landed) and `base_sha text` (the base commit the attempt started from; set
on deal). Both are plain card writes in layer 1 (`SetCardAuthor`,
`SetCardBaseSHA`), added with the version bump, and gen rises as for any card
write.

## The moves

Each row below is one event; the machine checks the pre-state and the actors
and returns the writes, with the card's gen as a condition (the plan is fenced
on the snapshot the event was built from) plus the conditions named.

| event | from | to | writes | extra conditions |
|---|---|---|---|---|
| `Resolve` | waiting | ready | state | `NeedsLanded(card)` |
| `Deal{row, branch, base}` | ready | working | state, holder=row, attempt+1, branch, base_sha, reason="" | row `up`, stream not held (width is enforced by the holder write) |
| `Finish{row, head}` | working | review | state, holder=none, author=row, head | holder is `row` |
| `DealRead{row}` | review, holder none | review | holder=row | row `up`, row != author (width is enforced by the holder write) |
| `ReadYes{row, head}` | review, holder row | merging | state, holder=none | head equals the card's head |
| `ReadFix{row, head, reason}` | review, holder row | ready | state, holder=none, reason | head equals the card's head; score untouched |
| `Landed{head}` | merging | landed | state | head equals the card's head |
| `Red{head, reason}` | merging | review | state, holder=none, reason | head equals the card's head |
| `Release{reason}` | working, or review with a reader | ready, or review with holder none | holder=none, reason; attempt kept | |
| `Cancel{reason}` | any non-final | fail | state, holder=none, reason | |
| `Replace{by, reason}` | any non-final | fail | state, holder=none, reason naming `by` | |
| `NeedFailed{need}` | any non-final | fail | state, holder=none, reason naming the need | `need` is in the card's needs |
| `Rescore{score}` | any non-final | same | score | |

Rules the table encodes, so they are checked by the model and tested one by
one:

- `landed` and `fail` are final: no event moves them, including `Rescore`.
- A red CI result never advances a card; it retreats `merging → review` and
  is reported (layer 3 raises the notification; this layer only moves).
- A fix keeps the score, so a fixed card is at the head of its queue without
  any priority rule (Glenn, 2026-09-29).
- `attempt` rises only on `Deal`; a release keeps it, so the next deal is a
  new attempt and the old attempt's report cannot finish the new one (the
  step layer fences finish on attempt as well as holder).
- The reader is never the author. One reader at a time per head: the holder
  is one column in every state. More reads of one head is a policy loop of
  one read at a time, never a second holder.
- A row going down does **not** take back its cards. `Release` is a separate
  event that policy raises after a silence it has stated; the engine offers
  the move and never decides the silence (Johnny, Emma, 2026-11-06).
- A need going `fail` takes its dependants: layer 3 raises `NeedFailed` for
  every card whose needs name it, in the same tick.

## The row machine

| event | from | to | writes |
|---|---|---|---|
| `Seen` | down | up | state=up |
| `Gone` | up | down | state=down, flaps+1 |
| `Flapped{limit}` | up or down, flaps >= limit | blocked | state=blocked |
| `Unblock` | blocked | down | state=down, flaps=0 |
| `Width{n}` | any | same | width (refused below the row's held count by layer 1) |

`limit` is a number policy supplies; the move is the engine's, the number is
not. `blocked` leaves only by `Unblock`, which is a coordinator verb. A row's
state never moves a card by itself.

## What it refuses, naming the fix

- A move from `landed` or `fail`: "card X is landed; nothing moves a landed card".
- An event whose from-state does not match: "card X is in review, Finish needs working".
- A finish, read or release by a row that is not the holder: "card X is held by r1, not r2".
- A head that is not a full 40-hex commit: "head abc is not a full commit sha".
- A read result for a head that is not the card's: "card X is at head H1, the read was of H2".
- A read dealt to the author: "r1 wrote card X and may not read it".
- A deal to a row that is down or blocked, or to a held stream.
- `NeedFailed` naming a card that is not a need of this card.
- `Flapped` below the limit; `Unblock` of a row that is not blocked.

A refusal is a value (`Refusal{Reason}`), never an error; the step layer logs
it through layer 1 as a refused op so it is visible.

## How it is checked

- `tla/CardMachine.tla`: one card (or two, for `NeedFailed`), two rows, the
  events above as actions, with holder, author, attempt and head as
  variables. Invariants: final states never move; holder is none in waiting,
  ready, merging, landed and fail; in review the holder is never the author;
  in working the holder is set; attempt rises only on Deal; red never reaches
  merging or landed; a card whose need failed is failed; one holder. Reversed
  witnesses, each a required violation: a landed card moving; the author
  reading their own work; red CI advancing; a need-failed card staying ready; a
  release that bumps attempt; a finish by a non-holder accepted.
- Unit tests, package `card`: every row of both tables accepted once and
  refused from every other state; every refusal above with its exact reason;
  no store, no clock, no sockets. The plans are checked against
  `store.Fake` in a conformance test so the writes are ones layer 1 accepts.
- Layer 1 gains the two writes and schema version 2 with their own
  conformance cases.

## Size

About 900 lines of Go. An estimate from the event count.

## Decided inside the layer (Rowan, 2026-11-06), easy to overrule

- Review has two shapes told apart by the holder (none: a read is owed;
  a row: that row is reading), instead of a second state. Fewer states, and
  the invariant "one holder column" holds everywhere.
- `author` is set at finish, not at deal: the author is whoever produced the
  head, and a release before a finish leaves no author.
- `Rescore` is the only move that touches score, and it is a coordinator verb
  (Glenn, 2026-09-29: only an explicit re-rank changes the order).
- The flap limit is an input to `Flapped`, not a constant here, so the rule
  changes without a binary (layer 5).
