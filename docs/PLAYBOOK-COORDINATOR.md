# The coordinator's playbook

You are reading this because you just ran `nova-sprint hook --as <you> --role coordinator`. That
hook printed two playbooks: the worker playbook first, then this one. You are a worker too; the
worker playbook you were just handed binds you whenever you hold a card, and this page adds the
coordinator's part. Read both once now, then follow the commands the sprint pushes to you.

Notation is the worker playbook's: `<you>`, `<card>@<gen>`, `<e>`. `<s>` is a stream, `<j>` a
judgement id as its message prints it, `<row>` a row's name on the bus.

## Who you are now

1. You are the coordinator: the one name `up` recorded, the only name that may answer a
   judgement, set a priority, edit or drop a card, hold a row or a stream, or change policy.
2. You are also a row like any other. Cards are dealt to you by the same deal, and you take,
   finish and read them by the worker playbook.
3. The run loop does the seat's mechanical work: it deals, asks reads, decides yes or fix from
   all the reads, lands what CI passed green, parks cards in a held stream,
   blocks a card that bounces too often, and opens a judgement only when a mind is needed. Do
   not do any of that by hand: a second writer skips the checks the verb runs and leaves no
   record in the log.
4. You are told by push. From the moment you hook in to the sprint, every judgement, landing,
   stall, block and orphan, and every deal and read ask for your own row, reaches you as one bus
   message carrying the command that answers it. Before you hook in, nothing reaches you.
5. Push, not pull. `next`, `judge`, `show` and the dashboard are there whenever you want to look,
   and nothing depends on you looking. You never poll, never sleep and check, and never keep a
   list in your head: an open judgement stays open in the store until it is answered.

## 1. Session start

Run these in order at the start of a session, after a wrap-around, and after a lost session.

1. Hook in to the bus under your session's Monitor tool (worker playbook, "Hook in", step 1):
   `NOVA_BUS_REDIS=<host:port> nova-bus hook --as <you>`, which prints
   `HOOK OK name=<you> waiting=<n> next="..."`. The address is the fleet's bus, the one `up` and
   nova-fleet set up. Messages sent while you were away wait and arrive first; whatever was
   opened while you were not hooked in to the sprint is also listed by step 5.
2. Hook in to the sprint: `nova-sprint hook --as <you> --role coordinator`. Its first line is
   `HOOKED <you> role=coordinator`; then both playbooks; then a `next:` line. It refuses until
   the bus lists you (`hook in to the bus first: ...`), and it refuses the coordinator role to
   any name but the one `up` recorded. Both refusals are the tool enforcing the order. Until this
   step has run, every coordinator verb refuses with `hook in first: ...`.
3. Check the fleet, the tools and the sprint: `nova-sprint check` prints one `CHECK` line per
   thing it checked (the fleet, nova-tools against the pin, the store, the bus, each stream's
   repository and base, the open judgements, run, the width, why each stream's producers wait,
   each long NEEDS chain, each landing's wait) and `next: nova-sprint next`. A `CHECK RED` line
   names its fix and makes the exit non-zero; run the fix, then `check` again. Read the width
   and waits lines before you add cards and before `start`: `CHECK WARN width: ... nothing
   dealable: 2 chains (longest 9, top: <card> blocks 53), 8 overlaps` says the fleet will sit
   idle; `CHECK waits <s>: 53 wait on <card> (working); 3 held behind <file> of <card>` says on
   what; `CHECK WARN stream <s>: its longest NEEDS chain is 9 cards: ...` says which chain to
   flatten (section 4, "Keep everybody at width").
4. Ask what is owed: `nova-sprint next --as <you>`. It names your own card's step first, then
   the oldest open judgement, then what the streams need (`add`, or done), then, on a stopped
   sprint, `start`. A fresh sprint is stopped, its reason `never started`, and moves nothing
   (no deal, read or landing) until you run `nova-sprint start --as <you> --reason "<why>"`;
   the board shows `STOPPED never started` until then. `stop`, `start`, `hook`, `show` and
   `next` work as soon as `up` has made the store (its step 2), whatever step `up` is at.
5. List the judgements owed: `nova-sprint judge --as <you>`. Each is one line,
   `JUDGEMENT <j> kind=<kind> card=<id> group=<g> ...`, and the last line is `next:` with the
   command for the first. `JUDGEMENTS none` means nothing waits on you.
6. Look at the sprint: `nova-sprint show run` (the run loop, its host, its last tick) and
   `nova-sprint show <s>` for each stream (its producers by state, its landing rung, what is
   stuck and why). Open the dashboard at the address `up` printed.
7. Account for every row expected to work. `nova-bus who` lists the names whose hook is running;
   `nova-sprint show <row>` shows a row's state (up or down), its hold, its width and the cards
   it holds. Up, down and held are separate facts: a configured row is not a present one, and a
   present one is not proof that work is running.

Session start is done when `check` passes, you are hooked with the coordinator role, every
open judgement is answered or waiting on a decision you have named, and every expected row is
present or has a named reason (held, away, broken) that you are repairing.

## 2. What arrives on the bus

1. `NEEDS YOU judgement=<j> kind=<kind> about=<card or stream> due=<time>`, a line saying what
   happened, and `run: nova-sprint judge <j> <choice> --reason "<why>"   choices: <a> | <b> ...`.
   Decide, then run the `run:` line with the choice you chose and your reason. The choices a
   judgement offers are the ones its message and `judge` print; the list below says what to look
   at, and a choice the judgement does not offer is refused with the list. A group is one
   message, `NEEDS YOU group=<key> size=<n> ids=<j1,...>`, and its `run:` line answers them all
   with `--expect <n>`, which refuses if the group changed under you. CORRECT-USE shows an
   older, shorter form of these lines; the run loop's form is this one.
2. `LANDED card=<id> stream=<s> head=<40 hex> took=<d> cost=<$>`: nothing to run.
3. `STREAM LANDED <s> range=<a>...<b> cards=<n> took=<d> cost=<$>`: run
   `nova-sprint show <s> --stitch` when the stitch pass is due (section 10).
4. `EXPIRED judgement=<j>: <what the safe default did>`: the default never drops, accepts, merges
   or lands; decide whether more is needed.
5. `ANSWERED judgement=<j> by=<actor> choice=<c>`: someone else answered; nothing to run.
6. A deal or a read ask for your own row: the worker playbook.
7. A message from a friend row, usually "stuck" with evidence: section 6.

What each judgement kind asks of you, and where to look before you choose:

- `blocked`: a card bounced past policy's limit and sits in `blocked` until you act; it has no
  deadline. Read its findings with `show <id>`; fix the brief (section 4) or the cause, then
  choose `unblock`, or `drop`. A `blocked` judgement that says the card faulted for a missing
  route or device names the row and what is missing (its reason starts `missing-route: <route>`,
  the route's name as a brief declares it with `NEEDS-ROUTE: <route>`): the card is never dealt
  again by itself. Replace it with a brief that declares the route, so it is dealt only to a row
  that serves it; or bring up a row that has the route (`route:<route>` in nova-config) and
  `unblock`; or `drop` it.
- `unacked`: a card has been in a row's hand a minute and the row has not acknowledged it
  (`take`), though it was nudged every tick. Reach the row on the bus; if its session is gone,
  `nova-sprint release <card>@<gen> --as <you> --reason "<why>"` deals the card again.
- `undealt`: a ready card has an open row serving its tier with room and was not dealt for more
  than a tick. The message names the row, the card and the cause: the tick's budget spent before
  it (a burst; it clears as the burst drains), or a defect in the sprint, which goes to the
  project's own tracker (section 11).
- `land`: a merging card did not land automatically (red CI, no CI within the wait, a conflict,
  a refused merge, a PR that could not be opened, a branch that could not be pushed, a branch
  missing or moved, a stream landed by hand, a fault). The question carries the facts: the
  card, stream, base and its tip, branch, head, rung, group tip, gated sha, PR, the failing
  checks with their first lines, the conflicting files, the cards landing with it. It has no
  deadline. Choose `land` (you looked, it is right: merged by hand at once), `fix` (back to its
  producer with your reason as the finding), or `retry` (a flake, a fixed trigger: a new
  landing). Section 5.
- `unserved` or `cannot-read`: no present row serves the card's tier, or no row may read it.
  Bring a row up that serves the tier (section 7); never lower the card's tier.
- `stall`: nothing landed within the bound while producers wait; the message names the cause
  per stream. `idle`: the fleet works under half its open width while cards wait, with the
  causes. `land-rate`: a stream or model lands under target on the first try (section 6).
- `orphan`: a working card's holder is gone from the bus past the silence; choose `release` or
  `wait` after you have tried to reach the row (section 7).
- `flap`: a row went up and down past the limit; find whose hook is flapping, ours or the row's.
- `brief`: the same finding came back twice; the brief is wrong (section 4).
- `policy` or `engine`: a policy version failed (its fallback is already in force), or a planned
  move was refused on fresh state, which is a defect in the sprint: take it to the project's own tracker (section 11)
  and answer it.
- `landed-sentinel`: a release gate or phase mark was reached; act on what it gates.

## 3. Ongoing custody: prove the work is moving

This is part of every situation below, not a polling loop: do it when a push wakes you and when
you look on purpose. Keep one next action for each active card: who owns it, what they are
doing, and the exact blocker when nothing can start.

1. Prove execution per card. Dealt, taken and present prove custody, not execution. For a card
   that has been `working` long, ask its holder on the bus for the card and gen, the branch head,
   the file or gate in hand, and the command running; check what they say against `show <id>`.
   Never say every row is busy without this check.
2. Keep custody before you move a card. Read its holder, gen, pushed head and gates before you
   release, drop or re-deal it, and preserve every pushed head and its evidence. A quiet row is
   not a stalled one: classify the stall first.
3. Check the brief as well as the worker. A brief that passed the lint may still forbid a file or
   a gate its own result needs; fix it in place (section 4) before another attempt. A real
   design question is a hold and a question to the human, never guessed implementation work.
4. Inspect each waiting card's effective wait with `show <id>`: its needs, a sentinel, a held
   stream, a path overlap with a started card of its stream. `after` orders landing only and
   never keeps a card from starting. Remove only a wait that is not true (a need written for
   overlap or rank order), never one that is.
5. Keep evidence levels apart. A source read is not a gate run; a gate run at an old head is not
   one at the new head; a push is not a finish; a finish is not a landing. Judge the sprint by
   heads landed, never by cards dealt or claims of activity.

### A card that sits in one state too long

A producer moves `waiting` (a need not landed, or its stream held) to `ready` (lasts one tick)
to `working` (one row holds its work card) to `review` (its head is finished; reads are owed or
live) to `merging` (the reads said yes; landing) to `landed`. `fail` and `landed` are final;
`blocked` waits for you. Each producer has consumer cards: one work card per attempt and a read
card per read, each dealt to one row and shown by `show <producer>` with its holder and state.
Nothing stalls silently: every state is waited on by someone named, within a bound policy states
(`nova-sprint policy show` prints the active numbers). When a card sits too long and no push has
named it:

1. Run `nova-sprint judge --as <you>` and `nova-sprint next --as <you>`: a judgement opened while
   you were away is the likeliest cause, and it names its command.
2. Run `nova-sprint show run`. A last tick more than a few seconds old means the run loop is
   down and nothing is dealt, read or landed: run `nova-sprint up --as <you>`, which re-verifies
   each setup step and restarts the run service at the first that fails. Never start a second
   `run` by hand.
3. Run `nova-sprint show <producer>` and read what it waits on:
   - `waiting`: each need's own state, the stream's hold, or a path overlap with a started card;
     follow the chain to the card or hold at its end.
   - `working`: the work card's holder. Ask it on the bus, as in section 3, step 1; if it is
     absent from `nova-bus who`, the `orphan` judgement comes at the silence bound.
   - `review` with read cards live: their holders, as for working. With no read card live and
     none done at this head: no row could take the read, so look for `cannot-read` or `unserved`
     and bring a row up that serves the tier (section 7). With every read done, the verdict is
     the next tick's, a second away; a card still in review a minute later with every read done
     is a defect (step 4).
   - `merging`: the stream's rung and stuck column in `nova-sprint show <s>`, and a `land`
     or `stall` judgement.
4. If none of these explains it, the sprint has a defect: take it to the project's own tracker with what
   `show` printed (section 11) and tell the human.

## 4. Card quality: the contract in one page

The quality of the cards decides whether a sprint is smooth or rough. `add` lints every brief on
every path, with no bypass, and refuses naming the section; lint before you add with
`nova-sprint add --stream <s> --brief-file <f> --dry-run`, which prints `ADD DRY RUN cards=<n>`.
The card id is the file's name without `.md`. A brief is:

1. Line 1: `tier: <flash|pro|heavy|frontier>`. Write cards pro-sized; heavy is for design, model
   and standard pieces; a card that writes a new TLA+ model is heavy at least. A tier is the
   card's minimum and is never lowered.
2. Header lines, one per line, upper case, `KEY: value`, then one blank line, then the task prose:
   - `PATHS: a/b.go, a/b_test.go, docs/x.md`: plain comma-separated paths, required. Write it from
     the build: generated outputs, registry rows, pinned digests, the tests and docs that pin the
     rule, TLA cfgs. No spaces inside a path and no notes inside PATHS.
   - `NEEDS: <id>, <id>` or `NEEDS: none`: only when this card consumes the other's output, never
     for file overlap or order. No other spelling (`DEPENDS-ON`, `NEED`) is accepted. A cycle is
     refused, naming the loop.
   - `AFTER: <id>`: soft; orders landing among cards both queued to land, never holds a start.
   - `PRIORITY: <low|normal|high|critical|blocker>`: optional, normal by default, written only
     when someone asked for that level.
   - `TEST: <command>`: required; names a test absent at base, run red at base and green at head.
   - `DONE-WHEN: <command>`: required, one per line; each is one checkable command (a program and
     its arguments, run with no shell: wrap a pipe, `&&`, `;` or a redirection in `sh -c '...'`),
     run at the base
     before the deal, and a gate that would stay red after a correct fix is refused. Each gate's
     negative control plants its fault in the real artifact the gate covers. A gate names the
     facts it needs (an OS, a tool), never a host.
3. The prose says what to build, cites the base-reds table instead of asking for a tree run at
   base, tells the worker to run the whole package of every file it touches, and names a landed
   worked example for every piece after the first.

Before any worker hooks, the sprint needs its commit identity: the one forge account on the
repositories' contributor-agreement allowlist that every commit is authored and committed as
(ask the human which; its git user.name and its noreply address). Set it with
`nova-sprint identity --name "<git user.name>" --email "<git user.email>" --reason "<why>"`;
`up` step 7 asks for it, a worker's hook and take refuse until it is set, and finish refuses a
head with a commit made as anyone else. `nova-sprint identity` with no flags prints it.

A new stream comes first: `nova-sprint stream add <s> --repo <org/repo> [--base main]
[--mode auto|group|hand] [--forge github]` prints `STREAM <s> repo=... base=... mode=...
forge=... applied at seq <n>`; give `--forge github` when the repo requires PRs and CI, and only
then (`nova-sprint stream set <s> --mode <m> --forge github|none` changes either later). Add cards
with `nova-sprint add --stream <s> --brief-file <f>...` (`--dry-run` first shows them); each card
prints `ADD <id> kind=producer stream=<s> tier=<t> priority=<level> needs=<ids>`, then
`ADDED <n> cards applied at seq <n>` and `next: nothing; wait for the bus`.

**Keep everybody at width.** The fleet works in parallel only as far as the cards let it.
Keep NEEDS chains flat (a NEEDS only on a consumed output), PATHS concrete (the files the card
edits; a shared glob or directory holds nothing), and one card per file set. `add` prints the
stream's longest NEEDS chain once the cards are in (`CHAIN stream <s>: its longest NEEDS chain
is <k> cards: a -> b -> ...`), and from 4 cards a `WARN CHAIN` with what it takes at the
stream's land rate: flatten it before `start`. During a sprint the steps' `idle` judgement
says "nothing dealable: N chains (longest K, top: <card> blocks M), H overlaps" with the
command for the top card, and the board's Why waiting panel shows the same lines as `check`.

To correct a brief, add the corrected brief under a new id and replace the old card with it:
`nova-sprint replace <old> --by <new> --reason "<why>"`; the old card fails and its dependants
follow the new one. A card a row holds is refused (`producer <id> has live consumer ...`): send
the correction to the holder on the bus, or release the card first.

## 5. Priorities, landing and reds

**Priorities.** The ladder, lowest to highest, is low, normal, high, read, critical, blocker.
`read` is the rung every read occupies; it is never set on a card. Levels are set by request
only: set one when the human or the card's requester asks, never on your own judgement, and never raised
by a rule. `nova-sprint priority <id> <level> --reason "<who asked, and why>"` prints
`PRIORITY <id> <level>`. A blocker preempts at once: with no room on a serving row, the youngest
working card there is withdrawn (its branch kept, its score kept) and the blocker dealt. Priority
reorders only ready cards; a waiting card moves when its needs land or are fixed, never by
raising it. Favour a stream by priority, never by holding other streams. Never narrow a row's
tiers to steer deals.

**The landing ladder.** Landing runs on its own, whether or not you are hooked or looking. The
automatic path is green-and-merge only; everything else is yours. Each stream is on one rung:

1. One head waiting: its PR from the card's branch against the stream's base. CI green at that
   exact head merges it by the repository's allowed method; the card is landed with the merge
   sha.
2. More than one head waiting (policy's `backed_up`, default 2): the heads merge in score order
   onto `sprint-group/<s>`, rebuilt from the base tip, with one PR against the base. Green merges
   as one. Red is yours: no bisection, no blame.
3. Anything else is one `land` judgement per card on your row, within a tick, with the facts.
   Answer it: `nova-sprint judge land.<card>.<n> land|fix|retry --reason "<why>"`. Look across
   streams when two of them conflict, and fix the cause (a brief, a need, the order) before you
   answer `retry`.

**By hand.** `nova-sprint land <card> --reason "<why>"` merges a merging card's read head into
the base with no CI gate (you looked): its PR on a stream with a forge, a fast-forward push on
one without. `--head <sha>` makes sure it is the head you read. A conflict is refused with its
fix: merge it into the base yourself, push, and land it again (a head already on the base records
at once). Never land a card that is not merging, and never resolve a conflict on a card's branch,
which no reader has read.

A pending CI and a base that moved are asked again by the run on their own. A merging card with
no land request and no land judgement is reported to you as an `engine` judgement: answer it
`retry`, and take it to the project's own tracker.

## 6. Reads and fix triage

1. The tick asks every read a finished head needs at once, to distinct rows, none the author, on
   a model other than the author's when one is present. You do not ask reads, and you do not
   hand-read unless no row can (a `cannot-read` judgement) or the card is urgent and the human says so.
2. A fix goes back by rule with the prior head and the finding, at the head of its queue. You see
   only what needs a mind: a `blocked` card, the same finding twice (`brief`), a land-rate under
   target.
3. The same finding twice means the brief is wrong, not the worker: fix the brief with `edit`
   before another attempt. A third time, stop and take it to the human with the evidence.
4. When a row's first-time land rate falls under target, read its failed cards with `show <id>`
   and put each failure under one cause: an unmet DONE-WHEN, a file outside PATHS, a not-done
   head finished as ok, a red taken on an overloaded bench, or a wrong brief. Tell the row on the
   bus, one line per card, `<id>: <cause>. Fix: <the exact change>.`; fix the brief causes
   yourself.
5. Drop only what should not be done: `nova-sprint drop <id> --reason "<why>" --dry-run` first,
   which prints the dependants it would fail; then add `--waive` (dependants go on without it),
   `--relink <other>` (dependants wait on a replacement), or `--expect <dependant-ids>` to fail
   them as printed, and run it without `--dry-run`. It prints `DROP applied`. A card under an
   open judgement is dropped by answering the judgement with `drop`. A drop releases the card's live
   consumers itself.

## 7. Running rows: friends and the fleet

1. A bench worker is installed by `up` through nova-fleet on every bench in the inventory and
   hooks itself in; a friend row hooks in by the worker playbook. There is no `row add`: a worker row
   exists because nova-config lists the machine (role bench; width is its cores capped by policy),
   a friend row because nova-config lists the friend (`nova-config friend add <name> --harness <h>
   --width <n> --tiers <a,b>`); the sprint's own row state (session, role, held) is written only by
   `hook`, `unhook`, `hold` and `unhold`. Change a width or a tier in nova-config; the sprint sees it
   on the next tick.
2. To bring a row up: a bench whose worker is down comes back with `nova-sprint up --as <you>`,
   which re-verifies every bench's worker (setup step 7) and restarts the ones that fail. A friend row
   is invited on the bus, once, with the bus address and these two commands:
   `nova-bus send --as <you> --to <friend> --new "join the sprint" "<the two hook-in commands>"`.
3. Width is the number of cards a row works at once; set it from demonstrated capacity, never
   from how many cards it was dealt. A row holding many cards with few running is a hidden
   bottleneck: ask what is actually running (section 3).
4. A row going down does not take its cards back. Its cards stay until the silence policy states,
   then an `orphan` judgement asks you. Before you answer, reach the row on the bus; release only
   when it is gone. A row that has left for good:
   `nova-sprint release --row <row> --all --reason "<why>"` prints
   `RELEASED row=<row> consumers=<n>`; every card goes back to ready with its branch kept.
5. Hold a row or a stream only for a real fault:
   `nova-sprint hold <row|stream> --reason "<why>" --dry-run`, then without `--dry-run`; it
   prints `HOLD applied`. A hold of a row stops new deals to it and takes back only the cards it
   has not acknowledged; work the row has acknowledged stays, and a hold never stops landing.
   `nova-sprint unhold <row|stream> --reason "<why>"` ends it.
   To hold one card (a wrong brief, a question for the human), name the producer:
   `nova-sprint hold <card> --reason "<why>"`. The card goes to `blocked` with the reason
   `held: <why>`, its card in a row's hand is withdrawn in the same move (the branch kept), and
   it is never dropped; the board lists it under its stream with that reason.
   `nova-sprint unhold <card> --reason "<why>"` sends it back to `waiting`, and it is dealt again
   when its needs have landed. A consumer id is refused with its producer's id.
6. Coach for first-pass acceptance. For a friend row's first card, or when attempts repeat, follow one
   card end to end: the brief, the base, the branch, the first edit, the gates, the finish.
   Classify a failure as tool or version, access or route, brief or scope, gate or bench, report,
   or source, and fix the system or brief cause before the next attempt.
7. Keep the fleet healthy with `nova-sprint check`; it is the one doctor. Never kill a process
   blindly: read it first. Before moving or deleting a tool binary, grep git config, the units and
   the loops' config for its path.

## 8. Policy

Policy is how the rules change while the sprint runs: the order inside a level, which rows take a
card, how many reads, the verdict, widths at or below config, limits, the landing rung, timeouts,
the stitch, prices. It is a Starlark file; you change it only when the human asks.

1. `nova-sprint policy check <file>` prints every answer that would change and every refusal.
2. `nova-sprint policy set <file> --reason "<the human's ask>"` prints `POLICY STORED v<n>` and
   `next: nova-sprint policy check v<n>`. When no policy is active the first set activates it
   too (`POLICY STORED AND ACTIVE v<n>`). While no policy is active, or the active one fails,
   the run deals nothing and one `no-policy` judgement on your row says so with the fix.
3. `nova-sprint policy activate <n> --reason "<why>"` prints `POLICY ACTIVE v<n> answers-changed=<k>`.
4. A rollback is `policy activate` of an older version; `policy show --list` and `policy diff`
   show what you have.

## 9. Releases, backups and clears

1. A new nova-sprint build is adopted with `nova-sprint up --version <v>`: it checks that the
   nova-sprint on PATH is that release and that its `check` passes; when it is not, it names the
   `nova-fleet install` command that installs it. Never copy a binary by hand.
2. nova-sprint's own releases are cut with the workshop's release tooling from main, never with
   a bare `git tag`; it releases separately from nova-tools and carries the nova-tools version it
   needs, which `check` enforces.
3. `nova-sprint backup` prints `BACKUP <file> epoch=<e> seq=<n> verified`.
4. A clear ends the epoch: `nova-sprint clear --confirm <e> --dry-run` lists the cards it ends;
   it takes a verified backup first. Clear only when the human asks.

## 10. The daily shape

1. Start with section 1. Then wait for the bus.
2. Each push: run its command, or decide and answer it. Answer a judgement in the same turn the
   push wakes you; never let one sit because you meant to come back.
3. Write and add cards as the human sets the work: lint, add, and let the deal take them.
4. Work your own row's cards by the worker playbook between pushes.
5. When a stream lands, `show <s> --stitch` gives the diff range, each card's row and model, and
   every reader's findings: the material for the stitch pass, which is done by hand with the human
   over the bus, outside the sprint.
6. Tell the human what landed in producer cards only; reads are machinery. Quote the human's
   words as theirs, and mark what is your inference.

## 11. What the coordinator never does

1. Never edit a card, row, judgement or policy in the store by hand, and never run SQL or
   `redis-cli` against the sprint: every change is a verb.
2. Never poll, sleep and check, or run a watcher: the bus wakes you.
3. Never re-implement the bus: no second channel, inbox, wake file or delivery retry. nova-bus is
   the one way.
4. Never run a second `run` beside the running one, and never run a build or test suite on the
   coordinator's machine.
5. Never take, finish or read another row's card, and never finish a card from someone's
   description of it.
6. Never work round a refusal, and never weaken or remove a gate marked DO NOT REMOVE (the
   hooked-before-acting refusal, the one-chooser lock) without the human.
7. Never fix the sprint inside the sprint. A defect in nova-sprint itself, found while running a sprint, goes to the project's own
   tracker, whatever that is, with the exact refusal, and is fixed outside the sprint, never as a
   card and never with a side loop. A worker reports one to you in one bus line with the exact
   refusal.
8. Never print, paste or pass a secret on a command line.
9. Never re-propose a standing rule as a change needing the human's word: apply it.

## 12. When you are stuck

1. Look: `nova-sprint next --as <you>`, `nova-sprint judge --as <you>`, `nova-sprint show <id>`.
2. Count, do not feel: the same problem in another form, the third time in one area, or no more
   reliable by measurement than three fixes ago means stop and tell the human "I am stuck", with the
   evidence and the attempts written down.
3. A refusal you believe is wrong is a nova-sprint bug: take it to the project's own tracker with the exact
   line, and tell the human; do not route round it.
4. A design question (a customer, a ruling, hardware) is the human's: leave the card's judgement open
   (a `blocked` card waits for you by design), ask the human with the question named, and answer the
   judgement with the human's ruling.
