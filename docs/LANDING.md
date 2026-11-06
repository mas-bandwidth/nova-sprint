# Landing: merging wide, never bottlenecked

Approved by Glenn 2026-11-06 ("landing seems fine"), after: "a consistent
failure in the previous system was doing lots of work, then failing in the
merge. bottlenecking there. you need to think seriously about this now in this
redesign." And: "merging was a thing that was done wide. Merging per-work
stream where possible, not one card at a time."

The cause in v1: every cost was paid at the end. Cards were written against a
base that had moved, read late, CI'd one at a time against current main, and
conflicts showed only at merge, where nothing was left but rework. v2 moves
each cost to the earliest point it can be paid, so landing is a fast-forward
in a batch.

## The design

1. **Each stream has an integration branch** (`sprint/<stream>` by default,
   set at `stream add`). Cards branch from it, not from base, and the base
   commit is recorded on the consumer at deal (`base_sha`). A stream lands
   into base as one batch PR: one CI run per batch, not per card. The merge
   cost is per batch.
2. **Conflicts are prevented, not resolved.** Every card declares its paths
   (the card lint refuses a brief without them). Two live cards in one stream
   never overlap in paths: the deal refuses the second until the first has
   landed, naming the overlap. So merges inside a stream cannot conflict.
   Streams land into base one at a time in their order (`pos`), and a
   cross-stream conflict is a judgement to the coordinator, never a silent
   rework.
3. **The runner rebases before it reports.** When a consumer's `base_sha` is
   behind the stream head at report time, `nova-worker` rebases
   mechanically, re-runs the card's tests, pushes, and only then reports. A
   rebase that conflicts is a `fix` result with the files named, early,
   while the author still has the context. A friend row is asked the same by
   the report refusal (step 4).
4. **A report of ok is refused until the head is landable.** The head must
   be on the current stream head (rebased), pushed, and have green CI at
   that exact head; a card that wants a PR has one. Each refusal names which
   and the command. So every producer in review is already landable; the
   read is the last gate; landing is a fast-forward.
5. **In-order landing, visible when stuck.** Within a stream, producers land
   in score order (work order; Glenn 2026-09-29: "merge in work order, not
   random"). A producer ahead that is not landable holds the ones behind it;
   that is what in order means, and the merge table shows it per stream:
   queued, merged, stuck (and why: the producer and its state), CI, state. A
   stuck batch is a judgement with its verb (`rescore`, `drop`, `rework`).
   v1's merging was invisible ("You would be stuck and not know it").
   The soft `after` list orders landings across streams the same way.
6. **Base is never red from a landing.** A batch lands only on CI green
   against the exact base head it lands on, re-run if base moved
   (2026-10-27: stale-green merges put main red on 12 jobs). The batch's
   CI is one run; its result is a notification either way.
7. **Batch atomic.** A batch lands whole or not at all; a red batch returns
   every producer in it to `review` with the reason, in one op.

## What is modelled

`tla/Landing.tla` (layer 3 and 4): a card lands at most once; landings in a
stream are in score order; a batch is all or none; the base is never advanced
by a batch whose CI was not green at the exact base head; a producer in
`merging` has a head equal to its last read's head. Reversed witnesses: a
stale-green landing; a card landing twice after an unknown outcome; a batch
half-landed; a landing out of order.

## Where it lives

- The refusals of step 4 are the consumer `Report` of layer 2 plus the
  checks layer 4's git and GitHub edge runs for it.
- The path-overlap refusal is a deal condition in layer 3, reading paths from
  the card.
- The batch and the merge table are layer 3's landing step and layer 6's
  view.
