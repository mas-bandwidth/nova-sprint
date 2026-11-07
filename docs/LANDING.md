# Landing

How a card that passed its reads becomes a commit on the stream's base branch, without a
bottleneck at the end.

Every cost is paid at the earliest point it can be: cards declare their paths so live cards in one
stream never overlap (the deal refuses the second, layer 3); `finish` asks only for a head at origin;
reads happen before landing; CI runs once on the exact tree that will merge. Landing is then a
merge of a batch, wide per stream, never one card at a time through a queue.

**The automatic path is green-and-merge only. Anything else is the coordinator's judgement.**

## The ladder

Each stream lands on one rung at a time. Layer 3 asks for a landing (`steps/land.go`); layer 4
performs it (`edge/git/land.go` `LandPass`); `run` asks again where a result says to
(`run/edges.go`).

A *merging* producer is one whose reads said yes at its current head. Merging producers of a stream
without a land request in flight, and not under a land judgement, are queued in score order.

### Rung 1: one head waiting

Fewer merging producers are queued than policy's `landing(stream).backed_up` (default 2, so one
head waiting is not backed up). Layer 3 asks for the first one in score order.

If that head already contains base's tip, `LandPass` opens (or finds) the PR from the card's own
branch `sprint/<stream>/<card>` against the stream's base, reads the checks at that exact head,
and a green one merges. If the head is behind base, the pass takes rung 2's path for the one head,
so that what CI passes contains the base it merges onto.

### Rung 2: backed up

`backed_up` or more merging producers are queued: every eligible one is one request. `LandPass`
rebuilds the stream's group branch `sprint-group/<stream>` from base's tip with every head merged
in, in order, pushes it with a lease on its old tip, and opens (or finds) one PR from it against
base. The merge is deterministic (commits are dated at base's commit and made as the sprint's
commit identity), so a pass that runs again with the same base and heads pushes nothing new and CI
is not restarted. Green at the gated sha merges as one, and every card of the request is landed at
the merge sha in one op group. Red is a judgement for each card; there is no bisection and no
blame.

A rung is chosen by `backed_up` alone. `flowing` is validated (`0 <= flowing < backed_up <= 1000`)
but does not choose a rung; nothing returns a stream from rung 2 to rung 1 on it.

### Merging a green PR

Both rungs merge the same way. The checks are read at the gated sha (`forge.Checks`). Only a green
rollup merges, and only while base is still the tip the pass fetched: the merge is
`gh pr merge --match-head-commit <gated sha>` by the repository's allowed method (merge commit,
then squash, then rebase, the first it allows), so a sha CI did not pass is never merged and base
never goes red from a landing. A PR that GitHub leaves open (a merge queue or auto-merge) is a
refusal: the landing records only a merge it saw. The card is recorded `landed` with the merge sha
(`card.Landed{Head, Merge}`, reason `landed at <merge>`). Heads already on base land at once at
base's tip.

### Rung 3: anything else is the coordinator's

One `land` judgement is opened per card still merging at the request's head, on the coordinator's
row, in the tick that reads the result. Nothing moves and nothing is blamed. The outcomes that
reach it (`LandOutcome`):

| outcome | meaning |
|---|---|
| `ci_red` | a check failed that is not red on the base (new red), or red that could not be compared |
| `ci_red_inherited` | the run ended and every failing check is red on the base too |
| `ci_fault` | a check was cancelled, timed out or never started, or the checks could not be read |
| `no-ci` | no check was reported at the gated sha within `ci_wait` seconds |
| `conflict` | the heads do not merge onto base, or GitHub says the PR conflicts |
| `merge_refused` | GitHub refused the merge |
| `pr_unopenable` | the PR could not be opened or read |
| `branch_failed` | the group branch could not be created or pushed |
| `missing_head` | a card's branch is not on origin |
| `head_moved` | a card's branch is no longer at the head the read passed |
| `hand` | the stream lands by hand (mode `hand`, or no forge) |
| `fault` | git or the forge itself failed (the clone, a command) |

The judgement's question carries the facts: the card, stream, base and base tip, branch, head, the
outcome, the rung, the group tip, the gated sha, the PR, the failing checks with the first one's
URL and its first failing lines, the conflicting files, the pass's own reason, and the cards that
were landing together. Its command is the next step:

```
nova-sprint judge land.<card>.<n> <land|fix|retry> --reason <why>
```

- **land**: you looked; the head is merged by hand (see `land` below).
- **fix**: the producer goes back to work (`ready`, red, review one up) with the reason as its
  finding.
- **retry**: the judgement closes; the next tick asks a new landing.

A land judgement has no deadline; it waits for the coordinator. The producer stays `merging` with no
request until it is answered.

Not judgements, because they resolve by themselves: pending CI, and a base that moved after the
gate. `run` asks the pass again every `LandPoll` (30 s), the first for as long as CI runs, the
second at once on the new tip (rung 2 then, since the head no longer contains base's tip). No check
at all is asked again until `ci_wait` seconds (policy, default 600) have passed, then it is the
`no-ci` judgement. A CI that stays pending for ever is met by layer 3's landing-stall judgement,
not by the landing.

## Red that is not the card's

A red gated sha is split by the base before it is a judgement (`forge.Inheritance`):

1. The merge base of base's tip and the gated sha is found
   (`gh api repos/<r>/compare/<base tip>...<gated> --jq .merge_base_commit.sha`).
2. Each side's workflow runs are read (`actions/runs?head_sha=<sha>&per_page=100`). A workflow is
   failing at a sha when a run of it there failed, or is still queued or in progress with a failed
   job (`actions/runs/<id>/jobs`).
3. A failing workflow is **inherited** when the merge base's newest completed run of it failed. When
   the merge base has no completed run of it (a base with no CI, or CI still running there), the base
   branch's last completed run of the same workflow is read instead
   (`actions/workflows/<id>/runs?branch=<base>&status=completed&per_page=1`) and the judgement says
   so; with none there either, it is **new**.
4. For each new workflow, its failed jobs with their runners and pages, and the first five error
   lines of the first three jobs' logs (`actions/jobs/<id>/logs`, readable once a job ends while its
   run goes on; timestamps stripped). Inherited red's logs are not read.

A failing check that is no failed job of any workflow (a commit status or another app's check run)
is new red: nothing on the base compares with it. When a failing check is named like a contributor
or license agreement (CLA, CAA, "Contributor Assignment Agreement"), the PR's commit author
identities are named (`gh pr view <n> --json commits`): one of them has not signed. Which one is on
the check's own page; the landing does not parse it.

**Timing.** There is no judgement while the run is still queued or in progress
(`CheckRollup.Waiting`) unless a failing workflow is new. Red that is all inherited so far is
`ci_pending` ("waiting for the run") and is asked again; new red escalates at once. A split that
cannot be read waits for the run, then escalates as plain `ci_red` with the first failing lines and
the cause ("not compared with the base (...)").

**Outcomes.** New red (or uncompared red) is `ci_red`; its facts are led by
`NEW red (not red on the base): <workflow> (<basis>) [job <name> on <runner> <url>: <lines>]`, then
`inherited red (red on the base too, not this card's): ...`. Red that is all inherited, with the run
ended, is `ci_red_inherited`: still a judgement, because red never lands by itself, but worded for a
glance and pointing at the hand merge. Examples as the code words them (shas cut):

> card c1 (stream s1 onto main at a007c1800b2e, branch sprint/s1/c1, head 5e1f00000000) has only
> inherited red (its run ended; every failing workflow is red on the base too, so not this card's):
> land it with nova-sprint land c1 --reason <why>. Facts: inherited red (red on the base too, not
> this card's): corpus-check (the merge base a007c1800b2e); merge base a007c1800b2e; rung 1; base tip
> a007c1800b2e; gated 5e1f00000000; PR https://github.com/o/r/pull/31; failing corpus-check; at
> https://github.com/o/r/actions/runs/11/job/111; reason: only inherited red: corpus-check (...)

> card c2 (stream s1 onto main at a007c1800b2e, branch sprint/s1/c2, head 9c3d00000000) did not land:
> ci_red: NEW red (not red on the base): Gates (the merge base a007c1800b2e) (its run still going)
> [job Windows Release on win-1 https://github.com/o/r/actions/runs/14/job/141: error C2065: 'x':
> undeclared identifier]; inherited red (red on the base too, not this card's): corpus-check ...;
> an agreement check failed; the PR's commit authors, one of them unsigned: A Writer
> <writer@example.invalid> (a-writer); rung 1; ...

## `land`: the hand merge

`nova-sprint land <card> --reason <why> [--head <sha>]` is the coordinator's merge with no check
gate. It lands a merging producer's own finished head (`--head`, when given, must be that head).

- On a stream with a forge, the card branch's PR against base (opened when there is none) is merged
  by the allowed method, and refused if the PR's head is not the card's head.
- On a stream with no forge, the head is merged onto base's tip and pushed, fast-forward only. A
  conflict is a refusal that says to merge it into base by hand and land again.
- A head already on base records at once at base's tip.

The card is recorded `landed` with the merge sha and the reason `landed at <merge>: by hand: <why>`,
and an open land judgement on it is answered `land` in the same op. Answering a land judgement
`land` runs the same path. `LANDED <card> head=<sha> merge=<sha> by hand: ...` is the output.

## What a stream needs

- **`finish` needs only a head on origin.** It checks a full sha that is the tip of the card's branch
  at origin. It asks for no PR, no forge, no token and no up-to-date-ness with any other branch.
- **The landing acts as the host's gh identity.** The forge runs `gh` with the login of the host
  `run` is on (the one identity that can act on every stream repository). `gh` and git's github.com
  credential helper (`gh auth git-credential`) run with every GitHub credential of the process
  removed (`GH_TOKEN`, `GITHUB_TOKEN`, the enterprise tokens, `GH_CONFIG_DIR`), so one in the
  environment never makes the landing someone else. `GH_GATE_TOKEN`, when set in `run`'s
  environment, is passed to `gh` as `GH_TOKEN` instead: an optional override. Messages name the
  choice ("this host's gh login", or `GH_GATE_TOKEN`), never a login the sprint expects.
- **No CI trigger on `sprint/**` is required.** Rung 1's PR is from the card branch and rung 2's from
  the group branch; the repository's own `pull_request` workflow runs on both.
- **The group branch** `sprint-group/<stream>` lives beside `sprint/`, never in it, so neither a base
  named `sprint/<stream>` nor a card branch `sprint/<stream>/<card>` can collide with it (git keeps
  no ref that is both a leaf and a directory). `stream add` creates it at the base tip for a stream
  with a forge; the first rung-2 pass creates it if it is absent. `stream add` refuses, naming the
  fix, a stream name git cannot use in a branch, a branch at origin that keeps the group branch from
  being created (`sprint-group` itself, or a branch under `sprint-group/<stream>/`), branches it
  cannot list, and a creation that fails (push access for the host's gh identity). A card branch
  under `sprint-group/` is refused (`ValidateCardBranch`).
- **Stream settings.** `stream add --mode auto|group|hand --forge github`; `stream set` changes
  them. A stream with mode `hand` or no forge has no group branch and every card on it is a `hand`
  judgement. Mode `group` is accepted and behaves as `auto`.

## Requests are rebuilt from the store

Land requests are rebuilt from the store's state every tick (and so at `run` start): a stream with no
request in flight and merging producers not under a land judgement gets one. A request's id hashes
the epoch, stream, base tip, and each card's head and review count, so a restart reissues the same
request, and a landing of one head after a retry has an id of its own when anything changed. A
merging producer that a tick leaves with no request in flight or planned for its stream, no land
judgement and no `after` target holding it is reported as an `engine` judgement ("merging with no
land request and no land judgement: answer retry"). By construction there is none; the report is the
check that there is none.

## Ordering and invariants

- **Conflicts are prevented, not resolved.** Every card declares its paths; two live cards in one
  stream never overlap in paths. Merges inside a stream are then textually clean; one that is not is
  a rung-3 judgement.
- **In-order landing.** Producers land in score order. The soft `after` orders landings across
  streams only among cards both queued to land, and an `after` whose target is not merging never
  holds a landing.
- **Base is never red from a landing.** Only the exact sha CI passed green is merged, and only while
  base is still the tip it was gated on.
- **Nothing lands that CI did not pass, except by `land`.**
- **Landing runs on its own.** It runs in the run loop whether or not the coordinator is hooked; a
  `hold` never touches it.
- **A release is not a failure.** `release` withdraws a consumer with the reason `released: <why>`
  and no result, neither ok nor fail. The re-deal is a new attempt of the same review, and nothing
  is counted against the card or the row.

## Behaviour to know

- **A land request is planned from the tick's view** and carries no fence against a coordinator's
  answer made after it. An answer (`fix`) that races a tick can leave a request in flight for a head
  just sent to fix, and a green one would merge it. Layer 3 then records nothing (the card is no
  longer merging at that head), but the head is on base. `Steps.tla`'s `EdgeAfterAccepted` states
  the race rather than forbidding it.
- **Pending CI has no bound of its own.** A check that never leaves pending is met by the stall
  judgement, not by `ci_wait`. Waiting for the run behind inherited red has none either.
- **The base split is read again every pass.** A pass waiting behind inherited red re-reads the merge
  base, both sides' runs and the running runs' jobs every `LandPoll`: a handful of GitHub calls per
  stream per pass, against the host login's hourly rate limit. A rate limit rests the identity, and
  the pass, failing to split while the run goes on, waits.

## What is modelled

`tla/LandEdge.tla` (layers 3 and 4): the rungs from a tick's land request to the base, with outside
commits to base and the coordinator's answers. Automatic moves: green to landed; anything else to a
judgement. Invariants:

- `BaseNeverRed`: every tree the landing put on base was green.
- `NothingLandsUnpassed`: a card enters base only in the exact tree CI gated, or by the
  coordinator's hand; an outside commit brings none; a landed card is on base.
- `MergingCovered`: after a tick, every merging card is under a land judgement, or its stream has a
  request in flight, or had one when the tick began.

Reversed witnesses, one each, under `tla/witnesses/`: `LandEdgeRedLands` (the merge does not read
the gate, so a red tree merges), `LandEdgeStaleGate` (base moves under the gate and the pass merges
anyway), `LandEdgeJudgementHoldsStream` (a judgement on one card holds its whole stream, leaving the
stream's other merging cards with neither a request nor a judgement). Reach checks show rung 2, a
judgement, a hand landing and a red gate. In the model every non-green, non-absent gate is one
judgement, which `ci_red_inherited` still is, and a pass is atomic up to its final outcome, so
waiting for the run is the same re-ask as pending CI.

`tla/Steps.tla` carries the land judgement and the `Answer` action (land, fix, retry) on layer 3's
side, with `MergingCovered` and the reversed witness `StepsLandAfterHoldsUntilLanded` (the `after`
hold kept until the earlier card lands, so a card after one sent to fix sits with no request).

The code is bound to `LandEdge.tla` by `edge/git/trace_test.go` (`TestLandEdgeTraceRefinementWalk`:
`LandPass` on the git fake and a truthful forge, with the tick, outside commits and the answers
played by the walk), checked by `tla/check-trace.sh LandEdge`.

## Where it lives

- Layer 3: `steps/land.go` (land requests: rung choice, order, the id; results: landed as one group,
  anything else a land judgement; the unlanded report) and `steps/judge.go` (the judgement's choices
  and no deadline).
- Layer 4: `edge/git/land.go` (`LandPass`), `edge/git/git.go` (the group branch's name and its
  blockers), `edge/forge` (`PR`, `Checks`, `Merge`, `FailedLines`; `actions.go`: `Inheritance`,
  `Authors`, `JobLines`).
- Layer 6: `run/edges.go` (passes asked again on pending CI and a moved base, `ci_wait` for absent
  CI, the facts), `verbs/land.go` (`land`, the group branch at `stream add`), `verbs/judge.go` (the
  answers), `cmd/nova-sprint/lander.go` (the hand merge as the host's gh identity).
