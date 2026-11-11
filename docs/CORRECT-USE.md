# Correct use, enforced by the tool

Glenn, 2026-11-06: "The key is #1. This is how we enforce correct usage,
instead of relying on LLMs to unreliably remember to do the right thing." And:
"Make a new AI, unsure of how to use nova-sprint, get walked through the
actual best practice way to use it, every time."

This page is the policy. Layer 6 (the verbs) implements it; layers 1 to 3
already refuse the moves below that are about state. Nothing here relies on an
AI remembering anything: every verb checks what must already be true, refuses
if it is not, and says what to do first.

## The one rule

Every verb runs its prerequisite table before doing anything. A prerequisite
that does not hold is a refusal, exit 1, one line, in the nova-fleet form:

```
SPRINT REFUSED <verb>: <what is not true>: <the command that makes it true>
```

A name, id or sha that does not exist (a row, a card, a stream, a commit)
is a refusal naming it, never an empty answer: v1's `queue --as <typo>`
returned `cards=0` (LESSONS 2.26).

Every successful verb ends with one line, `next: <the command to run now>`,
or `next: nothing; wait for the bus` when the next move is someone else's.

`nova-sprint` with no arguments does not print usage. It runs `nova-sprint
next --as <me>`: it looks at the machine, the bus and the store, and prints
the one thing to do now with its command. A cold AI types the tool's name and
is told what to do. `help` and `-h` exist for reading, not for finding the
way.

## The chain

Each step refuses until the one before it has happened.

| step | verb | refuses until | the refusal names |
|---|---|---|---|
| 1 | `check` | nova-tools' fleet is up (`nova-fleet check` green: Tailscale, Redis, PostgreSQL); nova-tools at the pin; `NOVA_PG_DSN` and `NOVA_BUS_REDIS` reach their servers; then the sprint's health: every stream's repository and base at origin, no open judgement, run serving, something dealt while producers are ready, no landing waiting on a person; a policy active and answering the ready producers' questions; every waiting or ready producer admitted as `add` would admit it; every working consumer on a row that is hooked, up and on the bus | every line it checked; each `CHECK RED` line names its fix; exit non-zero while any is red |
| 2 | `up` | nothing before it: it walks its own steps from step 1 each run, each refusing until it verifies (it does not run `check`) | the first step that does not verify, with its own fix |
| 3 | `stream add` | the store is at the schema `up` makes; the repository clones and its base is there; the card branches `sprint/<s>/<card>` can be created (git's ref-name rules, no parent branch at origin); with forge github, this host's gh identity can push, open and merge | `nova-sprint up`; the name to change, the branch to delete, or the gh login to make |
| 4 | `add` | the stream exists and its base is at origin; the brief passes the card contract ([CARD-CONTRACT.md](CARD-CONTRACT.md)), DONE-WHEN as one command and no retired mechanic; a row serves its tier and every NEEDS-ROUTE; its PATHS are at the base; its work is not already there; every need names a card | `stream add <s>`; the missing sections of the brief by name; the row to give the tier or route; the path; the unknown need |
| 5 | rows | a row hooks in (`nova-sprint worker --as <row>` on a bench; `nova-bus hook --as <name>` for a kin) | the recipe for the harness it detects |
| 6 | deals | `run` is a service (made by `up`) and at least one row is up | `nova-sprint up`; "no row is hooked in" with step 5's recipe |
| 7 | `take` | the name is hooked in; the card was dealt to this row at this gen | the hook recipe; "X is not dealt to this row at this generation: `nova-sprint next --as <row>`" |
| 8 | `finish` | this row holds the card; the head is a full sha; it exists on the card's branch at origin (no PR is asked: the landing opens the stream's) | the holder; "not a full sha"; "push first: `git push origin <branch>`" |
| 9 | `read` | the reader is hooked; the reader is not the author; the head is the card's | the author; "card X is at <head>" |
| 10 | landing | a read said yes to this head; CI is green at this head | "read owed"; "CI red at <head>: see <url>" |

Steps 7 to 10 are also refused by the card machine on state (a finish before a
take, a read by the author, a landed card moving), with the holder and the
state named. The chain is enforced twice: by the verb's table before it acts,
and by the engine when the move is applied.

## Keep everybody at width

The promise is parallel to width: every friend and bench busy while there is
work. Cards decide whether that can happen; the deal cannot deal a card that
waits. A dogfood board had 5 working of 74 width with 144 waiting, 136 of
them on NEEDS not yet landed (long chains). So:

- **Granular cards.** Cut work into many small, checkable cards, ids
  prefixed by area, so the board shows progress. The sprint deals cards by
  tier and width; they are never handed out per person.
- **Flat.** A NEEDS only for a hard dependency: B consumes A's landed
  output. Touching the same file is not a dependency; a small merge risk is
  fine, and both cards start. A chain of N cards lands one after another
  while the rest of the fleet waits. `add` prints the longest NEEDS chain
  through the cards it adds (`CHAIN ...`) and refuses an add that makes one
  longer than the policy's `chain_limit` (3 by default), naming the chain,
  what it takes at the stream's land rate and how to flatten it;
  `--dry-run` refuses the same, before anything is added.
- **The coordinator stitches.** After cards land, the coordinator does the
  holistic merge: conflicts across cards, integration, the base kept green.
- **Workstream branches when main moves fast.** When main moves fast or
  landing backs up, give each workstream its own stream base,
  `sprint/<workstream>`, under a stream name other than the workstream's
  (`stream add` refuses a stream `<s>` on `sprint/<s>`: its card branches
  live under it). Many cards move that branch forward without each one
  chasing main; the sprint promotes each workstream branch into main through
  one gated PR when it is green, and anything not green is the
  coordinator's land judgement. No rebasing card after card onto a moving
  main.

PATHS lists the files a card edits: `add` checks each exists at the base,
and the worker is confined to them. It never orders starts.

Before `start`, and whenever the fleet is under width, run `nova-sprint check`
and read its lines: the width line (a `WARN ... nothing dealable: N chains
(longest K, top: <card> blocks M)` when nothing is ready), one
`waits <stream>:` line per stream ("53 wait on <card> (<state>)"), and a
`WARN stream <s>: its longest NEEDS chain is K cards` line per long chain.
The steps' idle judgement says the same thing, from the same function
(`steps.Waits`).

## Hooked before anything that acts as you

As nova-bus does: `take`, `finish`, `read`, `release`, `run` and every other
verb that acts as a name refuse until that name is hooked in on the bus, and
print the hook-in recipe for the harness they detect. A row that cannot be
reached cannot hold work. Every verb refuses until you are hooked in: an unknown verb is refused first, then, before the verb reads or writes anything, it checks that --as (or NOVA_SPRINT_AS) is on the bus and exits 1 with `SPRINT REFUSED <verb>: <name> is not on the bus: hook in to the bus first: ... nova-bus hook --as <name> ...` (exit 2 when the bus itself cannot be asked); only help, version and the run, dash and worker services are exempt, and wake, check and next, which ask the bus themselves and answer its absence in their own documented shape.

## The deal message is the walkthrough

The one bus message a deal sends is the whole instruction. Its body:

```
DEAL card=<id> attempt=<n> kind=work|read stream=<s> tier=<t> model=<m> epoch=<e> repo=<org/repo> ... brief=<clipped>; in your hand now: run nova-sprint take <id>@<gen> --epoch <e> --as <you> (acknowledges, prints the whole brief), then do it and run: nova-sprint finish <id>@<gen> --head <sha> --as <you> --epoch <e>
```

`take` prints the repository, the base commit, the branch this row may push,
the brief with its done-when lines, how the card finishes, and ends with
`next: nova-sprint finish <id>@<gen> --head <sha> --as <you> --epoch <e>` (or
`read … yes|fix` for a read). Nothing is carried in a model's memory between steps; every
output names the next command.

## Finish is checked, not trusted

`finish` refuses, naming which: a head that is not forty hex characters; a
head not on the card's branch at origin; a branch other than the one dealt; a
row that is not the holder,
naming who is. v1 accepted `finish --head abc` and printed OK (Johnny,
2026-11-06); v2 never does.

## Unknown outcomes never leak

The store's unknown outcome (a reply lost after commit) is handled inside the
verb: it replays by op id and prints the real result. An AI never sees an op
id. Three exit codes: 0 done, 1 refused with the fix, 2 could not run with
the fix. Never a silent zero.

A busy store's writes never leak either. A coordinator verb's op is fenced
on what it read, and a fence is often wider than the verb's own record (a
stream's hold on every card, the coordinator verbs on every row's hook,
policy activate on every judgement). Refused by a write to a record it did
not name (a deal, a bench's report, a worker hooking in), the verb reads
again and commits, up to 5 applies, with no sleep (modules/verbs apply.go
`fenced`; tla/VerbFence.tla). It is refused only when its own record
changed, naming what changed, or after the fifth refusal, saying so.

## Notifications name their verb

Every judgement that reaches the coordinator (a red CI, a producer sent back to ready by its reads, a producer
held after too many bounces, a row that flapped, a card that cannot land) is a stored record in the store, pushed from that record, and carries the exact command that answers it:

```
NEEDS YOU judgement=<n> card=<id>: CI red at <head>: see <url>
choices: land | fix | retry
run: nova-sprint judge <n> <choice> --reason "<why>"
```

`judge` is the one verb that answers a judgement; its choices are what v1
spread over `rework`, `accept`, `unhold` and `drop` (LAYER-6, 2026-11-06). The
board does not show a "needs you" column (it stays an exact match of v1 until
it is nativized); the coordinator's `next` prints the oldest open judgement,
and the bus push carries the same line.

## What `next` looks at, in order

1. Is nova-tools' fleet up? If not: the nova-fleet command.
2. Is the store at the schema this binary knows? If not: `up`.
3. Is this name hooked in? If not: the recipe.
4. Does this name hold a card? If so: the step it is at and its command.
5. Is a judgement owed to this name? If so: its command.
6. Is a stream empty or everything landed? If so: `add`, or `done`.
7. Otherwise: `nothing; wait for the bus`.

## Everything moves by push, over nova-bus, and there is one way

Glenn, 2026-11-06: "it's important for AIs to be given structure and get push
notifications, esp. as coordinator. You should not need to keep polling or
remember to do things, things should move forward mechanically, and notify
you via push notification." "Similarly, for friends (and yourself) doing
work, you should be driven forward to accept ready cards, do work, mark the
work done automatically via push notifications." "And all push notification
should come through the nova-bus. There is ONE WAY. Do not re-implement the
nova bus in nova-sprint! Verboten!"

So:

- Every change of state that needs someone to act is one bus message to that
  someone, sent by the run loop the moment the change lands: a deal to the
  row; a read owed to the reader; a judgement to the coordinator; a landing,
  a red, a hold. The message carries the one command that answers it.
- Nobody polls. `next` is for a cold start or a lost session, never the
  mechanism; the mechanism is the hook waking the session.
- nova-sprint sends by running `nova-bus send` and reads presence with
  `nova-bus who`. It never opens Redis, never defines a stream, never keeps
  an inbox, never retries delivery on its own: nova-bus is the one way, and
  a second way is forbidden.
- The coordinator is a row like any other: hooked in, driven by pushes, with
  judgement verbs instead of take and finish.

## Built (stitch, 2026-11-07)

- `check` says the sprint's health, not only the plumbing's (it once printed
  four green lines while 24 of 25 streams could not be cloned, 28 judgements
  were open and nothing dealt). It prints every line and exits non-zero when
  any is red; each red line names its one fix. The lines, in order: the
  fleet; nova-tools against the pin (below is red, above is a `CHECK WARN`,
  exit 0); the store at schema; the bus; with `--integrity` the history; then,
  only with the store at schema, one line per stream (its repository reached
  at its clone address by `git ls-remote` and its base branch there), one line
  per kind of open judgement (the ids, the oldest's question, the `judge`
  command), who runs the sprint (the chooser lock, and the name `sprint` on
  the bus), the width (working and dealt against the open width, and when
  producers are ready and none is dealt, why: stopped, no run, no open row,
  held streams, streams with no base head, tiers no open row serves, and the
  steps' own idle judgement), and one line per stream with producers
  merging (what its landing waits for: a judgement about it, run, its hold, a
  hand landing, the lander's merge into its group branch, or CI at the group
  branch's tip). `check` stays a client verb: it reads the store and calls the
  edges, and starts nothing.
- A stream named `<org>/<repo>` with `--forge github` is reached at
  `https://github.com/<org>/<repo>.git` (`git.CloneURL`, LAYER-4 "Clone
  addresses"); `stream add` checks its base there, and a hand stream named
  `<org>/<repo>` is told to give the stream a forge.
- Admission (intent rule 34): what can be found at add is found at add,
  and each refusal names its one fix. The checks as built are listed in
  [CARD-CONTRACT.md](CARD-CONTRACT.md) "Admission". `stream add` proves the
  repository, the base, the card branch names and the forge identity; `add`
  proves each producer against the roster and the base tree; both print
  `BASE RED` (and `check` a `CHECK RED` line) for a github stream whose base
  tip is red by the landing's rule, admitting the cards (2026-11-08); `hook` proves
  the store credential (the snapshot read) and that every stream's base is
  readable from this process (finish and read need it), and asks no forge
  token of a worker; `up` step 9 says VERIFIED only when every worker bench
  runs this version and its worker has probed its harness for the policy's
  model ids and its seat's provider keys (the worker probes before its
  sprint hook, so a hooked row is a probed row); `restore` prints an `ADMIT`
  line per restored waiting or ready producer, and refuses a card record
  whose state this build does not map; `check` re-proves all of it, one line
  each.
- `check` also says: no policy active (the tick runs on the built-in
  default, which once worked a night with nothing dealt); a policy that fails
  a question the tick asks; and a working card whose row is unhooked, down or
  off the bus. A job dead on a hooked, present bench is not visible: no
  per-job heartbeat reaches the store, and the ok line says so.
- The sprint's bus messages are receipts. Its hook answers nothing that is a
  reply (a `re` set, or a REPLY subject), so a REPLY is never answered with a
  REPLY; a subject that is not a verb is logged once on its stderr and gets
  no reply; a verb is answered once with the command to run instead (the
  sprint takes no verbs over the bus).

## Built: gates are one command; a drop releases; the agreement warning (stitch w3A, 2026-11-07)

- A DONE-WHEN is one argv, not a shell line: `add` and `check` refuse a backticked
  chain, `a && b`, a pipe, a redirection, or a sentence, and the fix says to
  wrap it in `sh -c '...'` (CARD-CONTRACT "Admission"). The same parser runs
  the gate on the bench.
- `drop` (and `replace`, and a judgement answered `drop`) releases the dropped
  producer's live consumers itself, in the same op, recorded released with the
  drop's reason (no result, no blame, as `release` records it); it no longer
  asks the human to release first.
- `stream add` and `check` warn when the base requires a contributor-agreement
  check, naming the identities to sign (CARD-CONTRACT "Admission").
- `make install` puts the commands in `~/.local/bin` as a new file, signed ad
  hoc on macOS, renamed over the old one (`cmd/placebin`). Installing a
  release onto a bench is nova-fleet's install, which is another repository's
  and is not changed here.

## Built: one commit identity for the whole sprint (stitch w4I, 2026-11-07)

A repository's contributor agreement (CAA, CLA) keys off the forge account, and its allowlist
holds accounts, never people. So every agent, friend and bench worker of a sprint commits as
ONE configured identity: the git `user.name` and `user.email` of an account on the allowlist,
typically its noreply address. A contributor's own name goes only in the message, a
`By: <name>` line and a `Co-Authored-By: <name> <email>` trailer. Before this, each bench job
committed as `<row> <row@nova-sprint.local>` and friends as themselves, and heads sat in
merging behind a red agreement check.

The identity is the sprint's own state, never a literal in the code: the coordinator sets it
with `nova-sprint identity --name "<git user.name>" --email "<git user.email>" --reason
"<why>"`, and the store holds it (round 24, beside the stop). The tool enforces it in order:

- `up` step 9 asks the human for it before it looks at any bench (`ASK the human which forge
  account ...`, next: the identity command); no act can make it true.
- `hook --role worker` refuses until it is set; every hook prints `COMMIT AS <name> <email>`,
  the two `git config` lines and the By:/Co-Authored-By: rule.
- `take` refuses until it is set; its text repeats the rule for a friend, and its receipt
  carries `commit_name` and `commit_email` for the bench worker, which writes them into the
  job's clone (JOB.md and the prompt say so). A receipt without them ends the worker
  (`update nova-sprint`); staging a job with none is refused, never committed as the bench.
- `finish` reads the head's commits since the base (the attempt's base sha and the stream's
  base at origin excluded) with `git log` in a clone and refuses any commit whose author or
  committer email is not the identity's (compared case-folded), naming each commit and both
  emails; the fix is the two config lines and a `--reset-author` rebase.
- `check` has a line naming it, red when unset; the agreement warning at `stream add` and in
  `check` names it as the identity to sign for.

## Built: why the fleet is idle (stitch w4V, 2026-11-07)

- `steps.Waits` groups each stream's waiting producers by the front of the
  NEEDS chain they wait on (the first need not landed, followed while that
  need itself waits on a need), and finds the stream's longest NEEDS chain of cards not landed (sentinels add
  no length). `check`, the board and the idle judgement all read it. Its cost
  is 3.9 ms for 5,000 producers on the working machine (measured, one
  benchmark run, `BenchmarkWaits5000`).
- `check` warns, and `add` refuses, past the policy's `chain_limit` (3 by
  default; Glenn, 2026-11-08: wide and flat, NEEDS only for a hard A -> B
  dependency). The land
  rate is the stream's landings over the store's last 5,000 events (a guess:
  about an hour of a busy sprint), (landings - 1) over their span; under two
  landings it says "no land rate yet". A chain's hours are its cards over that
  rate: an estimate, since a chain's cards land one after another and the
  stream's rate counts all its landings.
- The idle judgement's text names the chains only when no ready
  producer is dealable and none resolved that tick; otherwise it keeps its
  held/unserved counts.
