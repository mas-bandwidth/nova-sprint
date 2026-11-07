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

## The ladder, as built (intent rules 16 and 32, 2026-11-07)

The first ladder (four rungs, bisection of red groups, a base-red judgement,
CI required on `sprint/**`, the sprint's own token) was found by the dogfood
of 2026-11-07 to be ignored in the first implementation and too much
machine for what landing needs. Rule 32 replaced it: **the automatic path
is green-and-merge only; anything else is the coordinator's judgement.**
Three rungs, each stream on one at a time; the group rung is kept.

1. **Rung 1: one head waiting.** The stream has fewer merging producers
   than policy's `landing(stream).backed_up` (default 2: more than one head
   waiting backs it up). Layer 3 asks a land request for the first one in
   score order (after-ordered). Layer 4's `LandPass` opens (or finds) the
   PR from the card's own branch (`sprint/<stream>/<card>`) against the
   stream's base, provided the head already contains base's tip; it reads
   the checks at that exact head, and a green one merges by the
   repository's allowed method (merge commit, then squash, then rebase)
   with `gh pr merge --match-head-commit <head>`, only while base is still
   the tip the head was gated on. The card is recorded `landed` with the
   merge sha (`card.Landed{Head, Merge}`; its reason `landed at <merge>`).
2. **Rung 2: more than one head waiting** (or one head behind base). Every
   eligible merging producer of the stream is one request. `LandPass`
   rebuilds the stream's group branch `sprint-group/<stream>` from base's
   tip with every head merged in, in order (a deterministic merge dated at
   base's commit, so a pass that runs again pushes nothing new and CI is
   not restarted), pushes it with a lease on its old tip, and opens (or
   finds) one PR from it against base. Green at the gated sha merges as
   one, the same way as rung 1, and every card of the request is landed at
   the merge sha in one op group. Red is a coordinator judgement for each
   card: no bisection and no blame.
3. **Rung 3: anything else is the coordinator's.** Red CI; no check at all
   within `ci_wait` seconds (policy, default 600, a guess); a conflict (git's
   merge or GitHub's mergeable state); a merge GitHub refused; a PR that
   could not be opened or read; a group branch that could not be pushed; a
   card branch missing or moved from its read head; a stream landed by
   hand (mode hand, or no forge); or a fault of git or the forge itself:
   each is one `land` judgement per card on the coordinator's row, opened
   in the tick that reads the result. Nothing moves and nothing is blamed.
   Its question carries the facts: the card, stream, base and base tip,
   branch, head, the outcome, the rung, the group tip, the gated sha, the
   PR, the failing checks with the first one's URL and its first failing
   lines, the conflicting files, the pass's own reason, and the cards that
   were landing together. Its command is the next step:
   `nova-sprint judge land.<card>.<n> <land|fix|retry> --reason <why>`.
   - **land**: the coordinator looked; the head is merged by hand (below).
   - **fix**: the producer goes back to work (`ready`, red, review one up)
     with the reason as its finding (intent rule 30).
   - **retry**: the judgement closes; the next tick asks a new landing.
   A land judgement has no deadline: it waits for the coordinator.

Pending CI and a base that moved after the gate are not judgements: run
asks the pass again every `LandPoll` (30 s), the first as long as CI runs,
the second at once on the new tip (rung 2 then, since the head no longer
contains base's tip). A CI that stays pending for ever is met by the stall
judgement (layer 3), not by the landing.

### land: the hand merge

`nova-sprint land <card> --reason <why> [--head <sha>]` is the coordinator's
merge with no check gate. It lands a merging producer's own read-ok head
(`--head`, when given, must be that head): on a stream with a forge, the
card branch's PR against base (opened when there is none) is merged by the
allowed method, refused if the PR's head is not the card's head; on a
stream with no forge, the head is merged onto base's tip and pushed,
fast-forward only, and a conflict is a refusal that says to merge it by
hand and land again. A head already on base records at once at base's tip.
The card is recorded `landed` with the merge sha and the reason
`landed at <merge>: by hand: <why>`, and an open land judgement on it is
answered `land` in the same op. Answering a land judgement `land` runs the
same path.

### What a stream needs

- **finish needs only a head on origin.** `finish` checks a full sha, not
  the base nor the prior head, at the tip of the card's branch at origin.
  It asks for no PR, no forge, no token and no up-to-date-ness with any
  other branch: the landing does the forge work.
- **The run lands as the host's gh identity.** The forge runs `gh` with the
  host's own login (the coordinator's host, the one identity that can act on
  every stream repository). `GH_GATE_TOKEN`, when set in run's environment,
  is passed to gh as `GH_TOKEN` instead: an optional override, not a
  requirement. No CI trigger on `sprint/**` is required: rung 1's PR is
  from the card branch and rung 2's from the group branch, and the
  repository's own `pull_request` workflow runs on both.
- **The group branch** `sprint-group/<stream>` (beside `sprint/`, never in
  it, so neither a base named `sprint/<stream>` nor a card branch
  `sprint/<stream>/<card>` can collide with it; defect 46) is created at
  `stream add` from the base tip for a stream with a forge, and by the
  first rung-2 pass if it is absent. `stream add` refuses, naming the fix,
  a stream name git cannot use in a branch, a branch at origin that keeps
  the group branch from being created (`sprint-group` itself, or a branch
  under `sprint-group/<stream>/`), branches it cannot list, and a creation
  that fails (push access for this host's gh identity).

### Requests rebuilt, nothing unexplained

Land requests are rebuilt from the store's state every tick (and so at run
start): a stream with no request in flight and merging producers not under
a land judgement gets one. A request's id hashes the epoch, stream, base
tip, cards, heads and reviews, so a restart reissues the same request. A
merging producer that a tick leaves with no request in flight or planned
for its stream, no land judgement, and no `after` target holding it is
reported as an `engine` judgement ("merging with no land request and no
land judgement: answer retry"); by construction there is none, and the
report is the check that there is none.

### A release is not a failure

`release` withdraws a consumer with the reason `released: <why>` and no
result: neither ok nor fail. The re-deal is a new attempt of the same
review, and nothing is counted against the card or the row.

## Kept from the first design

- **Conflicts are prevented, not resolved.** Every card declares its paths;
  two live cards in one stream never overlap in paths (the deal refuses the
  second, layer 3). Merges inside a stream are then textually clean; one
  that is not is a rung-3 judgement.
- **In-order landing.** Producers land in score order; the soft `after`
  orders landings across streams only among cards both queued to land, and
  an `after` whose target is not merging never holds a landing (LESSONS
  2.15).
- **Base is never red from a landing.** Only the exact sha CI passed green
  is merged, and only while base is still the tip it was gated on.
- **Landing runs on its own.** The landing runs in the run loop whether or
  not the coordinator is hooked; a hold never touches it.

## What is modelled

`tla/LandEdge.tla` (layers 3 and 4): the three rungs from a tick's land
request to the base, with outside commits to base and the coordinator's
answers. Automatic moves: green to landed; anything else to a judgement.
Invariants: `BaseNeverRed` (every tree the landing put on base was green),
`NothingLandsUnpassed` (a card enters base only in the exact tree CI gated,
or by the coordinator's hand; an outside commit brings none; a landed card
is on base), `MergingCovered` (after a tick, every merging card is under a
land judgement or its stream has a request in flight, or had one when the
tick began). Reversed witnesses, one each: `LandEdgeRedLands` (the merge
does not read the gate, so a red tree merges), `LandEdgeStaleGate` (base
moves under the gate and the pass merges anyway: the heads on the new tip
land, not the tree CI passed), `LandEdgeJudgementHoldsStream` (a judgement
on one card holds its whole stream, so the stream's other merging cards
are left with neither a request nor a judgement). Reach checks show rung 2, a
judgement, a hand landing and a red gate. `tla/Steps.tla` carries the land
judgement and the `Answer` action (land, fix, retry) on layer 3's side, with
`MergingCovered` and the reversed witness `StepsLandJudgementExpires`. The
code is bound to `LandEdge.tla` by `edge/git/trace_test.go`
(`TestLandEdgeTraceRefinementWalk`: `LandPass` on the git fake and a
truthful forge, with the tick, outside commits and the answers played by
the walk), checked by `tla/check-trace.sh LandEdge`.

## Known gaps

- **A stale land request.** A land request is planned from the tick's view
  and carries no fence against a coordinator's answer made after it: an
  answer (fix) that races a tick can leave a request in flight for a head
  that was just sent to fix, and a green one would merge it. Layer 3 then
  records nothing (the card is no longer merging at that head), but the
  head is on base. `Steps.tla`'s `EdgeAfterAccepted` states the race rather
  than forbidding it.
- **`flowing` is validated and not used.** Rung 2 is taken whenever
  `backed_up` or more heads wait; there is no hysteresis back to rung 1.
- **Pending CI has no bound of its own.** A check that never leaves pending
  is met by the stall judgement, not by `ci_wait`.
- **Stream mode `group`** is accepted and behaves as `auto`.

## Where it lives

- Layer 3 (`steps/land.go`): land requests (rung choice, order, the id),
  results (landed as one group; anything else a land judgement), the
  unlanded report; `steps/judge.go`: the land judgement's choices and no
  deadline.
- Layer 4 (`edge/git/land.go` `LandPass`; `edge/forge` PR, Checks, Merge,
  FailedLines): one pass of either automatic rung; `edge/git/git.go`: the
  group branch's name and its blockers.
- Layer 6 (`run/edges.go`): passes asked again on pending CI and a moved
  base, `ci_wait` for absent CI, the facts; `verbs/land.go`: `land`, the
  group branch at `stream add`; `verbs/judge.go`: the answers;
  `cmd/nova-sprint/lander.go`: the hand merge as the host's gh identity.
