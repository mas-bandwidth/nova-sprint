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
   in the tick that reads the result (a group's merge conflict that names
   its card: that card alone, "One card's conflict" below). Nothing moves and nothing is blamed.
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
judgement (layer 3), not by the landing. A PR or checks read that GitHub
rate-limited (`forge_rested`) is asked again the same way, said once in the
run log as an edge fault; the identity rests meanwhile, so those passes make
no call.

### A group in flight (2026-11-11)

Every pass used to rebuild the gate from the base tip as it was then: each
base move while CI ran pushed the group branch again, its CI started over
on the new sha (and a workflow that cancels superseded runs cancelled the
old one), and on a busy base groups never reached a verdict. Now the run keeps the gate a pass pushed
(`LandRequest.Gate`, from `LandResult.Kept`) for the next pass of the
same request, and a kept gate is read as it is, on the base it was built
on, while its CI has no verdict (pending, unfinished, absent, stopped jobs
run again, a rested forge). It is never rebuilt or pushed again meanwhile.
It is let go only when:

- it is green and the base moved since it was built: `base_moved`, and
  the next pass builds on the new tip (green merges only while base is
  still the tip it was gated on, as before);
- GitHub says its PR conflicts with a base that moved: it is built again
  in the same pass, and a merge that fails is the conflict (below);
- its branch is no longer at it on origin: built again in the same pass.

A red or a no verdict is judged on the gate as built, with its base tip
named. One group is in flight per stream: layer 3 asks a stream's next
landing only once the one in flight has a result, so cards that become
ready meanwhile wait for the next group, and the run never supersedes the
group in flight. A run restarted mid-landing loses the kept gate (it is
in memory), and its first pass builds on the tip as it is: at most one
supersede per restart.

Latency: a group whose CI is still running is no longer restarted, so it
reaches a verdict in one CI run; the price is that a green gate on a base
that moved costs one more CI run, on the new tip, before it can merge.

### One card's conflict (2026-11-11)

The group's merge takes its heads in landing order; the first whose merge
fails is named (`LandResult.ConflictHead`, the inbox item's `Head`). That
card alone gets the `conflict` judgement, with the files, and the
question says which cards are landing again without it; the group's other
cards are not judged, and the next tick asks them again as a new landing
with no answer needed. The named card may conflict with the base or with a
card before it in the group; the judgement says so. A conflict that names
no head (GitHub's mergeable state on the group's PR) is still one
judgement per card.

### A verdict only from completed checks (2026-11-11)

The sprint of 2026-11-10 judged landings red while their CI still ran: the
rollup is red as soon as one check fails, and one killed job read as the
card's fault. A land verdict is now taken only from completed checks:

- **Checks still running.** A gated sha with a failed check while others
  still run (`CheckRollup.Waiting`) is `ci_unfinished`: no log is read and
  the pass is asked again. When `ci_wait` passes with checks still running
  it is one `ci_no_verdict` judgement: "no verdict after <wait>: <n> checks
  still pending; failed so far: <names>", a different cause from red.
- **Stopped is not failed.** Once the checks complete, each failing one is
  read (`forge.Failures`: its conclusion and its failure annotations, one
  GraphQL call per 100 checks) and those GitHub says were stopped (a
  conclusion of cancelled or timed out; an annotation "exit code 137",
  "exit code 143", "exceeded the maximum execution time", "has timed out
  after", "the operation was canceled") are named apart in the judgement
  as "stopped, not failed (no verdict): <name> <how>". The words are
  GitHub's own, never a fleet's: a job killed at a runner's time limit, by
  the out-of-memory killer or by hand all read "killed (exit code 137)",
  and a run cancelled by a newer one reads as cancelled. They cannot be told
  apart from here.
- **Every failing check stopped.** That is no verdict about the code: the
  stopped jobs are run again once (`gh run rerun <run> --failed`, one call
  per distinct run, outcome `ci_rerun`, asked again), and if they are
  stopped again, or the rerun is refused, it is one `ci_no_verdict`
  judgement saying so. A request is run again at most once (`reran`).
- **Red is a check that failed on its own**, after every check completed:
  `ci_red`, with those checks, the first one's URL and its first failing
  lines (read once, from that completed run).

### A red base

A red gated sha is one `ci_red` judgement at once, with the failing checks
and the first failing lines of the first one. The pass also reads the base
tip's own checks (`forge.Checks` on the base tip). When the base itself is
red, the judgement leads with both sides, plainly:

> card c1 (stream s1 onto main at a007c1800b2e, branch sprint/s1/c1, head
> 5e1f00000000) did not land: ci_red: the base itself is red at its tip
> a007c1800b2e on: corpus; the card's head is red on: corpus, unit; rung 1;
> ... land: you looked, merge it by hand; fix: back to its producer with
> your finding as the reason; retry: a new landing

"The base itself is red" is said only from the base tip's completed checks.
While they still run the reason says "base CI still running at its tip
<sha>: <n> red so far (<names>)", and no comparison is made; the head's
red is itself taken only from completed checks (above).

A red base is a merge question for the whole picture, so the landing does
not classify which of the head's failures are "the base's" and gives no
advice: the coordinator decides with all three choices (land, fix, retry).
A base tip whose checks cannot be read is said in the reason. Red never
lands by itself (`BaseNeverRed`).

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
  the base nor a prior head an earlier attempt finished, at the tip of the
  card's branch at origin.
  It asks for no PR, no forge, no token and no up-to-date-ness with any
  other branch: the landing does the forge work.
- **The run lands as the host's gh identity.** The forge runs `gh` with the
  host's own login (the coordinator's host, the one identity that can act on
  every stream repository): gh, and git's github.com credential helper
  (`env -u ... gh auth git-credential`), run with every GitHub credential of
  the process removed (`GH_TOKEN`, `GITHUB_TOKEN`, the enterprise tokens,
  `GH_CONFIG_DIR`), so one in the environment never makes the landing
  someone else (directive 59). `GH_GATE_TOKEN`, when set in run's
  environment, is passed to gh as `GH_TOKEN` instead. A bench worker
  refuses to start without `GH_GATE_TOKEN` in its environment. Messages name the choice ("this host's gh login", or
  `GH_GATE_TOKEN`), never a login the sprint expects. No CI trigger on `sprint/**` is required: rung 1's PR is
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

### A stream's base changed (2026-11-11)

A stream lands into a branch that does not move: a stream branch
that nothing else merges into, promoted into main by the sprint.
`nova-sprint stream set <s> --base <branch>` moves a live stream onto
one. The branch is admitted as `stream add` admits a base: it must be at
origin, and must not be `sprint` or `sprint/<s>`. The command is refused
while cards merged onto the old base wait for their promotion to main,
because a promotion looks for their merges on the stream's base. Otherwise
it is one write, and the receipt names the old base and what happens to
the cards:

- **Cards merging.** The landing reads the stream's base before each pass
  (`Edges.Streams`, run's folded streams). A gate built on the old base
  is let go, and the group is built on the new base at the next pass.
  Nothing built on the old base lands: a merge also needs the base tip
  it was gated on.
- **Cards working.** Each finishes its attempt on the old base (its job
  record names the base it was taken on, and its rebase step fetches that
  branch). Its head then lands onto the new base, merged by the landing.
- **Cards ready, and every retry,** start from the new base.

In `LandEdge.tla` this is `Switch(s)`. The base's tip becomes a tree no
gate was built on, and a gate in flight is let go. `NothingLandsUnpassed`
covers it, and `GateReachesVerdict` allows a gate that was let go by a
switch. The reach check `NeverSwitchInFlight` shows a switch under a
gate. The card machine has no base, so a switch changes no card state.

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

## Landed means on main (2026-11-08)

The owner, 2026-11-08: "to me landed means on main". Until then a card was
recorded `landed` when it merged into its stream's base, and a base is often
a workstream branch (`sprint/<workstream>`) that nothing merged into main:
319 cards read landed while their work was off main. Workstream bases stay
(many cards move `sprint/<workstream>` forward while main moves fast); the
landing now also carries each base to main.

- **main** is the repository's default branch, read at origin
  (`git ls-remote --symref origin HEAD`), never a name in the sprint.
- **A card is `landed` only when its work is on main**: its merge sha (the
  commit its landing made, or found, on its base) is reachable from main's
  tip, checked with `git merge-base --is-ancestor` after a fetch, as the
  landing checks everything else. It is never recorded landed earlier.
- **On a stream whose base is main nothing changes**: rung 1 or 2 merges
  onto main and the card is `landed` at the merge.
- **On any other base, the merge onto base is an intermediate step, not a
  state.** The card stays `merging`, and the move `OnBase{Head, Merge}`
  records the base merge in its reason: `on base <merge>: merged onto
  <base>; landed when that merge is on main`. A merging producer with that
  record is *on base*: it is never asked to land onto its base again, and
  nothing else about it changes (a coordinator's `fix` sends it back as
  before, and the reason that move writes clears the record).
- **Promotion.** Per stream with producers on base, not under a land
  judgement, and no promotion in flight, the tick asks one *promote*
  request (edge request kind `promote`): the stream, its on-base cards,
  their heads and their recorded merges. Layer 4's `PromotePass`:
  1. reads main's name and tip; every recorded merge already reachable
     from main is landed at main's tip (this comes first, so a base
     deleted, or another stream's promotion that already carried it, is
     no fault);
  2. reads base's tip; a recorded merge that is not on base (the base was
     force-pushed or reset) is the coordinator's judgement;
  3. a base that does not contain main's tip (behind main: it needs main
     merged in) is the coordinator's judgement (`base_behind`), never a
     merge the sprint makes;
  4. otherwise it finds or opens the one PR base -> main and gates it at
     base's tip exactly as rung 1 gates a card's branch (`gateAndMerge`):
     green at that sha while main is still the gated tip merges by the
     repository's allowed method; pending CI, a PR head not yet at the
     gated sha and a main that moved are asked again; red, no CI, a
     conflict, a refused merge and a PR that cannot be opened are the
     coordinator's judgement.
  A landed promotion lands every card of the request whose merge is on
  the gated base tip, at the merge commit main then has. Its results come
  back as land results; anything not landed opens the same `land`
  judgement per card, with the same choices: **land** (the hand landing
  below), **fix**, **retry**.
- **Hand landings follow the same rule**: `nova-sprint land` (and a `land`
  answer) merges a card not yet on base onto its base as before, then reads
  whether that merge is on main (`Lander.OnMain`): on main it is `landed`,
  else it is recorded on base and promoted. A card already on base is not
  merged again: it is `landed` when its recorded merge (its head when none
  is recorded) is on main, and otherwise refused, naming the fix (merge
  the base into main, then land again).
- **Two streams on one base** each ask their own promotion; the passes run
  under the one clone lock of their repository, so the second reads main
  after the first merged and finds its merges already there.
- **Counts follow the state**: the board's landed count, `check` and the
  land-rate count `landed` producers, so they count landed-on-main only.
  `check`'s landing line says how many of a stream's merging producers are
  on base. The board's merging column counts them, as before.
- **Store.** `Migrate` moves every producer of the current epoch recorded
  landed before this change (reason `landed at <merge>`; the v1 import's
  excluded) back to `merging`, on base at that merge (no merge in the
  reason: on base with none, and the promotion checks the head instead),
  the reason noting `recorded landed before landed meant on main`. The
  next ticks promote them: those already on main land at once. New
  landings read `landed on main at <merge>`, so the migration is a no-op
  the second time. The ETA's landing times skip the re-landing of a
  migrated card, so no card is counted twice. Earlier epochs, which no
  rebuild reads, are left as they are.

Known gaps of this step: the check is reachability, so a promotion merged
by squash or rebase leaves the merges unreachable (the cards stay on base
and the next promotion is a `base_behind` judgement: prefer merge commits
for base -> main); a base merge later reverted on base is still reachable
and lands; a base that moves faster than its CI restarts the promotion's
gate each time.

## Kept from the first design

- **Conflicts are stitched, not prevented.** Every card declares its paths
  (admission and the worker's confinement), but two cards that edit one
  file both start: only a NEEDS orders starts (2026-11-08). Their heads meet
  on the group rung; a conflict there is a rung-3 judgement, and the
  coordinator stitches it in the holistic merge.
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
are left with neither a request nor a judgement). CI takes time in the
model (`CIStep`: a gate's checks run, may fail while others run, and
complete green, red or stopped; `Late`: the wait passes), with three more
invariants: `VerdictOnlyCompleted` (red only from completed checks),
`RedIsReal` (red only on a tree that is red; a stopped gate is never red)
and `StoppedRunAgainOnce` (a completed gate is no verdict only after its
stopped jobs were run again once). Their reversed witnesses:
`LandEdgeRedWhileRunning` (a failure while checks run is judged red),
`LandEdgeStoppedIsRed` (stopped jobs read as failed) and
`LandEdgeStoppedJudgedAtOnce` (no rerun before the no verdict). The gate
is kept (`Kept`, `LetGo`): `GateKept` (an action property: a gate whose
CI runs is never replaced while its request is in flight) and, on
`LiveSpec` (CI that runs completes; `LandEdgeLive.cfg`),
`GateReachesVerdict` (a gate in flight reaches its verdict, or its
request an outcome, the same gate); reversed witness
`LandEdgeRebuildsInFlight` (the 2.0.36 rule: rebuilt when base moved).
A conflict names its card (`odd`) and the tick judges that card alone:
`ConflictJudgesItsCard` (a card judged for a conflict conflicts with
something), reversed witness `LandEdgeConflictJudgesGroup` (the 2.0.37
rule: the whole group judged). Reach checks show rung 2, a judgement, a
hand landing, a red gate, a no verdict, a rerun, a wait that passed, a
gate kept while base moved, and a group's conflict naming one card. The
trace walk plays CI per gate and keeps the gate as the run does, and
committed breakers are refused at their last record:
`tla/traces/landedge-breaker-red-while-running` and
`landedge-breaker-stopped-is-red` (the 2.0.36 judgements),
`landedge-breaker-rebuilds-in-flight` (the 2.0.36 run, no gate kept) and
`landedge-breaker-conflict-judges-group` (the 2.0.37 tick). `tla/Steps.tla` carries the land
judgement and the `Answer` action (land, fix, retry) on layer 3's side, with
`MergingCovered` (judgements as the tick's snapshot saw them) and the
reversed witness `StepsLandAfterHoldsUntilLanded` (the after hold kept until the earlier card lands, so a card after one sent to fix sits with no request).
Landed means on main is modelled there too: `w.ob` (the merging producers
on base), `onmain` (the producers whose work is on main, set only by a
promotion, a landing onto a main base or a hand landing on main), stream
`s2`'s base not main in the land and burst configurations, the promote
request (`PromReq`) and its results, and the invariant `LandedOnMain`
(every landed producer is in `onmain`), with `MergingCovered` counting a
promotion in flight. Its reversed witness `StepsLandedBeforeMain` records
a merge onto a base that is not main as landed and breaks `LandedOnMain`;
reach checks show a promote request, a card on base and a landing off a
main base reaching main. The
code is bound to `LandEdge.tla` by `modules/edge/git/trace_test.go`
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
- **Pending CI has no bound of its own.** A check that never leaves pending,
  with none failed, is met by the stall judgement, not by `ci_wait`; one
  failed while others run is bounded by `ci_wait` (`ci_no_verdict`).
- **A rerun not yet visible.** The pass after `ci_rerun` reads the checks
  again; if GitHub still shows the old, stopped run there, that pass is the
  no verdict. Not seen live; a rerun's checks are queued at once.
- **Why a job was killed is not known here.** Exit 137 is the same for a
  runner's time limit, the out-of-memory killer and a hand kill.
- **Stream mode `group`** is accepted and behaves as `auto`.
- **A base that moves faster than CI.** A green gate merges only while the
  base is still the tip it was gated on; if the base always moves within
  one CI run, a group is green, let go, built again, and never merges. The
  coordinator sees no judgement for it (each pass is asked again). Not
  seen live yet; the fix would be a policy choice (merge a green gate whose
  base moved by a fast-forward-safe rule, or hold other landings).
- **The kept gate is in memory.** A restart mid-landing builds on the tip
  as it is, once.

## Where it lives

- Layer 3 (`modules/steps/land.go`): land requests (rung choice, order, the id),
  results (landed as one group; anything else a land judgement), the
  unlanded report; `modules/steps/judge.go`: the land judgement's choices and no
  deadline.
- Layer 2 (`modules/card`): `OnBase{Head, Merge, Base}`, the on-base
  record; `modules/store/landings.go`: the reasons (`landed on main`, `on
  base`), `BaseMerge`; `modules/store/pg.go` `migrateLanded`: the store's
  migration.
- Layer 4 (`modules/edge/git/land.go` `LandPass` and `PromotePass`; `Git.Main`; `modules/edge/forge` PR, Checks, Merge,
  FailedLines): one pass of either automatic rung, a red one with the base
  tip's own red beside it; `modules/edge/git/git.go`: the
  group branch's name and its blockers.
- Layer 6 (`modules/run/edges.go`): passes asked again on pending CI and a moved
  base, `ci_wait` for absent CI, the facts, whether a landing is on main
  (`InboxItem.Main`); `modules/verbs/land.go`: `land`, the
  group branch at `stream add`; `modules/verbs/judge.go`: the answers;
  `cmd/nova-sprint/lander.go`: the hand merge as the host's gh identity, and
  `OnMain`; `modules/verbs/check_landing.go`: the on-base count.
