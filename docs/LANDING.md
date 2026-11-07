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

## The escalation ladder

Glenn, 2026-11-06: "My only hope is that you intelligently do merges, and
don't fall back to merging only one card at a time to main, and chasing a
moving target. If you are progressing well, and cards come in one at a time,
obviously, you really should just merge those one at a time. if you get
backed up, you should look across work streams and merge that work stream
into its own branch, then merge the result into main as a group. If you
can't do a merge in a work stream this way, you have to escalate further,
and look wholistically across work streams and ask, why do I have conflicts
between two work streams? and then fix. this is a job for the coordinator.
my point is that merging is complex and you need to build in an escalation
policy all the way to the coordinator goes into a frontier model and merges
manually because it's complex and it needs judgement."

So landing is a ladder of four rungs, and each stream is on one rung at a
time. Which rung a stream is on is policy's choice from its backlog, by a
number policy states for each trigger; each rung's moves and refusals are
the engine's; the merge table shows the rung of every stream.

1. **Flowing.** Cards arrive one at a time and the stream is not backed up:
   each producer that reaches `merging` lands into base on its own, as it
   comes, with its CI, and nothing chases the base tip (step 6).
2. **Backed up.** Past policy's backlog number, the stream's merging
   producers are merged in score order onto the stream branch and land into
   base as one group: one PR, one CI run for the group.
3. **The stream cannot merge.** A stream group that cannot be merged (a
   conflict across streams, a group that stays red after bisection, step 7)
   is a judgement to the coordinator, who looks across the streams for why
   two streams conflict and fixes the cause.
4. **By hand.** When the coordinator's fix is itself a merge that needs
   judgement, the coordinator, on a frontier model, merges by hand, and the
   result lands through the same CI gate as any group (step 6).

This replaces v1's "never land a card alone" (PRIORITY M4, R15, Glenn
2026-09-25 "ALWAYS BATCH") for a stream that is flowing; the batch is now the
answer to a backlog, rung 2.

## The design

1. **Each stream has an integration branch** (`sprint/<stream>/group` by default (never `sprint/<stream>` itself: git refuses a ref that is both a file and a directory of the card branches `sprint/<stream>/<producer>`; found by Emma 2026-11-06),
   set at `stream add`). Cards branch from it, not from base, and the base
   commit is recorded on the consumer at deal (`base_sha`). How the stream's
   producers reach base is its rung on the ladder above; a group is one PR
   with one CI run, so on rung 2 the merge cost is per group.
2. **Conflicts are prevented, not resolved.** Every card declares its paths
   (the card lint refuses a brief without them). Two live cards in one stream
   never overlap in paths: the deal refuses the second until the first has
   landed, naming the overlap. So merges inside a stream cannot conflict.
   Streams land into base one at a time in their order (`pos`), and a
   cross-stream conflict is a judgement to the coordinator (rung 3), never a
   silent rework. Ledger files (fixes and roadmap data, TLA run tables,
   base-red tables, generated pages; the list is policy's, per repository)
   are not counted as an overlap: the lander merges them entry by entry, or
   regenerates them, and a ledger conflict it cannot resolve stops the
   stream with a judgement (LESSONS 3.11; PRIORITY M19).
3. **The runner rebases at report.** When a consumer's `base_sha` is behind
   the stream head at report time, `nova-worker` rebases mechanically onto
   the stream head as it is then, re-runs the card's tests, pushes, and
   reports. A rebase that conflicts is a `fix` result with the files named,
   early, while the author still has the context. A friend row is asked the
   same by the report refusal (step 4). The goal is to merge efficiently:
   after the report, the stream moving on is the landing's to absorb (step 6),
   never the card's to chase, as v1 did on 2026-10-20 (Glenn: "this has
   always been the rule. you guys just forget it over and over"); how many
   rebases that takes is the implementation's, not this design's (LESSONS
   3.14; ruled by Rowan 2026-11-06, corrected by Glenn: "work efficiently
   when merging").
4. **A report of ok is refused until the head is landable.** The head must
   be rebased at report (step 3), pushed, and have green CI at that exact
   head (CI runs on the card branch by the same push trigger as the group
   branch, below); no card is asked for a PR (defect 34, below). Each
   refusal names which and the command. A report is never refused because the stream head moved after
   it (LESSONS 3.14). So every producer in review is landable; the read is
   the last gate.
5. **In-order landing, visible when stuck.** Within a stream, producers land
   in score order (work order; Glenn 2026-09-29: "merge in work order, not
   random"). A producer ahead that is not landable holds the ones behind it;
   that is what in order means, and the merge table shows it per stream:
   queued, merged, stuck (and why: the producer and its state), CI, state,
   and the stream's rung. A
   stuck batch is a judgement with its verb (`rescore`, `drop`, `rework`).
   v1's merging was invisible ("You would be stuck and not know it").
   The soft `after` list orders landings across streams the same way, and
   only among cards both queued to land: an `after` whose target is not
   itself queued (in `merging`, or ahead in the same batch) is ignored for
   this landing and never holds it (LESSONS 2.15; PRIORITY M20; ruled by
   Rowan 2026-11-06).
6. **Base is never red from a landing.** A batch lands only on CI green
   against the exact base head it lands on, re-run if base moved
   (2026-10-27: stale-green merges put main red on 12 jobs). The batch's
   CI is one run; its result is a notification either way. A base that
   moved is met by re-merging the batch onto the new tip and gating the
   combined tree once, never by sending its cards back to rebase one at a
   time; a re-merge is needed only when the merge is refused for a conflict
   (LESSONS 3.14; PRIORITY M13).
7. **Batch atomic, red batches bisected.** A batch lands whole or not at
   all. A red batch is bisected: its halves are gated until the red head is
   found, that producer goes to fix (`ready`) with the failing checks and
   their URLs as its finding (intent rule 30, below), and the rest land as a
   smaller batch. A gate that did not run (a bench fault) blames no
   head: the batch is retried and nothing returns (LESSONS 3.13; PRIORITY
   M7).
8. **A red base has one judgement and a path back.** When base itself is red
   (not from a landing), there is one judgement for the base, not one per
   stream: a queued head that passes the gate and fixes the red lands first;
   base is re-gated (v1: after 2 and 5 minutes); the third failure stops the
   stream that met it; other streams that meet the same red are refused
   under that one judgement and not stopped; and when the tip is green,
   every stream stopped only on that red resumes by itself. Base is never
   fast-forwarded by hand (LESSONS 3.12; PRIORITY M18).
9. **Landing runs on its own.** The landing step runs in the run loop
   whether or not the coordinator is hooked or attending (LESSONS 3.15). A
   hold is a deal control: it stops deals to a row and never removes a host
   from the landing gate, so no hold, of one row or of every row, can stop
   landing (LESSONS 3.16; PRIORITY W10).
10. **A stalled landing is pushed.** When nothing has landed within a bound
    policy states (v1: one minute) while producers wait in `review` or
    `merging`, the coordinator is pushed once with the cause per stream, and
    the episode clears at the next landing (LESSONS 3.19).

## What is modelled

`tla/Landing.tla` (layer 3 and 4): a card lands at most once; landings in a
stream are in score order; a batch is all or none; the base is never advanced
by a batch whose CI was not green at the exact base head; a producer in
`merging` has a head equal to its last read's head; a soft `after` whose
target is not queued never holds a landing (LESSONS 2.15); a bench fault
returns no producer (LESSONS 3.13); every stream stopped only on a red base
resumes when the base is green (LESSONS 3.12); a hold never disables the
landing gate (LESSONS 3.16); and, as liveness, a producer in `merging`
lands, returns, or is under a judgement (LESSONS 2.21). Each rung of the
ladder is an action, its trigger a constant. Reversed witnesses: a
stale-green landing; a card landing twice after an unknown outcome; a batch
half-landed; a landing out of order; an `after` on an unqueued card freezing
a stream; a bench fault blaming a head; a red base with no path back.

## Where it lives

- The refusals of step 4 are the consumer `Report` of layer 2 plus the
  checks layer 4's git and GitHub edge runs for it.
- The path-overlap refusal is a deal condition in layer 3, reading paths from
  the card.
- The batch and the merge table are layer 3's landing step and layer 6's
  view.

## Added from the promise reviews (2026-11-06)

- After any landing into base, the stream branch is fast-forwarded to base
  in the same step, so cards cut afterwards start from what landed and the
  stream branch never drifts behind base (Fable, promise 4; v1: 105 conflicts).
- The engine learns a CI result from the run loop reading the check status of
  the exact head (layer 4's GitHub edge, a machine reading an API, which is
  not a model polling) and recording it as evidence on the consumer; a red is
  a judgement, a green at the landable head is what step 4 requires.
- "A card that wants a PR" was one whose stream's repo requires PRs; since
  2026-11-07 no card is asked for one (defect 34, below).

## CI on the group branch (2026-11-07, defects 38 and 40)

The first landing merged a head into `sprint/nova-tools/group` while the
head's own checks were red, and then waited for ever: nova-tools' workflow
ran on push to main, pull_request, merge_group and the nightly, so nothing
ever ran on the group branch, and the landing's gate (CI green on the exact
merged sha before base moves) had no verdict to read. Merging the head into
the group first is the design (the group's CI blames the head); the defect
was that no CI could reach the group.

**Decision: the stream's repository runs CI on push to `sprint/**`.** The
landing pushes the merged group sha to `sprint/<stream>/group` and reads the
check rollup of that exact sha; a green one is the sha that fast-forwards
base (intent rule 16), a red one is blamed on a head (bisected by pushing
subsets to the same branch, step 7). The merge queue was the other choice
and is not taken: it tests a merge sha GitHub makes, not the one nova-sprint
pushed, so "the exact sha CI passed is what lands" would be GitHub's sha and
not ours; it cannot be bisected by the landing; and it ties the landing to
one forge's feature. The same trigger runs CI on every card branch
(`sprint/<stream>/<producer>`), which step 4 needs now that cards open no PR.

What a stream's repository must have, for a stream with a forge:

- a workflow whose `on:` includes `push: branches: ['sprint/**']` and that
  reports its jobs as checks on the pushed commit (any job names; every
  check on the sha counts, and one failure is red);
- base's protection lets the sprint's identity (below) fast-forward base to
  a commit whose required checks passed: required status checks are met by
  the group sha's own checks; a rule that requires a merge queue or a PR
  merge refuses the landing's fast-forward unless the sprint's identity is on
  its bypass list.

What the landing waits for, and for how long: after it pushes the group sha,
a pending CI (jobs queued or running are checks already) is waited for as
long as it runs (the stall judgement, step 10, covers a slow one). A sha with
no check at all is waited for `CIAbsentWait` (10 minutes, a guess, not
measured: GitHub creates a queued job's check within seconds of a push), and
then the landing stops: nothing lands, nothing is blamed, and layer 3 opens
one `no-ci` judgement naming `sprint/<stream>/group`, the sha and the fix
(add the trigger, then re-run CI on that sha or answer retry). The request
is asked again on later ticks under that one judgement; a re-push of the
same merged sha starts no new CI, so the fix re-runs CI on it.

How it is verified: at run time, by that bounded wait and its judgement,
within ten minutes of a stream's first landing. Not verified yet at
`stream add`: reading a repository's workflow files for a push trigger is a
heuristic (a reusable workflow, another CI system), so it is left to `check`,
which shows every open judgement by kind (directive 59); a `stream add`
warning from the workflow files is owed.

## The sprint's identity (2026-11-07, defect 39)

The first landing's merge author was the human's own GitHub login: run's git
pushed the group branch with the host's own credential helper (GitHub marks
a PR merged, by the pusher, when its head reaches its base by a push), and
its forge would have used whatever gh was logged in as. Now:

- The forge acts only with the token in `GH_GATE_TOKEN`, the control seat's
  secret of that name, handed to `nova-sprint run` by
  `nova-secrets exec --only ...,GH_GATE_TOKEN` (the sprint-run service's
  unit lists the key). Every `gh` call gets `GH_TOKEN` set to it in a clean
  environment; an inherited `GH_TOKEN`, `GITHUB_TOKEN` or `GH_CONFIG_DIR`
  never reaches gh. With no token the forge refuses, naming the fix, and
  never falls back to the host's gh login; `NOVA_SPRINT_FORGE_LOGIN`, when
  set, is checked against `gh auth status`, and the human's own login is
  always refused.
- git, given the same token, resets the host's credential helpers and
  answers every https credential request with it (in the environment, never
  argv), so the landing's pushes are the token's identity. This holds for an
  https remote; an ssh remote would push with the host's ssh key.
- A landing whose forge has no token merges and pushes nothing: it answers
  `forge`, and layer 3 opens one `forge` judgement with the refusal's fix.

The token needs, on each stream repository: contents write (the group
branch, base's fast-forward), pull requests write (the group PR), and checks
read. A token that cannot create a pull request is refused at the landing's
OpenPR with GitHub's own error, as a fault.

## PRs and the group branch (2026-11-07, defect 34)

`finish` no longer asks a card for a PR at its head, and `take` no longer
tells the worker to open one. The landing never used the card's PR: it
merges the card heads into the group branch by git, opens (or finds) the one
PR from `sprint/<stream>/group` into base itself, as the sprint, and lands
by fast-forwarding base to the gated sha. The group branch needs no setup:
the landing's first push creates it, and `finish` checks a head contains the
group's tip only when the group branch exists. So a worker's seat needs no
token that can open PRs; only the control seat's does.

## A red landing goes to fix (2026-11-07, intent rule 30)

A red gate moves the blamed producer from `merging` to fix (`ready`;
`waiting` in a held stream) with the finding `CI red at <head> on <base>:
failing: <check names>; <first failing check's URL>; <reason>; gated <sha>`,
which the fix attempt's work consumer carries from `Start`. It used to go to
`review`, where a reader re-read the same red head. The coordinator can
still `WaiveRed` a red that was not the code's while the producer waits in
fix (before the next `Start`).
